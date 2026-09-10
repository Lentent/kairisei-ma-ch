package cnbootstrap

import (
	"encoding/json"
	"fmt"
	"io"
	"kairisei.local/server/internal/multiplayer"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"testing"
)

// Optional full-input gate: constructs the production handlers without opening
// any sockets. Its database and logs belong to the test, never to a player.
func TestCompleteRuntimeSetConstruction(t *testing.T) {
	root := os.Getenv("CN602_RUNTIME_SET")
	if root == "" {
		t.Skip("set CN602_RUNTIME_SET to the complete resource directory")
	}
	if !filepath.IsAbs(root) {
		t.Fatal("absolute resource root required")
	}
	content, err := os.ReadFile(filepath.Join(root, "resource-set.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Entrypoints map[string]string `json:"entrypoints"`
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatal(err)
	}
	p := func(key string) string {
		t.Helper()
		value, ok := manifest.Entrypoints[key]
		if !ok {
			t.Fatalf("missing entrypoint %s", key)
		}
		return filepath.Join(root, filepath.FromSlash(value))
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "requests.jsonl"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	handler, err := NewWithMultiplayerAndPVP(filepath.Join(dir, "requests.jsonl"), filepath.Join(dir, "save.json"), p("cn-save-seed"), p("cn-asset-map"), p("cn-card-master"), p("cn-explore-master"), p("cn-story-master"), p("cn-battle-master"), p("cn-navi-master"), p("cn-item-master"), p("cn-avatar-master"), p("cn-gacha-banner"), p("cn-five-star-gacha-banner"), p("cn-home-banner"), p("cn-stamp-master"), p("cn-honor-master"), p("cn-pvp-master"), p("cn-player-progression"), p("cn-login-bonus"), "127.0.0.1", 26020, multiplayer.Endpoint{Host: "127.0.0.1", Port: 26021}, multiplayer.NewHub(), p("cn-cpk-root"), p("cn-cpk-aliases"), []string{p("cn-patch-root")}, CDNConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	cards, err := loadCNCardRuntimeMaster(p("cn-card-master"))
	if err != nil {
		t.Fatal(err)
	}
	auditCompleteAdminContent(t, handler)
	auditCompletePastAdmin(t, handler)
	auditCompletePlayerPolicy(t, handler)
	auditCompleteFollowBusiness(t, handler, filepath.Join(dir, "save.json"), p("cn-save-seed"), cards)
	auditCompleteSpheres(t, handler, filepath.Join(dir, "save.json"), p("cn-save-seed"), cards)
	if profile := os.Getenv("CN602_ACCOUNT_HEAP_PROFILE"); profile != "" {
		profileCompleteRuntimeAccounts(t, handler, profile)
	}
	if output := os.Getenv("CN602_GACHA_PROBE"); output != "" {
		probeCompleteRuntimeGacha(t, handler, filepath.Join(dir, "save.json"), p("cn-save-seed"), cards, output)
	}
	if output := os.Getenv("CN602_BURST_PROBE"); output != "" {
		probeCompleteRuntimeBurst(t, handler, filepath.Join(dir, "save.json"), p("cn-save-seed"), cards, output)
	}
	if output := os.Getenv("CN602_FUSION_PROBE"); output != "" {
		probeCompleteRuntimeFusion(t, handler, filepath.Join(dir, "save.json"), p("cn-save-seed"), cards, output)
	}
	if output := os.Getenv("CN602_PURCHASE_PROBE"); output != "" {
		probeCompleteRuntimePurchase(t, handler, filepath.Join(dir, "save.json"), p("cn-save-seed"), cards, output)
	}
	if output := os.Getenv("CN602_PAGE_PROBE"); output != "" {
		probeCompleteRuntimePages(t, handler, filepath.Join(dir, "save.json"), p("cn-save-seed"), cards, output)
	}
	items, err := loadCNItemRuntimeMaster(p("cn-item-master"))
	if err != nil {
		t.Fatal(err)
	}
	_, catalog, err := buildCNAdminCatalog(cards, items)
	if err != nil {
		t.Fatal(err)
	}
	state, err := loadCNSaveState(p("cn-save-seed"))
	if err != nil {
		t.Fatal(err)
	}
	admin := &cnAdmin{catalogByKey: catalog}
	auditCompleteRuntimeDropCatalog(t, state, cards, p("cn-battle-master"))
	count := 0
	for _, base := range state.Gachas {
		if len(base.RewardPool) == 0 {
			continue
		}
		count++
		config := cnAdminGachaConfigFromProfile(base)
		if _, err := admin.validateMixedGachaConfig(base, config); err != nil {
			t.Fatalf("preset %d: %v", base.GachaID, err)
		}
		config.RewardPool[0].Reward.Num++
		if _, err := admin.validateMixedGachaConfig(base, config); err == nil {
			t.Fatal("operator could replace locked reward quantity")
		}
	}
	if count != 4 {
		t.Fatalf("mixed presets=%d", count)
	}
	t.Logf("production construction, Admin assets and %d mixed editor presets passed; no listeners started", count)
}

// Opt-in production-data reproduction of high balances and large inventories.
func attachProbeCardCatalog(t *testing.T, storage *cnSaveDatabase, cards cnCardRuntimeMaster) {
	t.Helper()
	state, err := loadCNSaveState(storage.seedPath)
	if err != nil {
		t.Fatal(err)
	}
	state.CardTemplates = cards.CardTemplates
	storage.catalog = &state
}

func probeCompleteRuntimeGacha(t *testing.T, handler http.Handler, savePath, seedPath string, cards cnCardRuntimeMaster, output string) {
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
	attachProbeCardCatalog(t, storage, cards)
	accounts := &cnAccountStore{storage: storage}
	// Reserve the prebuilt primary handler; cases use fresh uncached accounts.
	if _, err := accounts.resolveLogin("00000000-0000-0000-0001-000000000000"); err != nil {
		t.Fatal(err)
	}
	for caseID, scenario := range []struct{ paid, free, cards int }{
		{0, 1000, 0}, {0, 10000000, 0}, {10000000, 0, 0},
		{0, 10000000, 700}, {0, 10000000, 701}, {0, 10000000, 2000},
		{0, 10000000, 5995}, {0, 10000000, 6000},
	} {
		identity, err := accounts.resolveLogin(fmt.Sprintf("00000000-0000-0000-0001-%012d", caseID+1))
		if err != nil {
			t.Fatal(err)
		}
		state, err := accounts.loadState(identity.UserID)
		if err != nil {
			t.Fatal(err)
		}
		state.User.Coin, state.User.CoinFree = scenario.paid, scenario.free
		if state.User.CardMax != 6000 {
			t.Fatalf("initial card capacity=%d, want 6000", state.User.CardMax)
		}
		for len(state.Cards) < scenario.cards {
			card := state.Cards[0]
			card.UniqueID = int64(len(state.Cards) + 100000)
			state.Cards = append(state.Cards, card)
		}
		state.Onboarding.Step = cnOnboardingStepCount
		if err := accounts.persistState(identity.UserID, state); err != nil {
			t.Fatal(err)
		}
		call := func(route, payload, name string) map[string]json.RawMessage {
			t.Helper()
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, route, strings.NewReader(identity.SessionKey+payload)))
			if err := os.WriteFile(filepath.Join(output, fmt.Sprintf("%d-%s.jsonl", caseID, name)), response.Body.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusOK {
				t.Fatalf("case %d %s HTTP %d: %s", caseID, route, response.Code, response.Body.String())
			}
			lines := strings.Split(strings.TrimSpace(response.Body.String()), "\n")
			var method map[string]json.RawMessage
			var common struct {
				Code int `json:"res_code"`
			}
			if len(lines) != 3 || json.Unmarshal([]byte(lines[0]), &common) != nil || common.Code != 0 || json.Unmarshal([]byte(lines[1]), &method) != nil {
				t.Fatal("invalid protocol envelope")
			}
			return method
		}
		shown := call("/GachaShow", `{"0":0}`, "show")
		var pools []map[string]json.RawMessage
		if err := json.Unmarshal(shown["1"], &pools); err != nil {
			t.Fatal(err)
		}
		count := 0
		seenCounts := make(map[int]bool)
		for _, pool := range pools {
			var id, payType, cards int
			if json.Unmarshal(pool["0"], &id) != nil || json.Unmarshal(pool["9"], &payType) != nil || json.Unmarshal(pool["12"], &cards) != nil {
				t.Fatal("invalid gacha identity")
			}
			if payType != 3 || (cards != 1 && cards != 10) || seenCounts[cards] {
				continue
			}
			seenCounts[cards] = true
			before, err := accounts.loadState(identity.UserID)
			if err != nil {
				t.Fatal(err)
			}
			result := call("/GachaPlay2", fmt.Sprintf(`{"0":%d,"1":%d,"2":"isolated-probe","3":[],"4":0}`, id, payType), fmt.Sprintf("draw-%d", id))
			var user map[string]json.RawMessage
			if err := json.Unmarshal(result["5"], &user); err != nil {
				t.Fatal(err)
			}
			after, err := accounts.loadState(identity.UserID)
			if err != nil {
				t.Fatal(err)
			}
			var destinations []int
			if json.Unmarshal(result["11"], &destinations) != nil || len(destinations) != cards {
				t.Fatal("draw delivery flags missing")
			}
			mailed := 0
			for _, destination := range destinations {
				if destination == 1 {
					mailed++
				} else if destination != 0 {
					t.Fatal("invalid native draw delivery flag")
				}
			}
			if len(after.Cards) != min(6000, len(before.Cards)+cards) || mailed != max(0, len(before.Cards)+cards-6000) || len(after.Engagement.Presents)-len(before.Engagement.Presents) != mailed {
				t.Fatal("draw lost or duplicated cards across inventory and presents")
			}
			t.Logf("gacha case=%d inventory=%d capacity=%d id=%d draw=%d mailed=%d paid=%s free=%s", caseID, len(before.Cards), after.User.CardMax, id, cards, mailed, user["35"], user["36"])
			count++
			if count >= 2 {
				break
			}
		}
		if count != 2 {
			t.Fatalf("expected both ordinary draw options, got %d", count)
		}
		if scenario.cards == 6000 {
			before, err := accounts.loadState(identity.UserID)
			if err != nil {
				t.Fatal(err)
			}
			present := before.Engagement.Presents[len(before.Engagement.Presents)-1]
			// Sell a probe-owned, unused card, then claim the mailed draw through
			// the real adapters and reload the independent SQLite repository.
			call("/CardSell", fmt.Sprintf(`{"uniqids":[%d],"cardids":[]}`, before.Cards[len(before.Cards)-1].UniqueID), "sell-for-mail")
			call("/PresentBoxRecv", fmt.Sprintf(`{"presentid":%d}`, present.PresentID), "claim-mailed-draw")
			after, err := accounts.loadState(identity.UserID)
			if err != nil {
				t.Fatal(err)
			}
			if len(after.Cards) != 6000 || len(after.Engagement.Presents) != len(before.Engagement.Presents)-1 || len(after.Engagement.Histories) != len(before.Engagement.Histories)+1 || after.Cards[len(after.Cards)-1].CardID != present.Reward.RewardTypeID {
				t.Fatal("mailed draw was lost or duplicated when claiming after freeing capacity")
			}
		}
		db, err := storage.open()
		if err != nil {
			t.Fatal(err)
		}
		var metadataBytes, cardRows int
		err = db.QueryRow(`SELECT length(payload_json),(SELECT COUNT(*) FROM cn_account_card WHERE user_id=?) FROM cn_account_snapshot WHERE user_id=?`, identity.UserID, identity.UserID).Scan(&metadataBytes, &cardRows)
		db.Close()
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("SQLite case=%d card_rows=%d metadata_bytes=%d", caseID, cardRows, metadataBytes)
	}
}

// Opt-in profiling uses only this test's disposable accounts; no listener or player DB.
func profileCompleteRuntimeAccounts(t *testing.T, handler http.Handler, profile string) {
	t.Helper()
	for count := 0; count <= 32; count++ {
		if count > 0 {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/loginSDK.php", strings.NewReader(fmt.Sprintf(`{"uuid":"00000000-0000-0000-0000-%012d"}`, count))))
			var login struct {
				Session string `json:"sess_key"`
			}
			if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &login) != nil || login.Session == "" {
				t.Fatalf("profile login: %s", response.Body.String())
			}
			response = httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/HomeShow", strings.NewReader(login.Session)))
			if response.Code != http.StatusOK {
				t.Fatalf("profile HomeShow: %s", response.Body.String())
			}
		}
		if count%4 == 0 {
			runtime.GC()
			var mem runtime.MemStats
			runtime.ReadMemStats(&mem)
			t.Logf("accounts=%d heap_live_bytes=%d heap_inuse_bytes=%d total_alloc_bytes=%d", count, mem.HeapAlloc, mem.HeapInuse, mem.TotalAlloc)
		}
	}
	file, err := os.OpenFile(profile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := pprof.WriteHeapProfile(file); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	runtime.KeepAlive(handler)
}
