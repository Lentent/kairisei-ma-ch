package cnbootstrap

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kairisei.local/server/internal/release"
)

// Use the published Punishment exchange and MR definitions through the native
// adapter and an isolated account, so the audit covers both wire and SQLite.
func auditCompleteSpheres(t *testing.T, handler http.Handler, savePath, seedPath string, cards cnCardRuntimeMaster) {
	t.Helper()
	storage, err := newCNSaveDatabase(savePath, seedPath, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	attachProbeCardCatalog(t, storage, cards)
	accounts := &cnAccountStore{storage: storage}
	identity, err := accounts.resolveLogin("00000000-0000-4000-8480-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	state, err := accounts.loadState(identity.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Spheres) != 0 {
		t.Fatal("new account unexpectedly owns spheres")
	}
	if _, err := applyCNCardRuntimeMaster(&state, cards); err != nil {
		t.Fatal(err)
	}
	state.Onboarding.Step = cnOnboardingStepCount
	state.User.Gold = 1000000
	state.Items = append(state.Items, release.Item{ItemID: 7025, Num: 50})
	for _, stack := range cards.StackCardTemplates {
		if stack.CardID == 20006001 {
			stack.Num = 1
			state.StackCards = append(state.StackCards, stack)
		}
	}
	if err := accounts.persistState(identity.UserID, state); err != nil {
		t.Fatal(err)
	}
	call := func(route string, payload any, want int) map[string]json.RawMessage {
		t.Helper()
		var body []byte
		if payload != nil {
			body, err = json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, route, strings.NewReader(identity.SessionKey+string(body))))
		lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
		var common struct {
			Code   int `json:"res_code"`
			Action int `json:"res_err_action"`
			Delete int `json:"res_is_del_savedata"`
		}
		var result map[string]json.RawMessage
		if w.Code != 200 || len(lines) != 3 || json.Unmarshal([]byte(lines[0]), &common) != nil || common.Code != want || json.Unmarshal([]byte(lines[1]), &result) != nil || (want != 0 && (common.Action != 2 || common.Delete != 0)) {
			t.Fatalf("%s: expected %d, got HTTP %d %s", route, want, w.Code, w.Body.String())
		}
		return result
	}
	load := func() release.State {
		t.Helper()
		s, err := accounts.loadState(identity.UserID)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	call("/ItemExchange", map[string]int{"itemid": 7025, "change_sets": 10}, 0)
	state = load()
	if len(state.Spheres) != 10 {
		t.Fatalf("fragment exchange gave %d spheres", len(state.Spheres))
	}
	base := state.Spheres[0]
	for _, sphere := range state.Spheres {
		if sphere.SphereID != 16000380 || sphere.Level != 1 {
			t.Fatal("incorrect summoned sphere identity")
		}
	}
	deck := state.Decks[0]
	call("/CardDeckSet", map[string]any{"decks": []any{map[string]any{
		"arthur_type": deck.ArthurType, "idx": deck.Index, "job_type": deck.JobType, "leader_card_idx": deck.LeaderCardIndex,
		"card_uniqid": deck.CardUniqueIDs, "support_card_uniqid": deck.SupportCardUniqueIDs, "sphr_uniqid": []int64{base.UniqueID, 0, 0},
		"buddy_uniqid": deck.BuddyUniqueIDs, "name": deck.Name, "is_active": deck.IsActive, "is_rental": deck.IsRental,
	}}}, 0)
	lock := func(id int64, locked bool) {
		route := "/SphrUnlock"
		if locked {
			route = "/SphrLock"
		}
		call(route, map[string]int64{"uniqid": id}, 0)
	}
	lock(base.UniqueID, true)
	material := state.Spheres[1].UniqueID
	lock(material, true)
	inputs := []map[string]any{}
	for _, sphere := range state.Spheres[1:] {
		inputs = append(inputs, map[string]any{"input_type": 0, "id": sphere.UniqueID, "num": 1})
	}
	fusion := map[string]any{"base_uniqid": base.UniqueID, "add_inputs": inputs}
	call("/SphrFusion2", fusion, -1)
	call("/SphrEvolution", map[string]any{"base_uniqid": base.UniqueID, "add_uniqid": material, "add_cardid": 0}, -1)
	call("/SphrSell", map[string]any{"uniqids": []int64{material}}, -1)
	if s := load(); len(s.Spheres) != 10 || s.User.Gold != 1000000 {
		t.Fatal("locked sphere rejection consumed inventory or gold")
	}
	lock(material, false)
	result := call("/SphrFusion2", fusion, 0)
	var wire struct {
		UniqueID int64 `json:"uniqid"`
		ID       int   `json:"sphrid"`
		Exp      int   `json:"exp"`
		Lock     int   `json:"is_lock"`
	}
	if json.Unmarshal(result["base_sphr"], &wire) != nil || wire.UniqueID != base.UniqueID || wire.Exp <= 0 || wire.Lock != 1 {
		t.Fatal("invalid native sphere fusion response")
	}
	state = load()
	if len(state.Spheres) != 1 || state.Spheres[0].Experience != wire.Exp || state.User.Gold != 1000000-9*base.BaseAddPrice {
		t.Fatal("nine MR materials did not persist correctly")
	}
	call("/SphrSell", map[string]any{"uniqids": []int64{material}}, -1)
	result = call("/SphrEvolution", map[string]any{"base_uniqid": base.UniqueID, "add_uniqid": 0, "add_cardid": 20006001}, 0)
	if json.Unmarshal(result["base_sphr"], &wire) != nil || wire.ID != 16000381 || wire.Exp != state.Spheres[0].Experience || string(result["reward"]) == "null" {
		t.Fatal("sphere evolution did not preserve progress or produce native reward shape")
	}
	state = load()
	for _, stack := range state.StackCards {
		if stack.CardID == 20006001 && stack.Num != 0 {
			t.Fatal("last evolution material was not consumed")
		}
	}
	if state.Spheres[0].SphereID != 16000381 || state.Decks[0].SphereUniqueIDs[0] != base.UniqueID {
		t.Fatal("evolution lost equipped sphere")
	}
	call("/SphrEvolution", map[string]any{"base_uniqid": base.UniqueID, "add_uniqid": 0, "add_cardid": 20006001}, -1200)
	call("/SphrShow", nil, 0)
	lock(base.UniqueID, false)
	call("/SphrSell", map[string]any{"uniqids": []int64{base.UniqueID}}, 0)
	state = load()
	if len(state.Spheres) != 0 || state.Decks[0].SphereUniqueIDs[0] != 0 {
		t.Fatal("sold sphere left stale ownership or deck reference")
	}
	t.Log(fmt.Sprintf("native Sphere: 5 fragments per Punishment, equip, locked rejection, 9 MR materials, evolution with last relic, SQLite reload and sale cleanup passed (%d definitions)", len(cards.SphereDefinitions)))
}
