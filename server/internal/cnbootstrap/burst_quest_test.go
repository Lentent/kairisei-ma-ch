package cnbootstrap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kairisei.local/server/internal/release"
)

// Extends the existing optional production-data gate; no synthetic resource
// catalog, player database, device or network listener is involved.
func probeCompleteRuntimeBurst(t *testing.T, handler http.Handler, savePath, seedPath string, cards cnCardRuntimeMaster, output string) {
	t.Helper()
	if !filepath.IsAbs(output) {
		t.Fatal("absolute probe output required")
	}
	if err := os.Mkdir(output, 0700); err != nil {
		t.Fatal(err)
	}
	storage, err := newCNSaveDatabase(savePath, seedPath, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	accounts := &cnAccountStore{storage: storage}
	attachProbeCardCatalog(t, storage, cards)
	if _, err := accounts.resolveLogin("00000000-0000-0000-0002-000000000000"); err != nil {
		t.Fatal(err)
	}
	identity, err := accounts.resolveLogin("00000000-0000-0000-0002-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	state, err := accounts.loadState(identity.UserID)
	if err != nil {
		t.Fatal(err)
	}
	state.Onboarding.Step = cnOnboardingStepCount
	state.User.UnlockedFeatureIDs = append(state.User.UnlockedFeatureIDs, 0, 1, 2, 3)
	state.User.Gold = 2000000
	state.Items = append(state.Items, release.Item{ItemID: 1305, Num: 20})
	if err := accounts.persistState(identity.UserID, state); err != nil {
		t.Fatal(err)
	}
	call := func(route, payload, name string, expected int) map[string]json.RawMessage {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, route, strings.NewReader(identity.SessionKey+payload)))
		if err := os.WriteFile(filepath.Join(output, name+".jsonl"), response.Body.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(response.Body.String()), "\n")
		var common struct {
			Code int `json:"res_code"`
		}
		var method map[string]json.RawMessage
		if response.Code != 200 || len(lines) != 3 || json.Unmarshal([]byte(lines[0]), &common) != nil || json.Unmarshal([]byte(lines[1]), &method) != nil || common.Code != expected {
			t.Fatalf("%s: HTTP %d %s", name, response.Code, response.Body.String())
		}
		return method
	}
	collection := call("/CardCollectionShow", "", "collection", 0)
	auto, err := accounts.loadState(identity.UserID)
	if err != nil || len(auto.Buddies) != len(state.Buddies)+4 || auto.BurstProgress != [4]uint8{} {
		t.Fatal("completed training did not grant the four initial Buddies independently of learning progress")
	}
	for _, quest := range release.BurstQuests() {
		if release.ArthurBurstUnlocked(auto.User.UnlockedFeatureIDs, quest.ArthurType) != 1 {
			t.Fatal("training did not unlock a sword")
		}
		var id int64
		for _, buddy := range auto.Buddies {
			if buddy.BuddyID == quest.BuddyID {
				id = buddy.UniqueID
			}
		}
		for _, deck := range auto.Decks {
			if deck.ArthurType == quest.ArthurType && (len(deck.BuddyUniqueIDs) == 0 || deck.BuddyUniqueIDs[0] != id) {
				t.Fatal("initial sword was not bound to its profession deck")
			}
		}
	}
	var pages []struct {
		Cards []struct {
			ID    int `json:"0"`
			State int `json:"1"`
		} `json:"0"`
	}
	if json.Unmarshal(collection["0"], &pages) != nil || len(pages) != 553 {
		t.Fatal("original collection pages missing")
	}
	total, acquired := 0, 0
	for _, page := range pages {
		if len(page.Cards) != 10 {
			t.Fatal("original client reads ten collection slots")
		}
		for _, card := range page.Cards {
			if card.ID != 0 {
				total++
			}
			if card.State&1 != 0 {
				acquired++
			}
		}
	}
	if total != 5528 || string(collection["2"]) != fmt.Sprint(total) || acquired == 0 || string(collection["1"]) != fmt.Sprint(acquired) || acquired == total {
		t.Fatal("collection progress differs from its catalog")
	}
	love := call("/CardLoveUp", fmt.Sprintf(`{"base_uniqid":%d,"use_items":[{"itemid":1305,"num":20}]}`, state.Cards[0].UniqueID), "love-max", 0)
	var grown struct {
		Love int `json:"love"`
	}
	if json.Unmarshal(love["base_card"], &grown) != nil || grown.Love != 10000 {
		t.Fatalf("original calcAddLove value*quantity: %s", love["base_card"])
	}
	call("/StoryTeamBattleEnd", "", "no-active-story", -6800)
	for _, quest := range release.BurstQuests() {
		label := fmt.Sprint(quest.ArthurType)
		shown := call("/TeamBattleSoloShow", fmt.Sprintf(`{"0":%d}`, quest.ArthurType), label+"-show", 0)
		var groups []map[string]json.RawMessage
		if json.Unmarshal(shown["12"], &groups) != nil || len(groups) == 0 || string(groups[0]["0"]) != fmt.Sprint(quest.GroupID) {
			t.Fatal("learning group missing")
		}
		var stories []map[string]json.RawMessage
		if json.Unmarshal(groups[0]["11"], &stories) != nil || len(stories) == 0 {
			t.Fatal("learning story missing")
		}
		// The stock parser dereferences this dictionary even when is_lock=0.
		var prerequisite map[string]json.RawMessage
		if json.Unmarshal(stories[0]["5"], &prerequisite) != nil || prerequisite == nil {
			t.Fatal("story unlock dictionary would crash TeamBattleSoloShowReceive")
		}
		call("/StoryTeamBattleStart", fmt.Sprintf(`{"story_teambattleid":%d}`, quest.StoryIDs[2]), label+"-locked", -6800)
		for step, id := range quest.StoryIDs {
			prefix := fmt.Sprintf("%s-%d", label, step)
			call("/StoryTeamBattleStart", fmt.Sprintf(`{"story_teambattleid":%d}`, id), prefix+"-start", 0)
			// StoryMgr.startStoryBattle uses IsEnableWeb(STORY=2), which is
			// false in the original CN assembly. Its fixed battle is local:
			// there is no protocol-89 StoryStart between protocols 119/120.
			// The original client sends only its session, with no JSON suffix.
			end := call("/StoryTeamBattleEnd", "", prefix+"-end", 0)
			retry := call("/StoryTeamBattleEnd", "", prefix+"-retry", 0)
			if !bytes.Equal(end["burst_unlock"], retry["burst_unlock"]) {
				t.Fatal("unlock response changed on retry")
			}
			var unlocks []json.RawMessage
			if json.Unmarshal(end["burst_unlock"], &unlocks) != nil || len(unlocks) != 0 {
				t.Fatal("optional learning must not grant the training unlock twice")
			}
		}
		call("/StoryTeamBattleStart", fmt.Sprintf(`{"story_teambattleid":%d}`, quest.StoryIDs[2]), label+"-rewatch-start", 0)
		call("/StoryTeamBattleEnd", "", label+"-rewatch-end", 0)
	}
	// Inspect committed state through an independent repository lifetime.
	final, err := accounts.loadPersistentState(identity.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if final.BurstProgress != [4]uint8{3, 3, 3, 3} || len(final.Buddies) != len(state.Buddies)+4 || final.StoryTeamBattleSession.StoryID != 0 || final.ActiveTeamBattle != nil {
		t.Fatal("persistent reward/progress or runtime boundary differs")
	}
	// Published local policy: twelve first clears, fifty crystals each.
	if final.User.CoinFree != auto.User.CoinFree+600 {
		t.Fatalf("first-clear crystals=%d, want %d without repeat rewards", final.User.CoinFree, auto.User.CoinFree+600)
	}
	if len(final.SupportDeck.CardCollectionLoveMaxIDs) != 1 || final.SupportDeck.CardCollectionLoveMaxIDs[0] != state.Cards[0].CardID || final.Cards[0].Love != 10000 || final.User.Gold != 1000000 {
		t.Fatal("loyalty value, item cost or full-love collection history was not committed")
	}
	for _, quest := range release.BurstQuests() {
		if release.ArthurBurstUnlocked(final.User.UnlockedFeatureIDs, quest.ArthurType) != 1 {
			t.Fatal("feature not committed")
		}
	}
	progress, err := json.Marshal(map[string]any{"progress": final.BurstProgress, "buddies": len(final.Buddies)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "progress.json"), progress, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("training auto-unlock, four native CN learning chains without StoryStart, duplicate End and independent SQLite reload passed")
}
