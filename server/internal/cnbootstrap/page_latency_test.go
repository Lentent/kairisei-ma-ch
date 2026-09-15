package cnbootstrap

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// An opt-in diagnostic against the complete runtime, not a timing assertion in
// CI. It includes session resolution, account construction, adapters and saves.
func probeCompleteRuntimePages(t *testing.T, handler http.Handler, savePath, seedPath string, cards cnCardRuntimeMaster, output string) {
	t.Helper()
	storage, err := newCNSaveDatabase(savePath, seedPath, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	accounts := &cnAccountStore{storage: storage}
	attachProbeCardCatalog(t, storage, cards)
	if _, err := accounts.resolveLogin("00000000-0000-0000-0003-000000000000"); err != nil {
		t.Fatal(err)
	}
	rows := []map[string]any{}
	for index, count := range []int{10, 2000} {
		identity, err := accounts.resolveLogin(fmt.Sprintf("00000000-0000-0000-0003-%012d", index+1))
		if err != nil {
			t.Fatal(err)
		}
		state, err := accounts.loadState(identity.UserID)
		if err != nil {
			t.Fatal(err)
		}
		seen := make(map[int]bool)
		for _, card := range state.Cards {
			seen[card.CardID] = true
		}
		for _, card := range cards.CardTemplates {
			if len(state.Cards) >= count {
				break
			}
			if seen[card.CardID] {
				continue
			}
			card.UniqueID = int64(len(state.Cards) + 1)
			state.Cards = append(state.Cards, card)
			state.SupportDeck.CardCollectionIDs = append(state.SupportDeck.CardCollectionIDs, card.CardID)
		}
		state.Onboarding.Step = cnOnboardingStepCount
		if err := accounts.persistState(identity.UserID, state); err != nil {
			t.Fatal(err)
		}
		for pass := 0; pass < 3; pass++ {
			for _, route := range []string{"/HomeShow", "/CardShow2", "/CardCategoryGet", "/DeckLimitShow", "/CardCollectionShow"} {
				response := httptest.NewRecorder()
				start := time.Now()
				handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, route, strings.NewReader(identity.SessionKey)))
				ms := float64(time.Since(start).Microseconds()) / 1000
				var common struct {
					Code int `json:"res_code"`
				}
				if response.Code != 200 || json.Unmarshal([]byte(strings.Split(response.Body.String(), "\n")[0]), &common) != nil || common.Code != 0 {
					t.Fatalf("%s: %d %.500s", route, response.Code, response.Body.String())
				}
				compression := map[string]int{}
				for _, level := range []int{1, 6} {
					var packed bytes.Buffer
					zipper, err := gzip.NewWriterLevel(&packed, level)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := zipper.Write(response.Body.Bytes()); err != nil {
						t.Fatal(err)
					}
					if err := zipper.Close(); err != nil {
						t.Fatal(err)
					}
					compression[fmt.Sprintf("gzip%d_bytes", level)] = packed.Len()
				}
				rows = append(rows, map[string]any{"cards": count, "pass": pass, "route": route, "elapsed_ms": ms, "response_bytes": response.Body.Len(), "compression": compression})
				if pass == 0 {
					path := fmt.Sprintf("%s.%d.%s.txt", output, count, strings.TrimPrefix(route, "/"))
					file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
					if err != nil {
						t.Fatal(err)
					}
					_, err = file.Write(response.Body.Bytes())
					closeErr := file.Close()
					if err != nil || closeErr != nil {
						t.Fatalf("write response sample: %v / %v", err, closeErr)
					}
				}
			}
		}
	}
	content, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Write(content); err != nil {
		t.Fatal(err)
	}
}
