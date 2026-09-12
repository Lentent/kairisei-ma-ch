package cnbootstrap

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"kairisei.local/server/internal/release"
)

func auditCompleteCollectionRewards(t *testing.T, h http.Handler, savePath, seedPath, stampPath, honorPath string, cards cnCardRuntimeMaster) {
	t.Helper()
	storage, err := newCNSaveDatabase(savePath, seedPath, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	attachProbeCardCatalog(t, storage, cards)
	accounts := &cnAccountStore{storage: storage}
	identity, err := accounts.resolveLogin("00000000-0000-4000-8491-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	state, err := accounts.loadPersistentState(identity.UserID)
	if err != nil {
		t.Fatal(err)
	}
	state.InventorySequence = release.InventorySequenceState{Card: 900001, Sphere: 900002, Buddy: 900003}
	stamps, err := loadCNStampRuntimeMaster(stampPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = applyCNStampRuntimeMaster(&state, stamps); err != nil {
		t.Fatal(err)
	}
	honors, err := loadCNHonorRuntimeMaster(honorPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = applyCNHonorRuntimeMaster(&state, honors); err != nil {
		t.Fatal(err)
	}
	admin := h.(interface{ AdminHandler() http.Handler }).AdminHandler()
	ownership := map[string][]int{"costume": {1}, "stamp": state.Stamps.StampIDs, "honor": state.Honors.HonorIDs}
	want := map[string]int{}
	before := map[string]int{}
	state.Engagement.Presents = nil
	for _, kind := range []string{"costume", "stamp", "honor", "sphere", "buddy", "card", "item", "material"} {
		var catalog struct {
			Entries []cnAdminCatalogEntry `json:"entries"`
		}
		if err := json.Unmarshal(callContentAdmin(t, admin, "GET", "/api/catalog?kind="+kind+"&limit=200", nil, 200), &catalog); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, entry := range catalog.Entries {
			if slices.Contains(ownership[kind], entry.RewardTypeID) || entry.ResourceState == "unavailable" {
				continue
			}
			reward := release.Reward{Type: entry.RewardType, RewardTypeID: entry.RewardTypeID, Num: 1, CardSkillLevels: []int16{}}
			if kind == "card" {
				reward.CardLevel, reward.CardFame, reward.CardSkillLevels = 1, 1, []int16{1}
			}
			state.Engagement.Presents = append(state.Engagement.Presents, release.Present{PresentID: int64(491000 + len(state.Engagement.Presents)), Title: "运营奖励", Reward: reward})
			want[kind] = entry.RewardTypeID
			before[kind] = inventoryRewardCount(t, state, reward)
			if !release.IsCollectionReward(reward.Type) {
				reward.Num = 2
			}
			state.Engagement.Presents = append(state.Engagement.Presents, release.Present{PresentID: int64(491000 + len(state.Engagement.Presents)), Title: "同类再次赠送", Reward: reward})
			found = true
			break
		}
		if !found {
			t.Fatalf("no unowned %s sample", kind)
		}
	}
	if err = accounts.persistState(identity.UserID, state); err != nil {
		t.Fatal(err)
	}
	call := func(route string, payload any) map[string]json.RawMessage {
		t.Helper()
		data, _ := json.Marshal(payload)
		if payload == nil {
			data = nil
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", route, strings.NewReader(identity.SessionKey+string(data))))
		lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
		var common struct {
			Code int `json:"res_code"`
		}
		var result map[string]json.RawMessage
		if w.Code != 200 || len(lines) != 3 || json.Unmarshal([]byte(lines[0]), &common) != nil || common.Code != 0 || json.Unmarshal([]byte(lines[1]), &result) != nil {
			t.Fatalf("%s: %d %s", route, w.Code, w.Body.String())
		}
		return result
	}
	stampReceived := false
	for _, present := range state.Engagement.Presents {
		result := call("/PresentBoxRecv", map[string]int64{"presentid": present.PresentID})
		if present.Reward.Type == 16 {
			var ids []int
			if json.Unmarshal(result["new_stampids"], &ids) != nil || (!stampReceived && !slices.Contains(ids, want["stamp"])) || (stampReceived && len(ids) != 0) {
				t.Fatalf("native stamp %d ownership delta absent: %s (prior: %v)", want["stamp"], result["new_stampids"], state.Stamps.StampIDs)
			}
			stampReceived = true
		}
		call("/PresentBoxRecv", map[string]int64{"presentid": present.PresentID})
	}
	var costumes []int
	if json.Unmarshal(call("/CostumeShow", nil)["costumeids"], &costumes) != nil || !slices.Contains(costumes, want["costume"]) {
		t.Fatal("costume window did not refresh ownership")
	}
	reloaded, err := accounts.loadPersistentState(identity.UserID)
	if err != nil {
		t.Fatal(err)
	}
	var costume struct {
		IDs []int `json:"costumeids"`
	}
	_ = json.Unmarshal(reloaded.Costume, &costume)
	if !slices.Contains(costume.IDs, want["costume"]) || !slices.Contains(reloaded.Stamps.StampIDs, want["stamp"]) || !slices.Contains(reloaded.Honors.HonorIDs, want["honor"]) {
		t.Fatal("unlock lost after SQL reload")
	}
	for kind, rewardType := range map[string]int{"card": 6, "item": 8, "material": 13, "sphere": 15, "buddy": 19} {
		if got := inventoryRewardCount(t, reloaded, release.Reward{Type: rewardType, RewardTypeID: want[kind]}); got != before[kind]+3 {
			t.Fatalf("%s repeat gift/retry: got %d, expected %d", kind, got, before[kind]+3)
		}
	}
	for _, card := range reloaded.Cards {
		if card.CardID == want["card"] && card.UniqueID >= 900001 {
			want["card"] = 0
			break
		}
	}
	for _, sphere := range reloaded.Spheres {
		if sphere.SphereID == want["sphere"] && sphere.UniqueID >= 900002 {
			want["sphere"] = 0
			break
		}
	}
	for _, buddy := range reloaded.Buddies {
		if buddy.BuddyID == want["buddy"] && buddy.UniqueID >= 900003 {
			want["buddy"] = 0
			break
		}
	}
	if want["card"] != 0 || want["sphere"] != 0 || want["buddy"] != 0 {
		t.Fatal("instance IDs regressed after account reload")
	}
	t.Log("native repeat gifts/retry: independent card/sphere/buddy copies, additive item/material stacks, unique unlocks, SQLite and monotonic IDs passed")
}

func inventoryRewardCount(t *testing.T, state release.State, reward release.Reward) int {
	t.Helper()
	total := 0
	seen := make(map[int64]bool)
	addInstance := func(uniqueID int64) {
		if seen[uniqueID] {
			t.Fatalf("reward type %d repeats instance %d", reward.Type, uniqueID)
		}
		seen[uniqueID] = true
		total++
	}
	switch reward.Type {
	case 6:
		for _, card := range state.Cards {
			if card.CardID == reward.RewardTypeID {
				addInstance(card.UniqueID)
			}
		}
	case 15:
		for _, sphere := range state.Spheres {
			if sphere.SphereID == reward.RewardTypeID {
				addInstance(sphere.UniqueID)
			}
		}
	case 19:
		for _, buddy := range state.Buddies {
			if buddy.BuddyID == reward.RewardTypeID {
				addInstance(buddy.UniqueID)
			}
		}
	case 8:
		for _, item := range state.Items {
			if item.ItemID == reward.RewardTypeID {
				total += item.Num
			}
		}
	case 13:
		for _, stack := range state.StackCards {
			if stack.CardID == reward.RewardTypeID {
				total += stack.Num
			}
		}
	}
	return total
}
