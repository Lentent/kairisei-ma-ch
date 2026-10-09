package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

// Both registries must survive a mixed deployment without resetting identities,
// resurrecting deleted pools, or dropping old draw rules.
func TestMergedLegacyPoolCopyRecycleRestartAndPurge(t *testing.T) {
	a, accounts, legacy := customGachaTestAPI(t)
	w := customGachaRequest(t, legacy, "/create", map[string]any{"source_id": 60201301, "name": "legacy"})
	if w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	var created struct {
		ID int `json:"gacha_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id := created.ID
	router := chi.NewRouter()
	router.Post("/pools", a.createCustomGacha)
	router.Post("/pools/{gachaID}/{action}", a.changeCustomGachaDeletion)
	call := func(path string, body any) {
		t.Helper()
		w := customGachaRequest(t, router, path, body)
		if w.Code != 200 {
			t.Fatal(path, w.Body.String())
		}
	}
	call("/pools", map[string]any{"source_id": id, "name": "copy", "expected_revision": a.operations.customGachaRevision})
	copyID := a.operations.nextCustomGachaID() - 1
	if copyID <= id || a.operations.customGachas[copyID].LegacyProfile == nil {
		t.Fatal("legacy copy lost its rule snapshot")
	}
	config := AdminGachaConfigFromProfile(a.operations.gachaBases[id])
	config.CardFames = map[int]int{1: 5}
	if _, err := a.validateGachaConfig(config); err != nil {
		t.Fatal("legacy pool cannot use new fame settings", err)
	}
	call("/pools/"+strconv.Itoa(id)+"/delete", map[string]any{"expected_revision": a.operations.customGachaRevision})
	w = customGachaRequest(t, legacy, "/create", map[string]any{"source_id": id, "name": "deleted copy"})
	if w.Code != http.StatusBadRequest {
		t.Fatal("legacy creator copied a deleted pool", w.Body.String())
	}
	seed := []gamestate.GachaProfile{a.operations.gachaBases[60201301]}
	reloaded, err := NewOperations(accounts.Database(), seed)
	if err != nil {
		t.Fatal("restart after recycle", err)
	}
	if !reloaded.customGachas[id].Deleted || !reloaded.legacyCustomGachas[copyID] {
		t.Fatal("restart dropped deletion or copied legacy identity")
	}
	a.operations = reloaded
	call("/pools/"+strconv.Itoa(id)+"/restore", map[string]any{"expected_revision": a.operations.customGachaRevision})
	if a.operations.customGachas[id].Deleted {
		t.Fatal("restore failed")
	}
	call("/pools/"+strconv.Itoa(id)+"/delete", map[string]any{"expected_revision": a.operations.customGachaRevision})
	preview, err := a.purgeGacha(context.Background(), id, "")
	if err != nil {
		t.Fatal("legacy purge preview", err)
	}
	result, err := a.purgeGacha(context.Background(), id, preview.Digest)
	if err != nil || !result.Applied {
		t.Fatal("legacy purge apply", err)
	}
	reloaded, err = NewOperations(accounts.Database(), seed)
	if err != nil {
		t.Fatal("restart after permanent deletion", err)
	}
	if _, exists := reloaded.gachaBases[id]; exists {
		t.Fatal("old registry resurrected permanently deleted pool")
	}
	if !reloaded.legacyCustomGachas[copyID] || reloaded.nextCustomGachaID() <= copyID {
		t.Fatal("purge removed independent copy or reused identities")
	}
}

func TestMergedNoticeScheduleSuppressesHomePopup(t *testing.T) {
	state := testfixture.RuntimeState(t)
	account, err := game.New(state)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	o := &Operations{}
	for i, window := range []struct {
		start, end int64
		visible    bool
	}{{now + 3600, 0, false}, {0, now - 3600, false}, {now - 3600, now + 3600, true}} {
		p := PlayerPolicy{Notice: noticePolicy{Enabled: true, StartUnix: window.start, EndUnix: window.end, Entries: []noticeEntry{{Enabled: true, Title: "公告", Body: "内容"}}}}
		account.ApplyPlayerConfiguration(o.playerSnapshot(p, i+1).Runtime)
		if shown := account.UnreadNoticePath(1) != ""; shown != window.visible {
			t.Fatalf("schedule %d: visible=%v want=%v", i, shown, window.visible)
		}
	}
}
