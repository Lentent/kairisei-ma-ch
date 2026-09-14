package cnbootstrap

import (
	"encoding/json"
	"slices"
	"testing"

	"kairisei.local/server/internal/httpapi"
	"kairisei.local/server/internal/release"
)

// Reload real SQLite snapshots against an unmodified catalog, using new
// repository instances as after eviction/restart, for both account tables.
func TestBattleProgressSurvivesAccountAndServerReload(t *testing.T) {
	for _, category := range []string{"9", "10", "11", "12"} {
		t.Run(category, func(t *testing.T) {
			accounts := newFriendCapacityTestAccounts(t)
			catalog, err := accounts.storage.catalogState()
			if err != nil {
				t.Fatal(err)
			}
			var top map[string]json.RawMessage
			if err := json.Unmarshal(catalog.TeamBattleSolo, &top); err != nil {
				t.Fatal(err)
			}
			var groups []json.RawMessage
			if err := json.Unmarshal(top["9"], &groups); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"9", "10", "11", "12"} {
				top[key] = json.RawMessage("[]")
			}
			top[category], _ = json.Marshal(groups[:1])
			catalog.TeamBattleSolo, _ = json.Marshal(top)
			accounts.storage.catalog = &catalog
			createNamedFriendCapacityAccount(t, accounts, 0x490)
			other := createNamedFriendCapacityAccount(t, accounts, 0x491)
			for _, userID := range []int{cnPrimaryUserID, other} {
				state, err := accounts.loadPersistentState(userID)
				if err != nil {
					t.Fatal(err)
				}
				var data map[string]json.RawMessage
				_ = json.Unmarshal(state.TeamBattleSolo, &data)
				var entries []map[string]json.RawMessage
				_ = json.Unmarshal(data[category], &entries)
				var bosses []map[string]json.RawMessage
				_ = json.Unmarshal(entries[0]["10"], &bosses)
				if string(bosses[0]["10"]) != "0" {
					t.Fatal("another account modified the shared catalog")
				}
				bosses[0]["10"] = json.RawMessage("2")
				entries[0]["10"], _ = json.Marshal(bosses)
				data[category], _ = json.Marshal(entries)
				state.TeamBattleSolo, _ = json.Marshal(data)
				state.User.CoinFree += 50
				for reload := 0; reload < 2; reload++ {
					if err := accounts.persistState(userID, state); err != nil {
						t.Fatal(err)
					}
					storage := *accounts.storage
					fresh := &cnAccountStore{storage: &storage}
					reloaded, err := fresh.loadPersistentState(userID)
					if err != nil {
						t.Fatal(err)
					}
					reloaded = httpapi.ApplyContentState(reloaded, httpapi.ContentConfiguration{Revision: 1, State: catalog})
					_ = json.Unmarshal(reloaded.TeamBattleSolo, &data)
					_ = json.Unmarshal(data[category], &entries)
					_ = json.Unmarshal(entries[0]["10"], &bosses)
					if string(bosses[0]["10"]) != "2" || reloaded.User.CoinFree != state.User.CoinFree {
						t.Fatalf("user %d category %s reload %d lost clear/wallet: %s / %d", userID, category, reload, bosses[0]["10"], reloaded.User.CoinFree)
					}
					state = reloaded
				}
			}
		})
	}
}

func TestCollectionUnlocksSurviveAccountReload(t *testing.T) {
	accounts := newFriendCapacityTestAccounts(t)
	state, err := accounts.loadPersistentState(cnPrimaryUserID)
	if err != nil {
		t.Fatal(err)
	}
	state.Costume = json.RawMessage(`{"costumeids":[1,2]}`)
	state.InventorySequence = release.InventorySequenceState{Card: 900001, Sphere: 900002, Buddy: 900003}
	state.Stamps.StampIDs = append(state.Stamps.StampIDs, 900001)
	state.Honors.HonorIDs = append(state.Honors.HonorIDs, 900002)
	// Existing schema stores an ID list independently of the legacy 63-bit mask.
	state.User.SelectableNaviIDs = append(state.User.SelectableNaviIDs, 63, 64, 70)
	state.User.NaviID = 70
	if err = accounts.persistState(cnPrimaryUserID, state); err != nil {
		t.Fatal(err)
	}
	fresh := &cnAccountStore{storage: accounts.storage}
	reloaded, err := fresh.loadPersistentState(cnPrimaryUserID)
	if err != nil {
		t.Fatal(err)
	}
	if string(reloaded.Costume) != string(state.Costume) || len(reloaded.Stamps.StampIDs) != len(state.Stamps.StampIDs) || len(reloaded.Honors.HonorIDs) != len(state.Honors.HonorIDs) {
		t.Fatal("collection ownership lost on reload")
	}
	if reloaded.InventorySequence != state.InventorySequence {
		t.Fatal("instance sequence lost on reload")
	}
	if reloaded.User.NaviID != 70 || reloaded.User.NaviUnlockFlag != state.User.NaviUnlockFlag ||
		!slices.Equal(reloaded.User.SelectableNaviIDs, state.User.SelectableNaviIDs) {
		t.Fatal("expanded navigator ownership lost in existing SQLite format")
	}
}
