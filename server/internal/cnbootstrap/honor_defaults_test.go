package cnbootstrap

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/testfixture"
)

func TestNewAccountHonorDefaultsAndEarnedOwnership(t *testing.T) {
	const earnedHonorID = 26093002
	starterIDs := []int{10000000, 10100001, 10100002, 10100003, 10100004, 10100005, 10100006, 10100007, 10100008}
	seed, err := accountstore.LoadSaveState(filepath.Join("..", "..", "config", "cn602-save-template.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Deliberately contaminate the catalog with another player's collection.
	seed.Honors = gamestate.HonorCollectionState{
		HonorIDs:     []int{earnedHonorID},
		DeckHonorIDs: []int{earnedHonorID, earnedHonorID, 0, 0},
	}
	root := t.TempDir()
	seedPath := filepath.Join(root, "seed.json")
	content, err := json.Marshal(accountstore.SaveFromState(seed))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(seedPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	type honorRow struct {
		ID           int    `json:"honor_id"`
		Name         string `json:"name"`
		SlotMask     int    `json:"slot_mask"`
		SameCardID   int    `json:"same_card_id"`
		GetType      int    `json:"get_type"`
		DefaultOwned bool   `json:"default_owned"`
	}
	rows := make([]honorRow, 0, len(starterIDs)+1)
	for _, id := range starterIDs {
		rows = append(rows, honorRow{ID: id, Name: "基础称号", SlotMask: 15, DefaultOwned: true})
	}
	rows = append(rows, honorRow{ID: earnedHonorID, Name: "奖励称号", SlotMask: 15, GetType: 1})
	content, err = json.Marshal(map[string]any{
		"schema_version": 1,
		"client_profile": "cn602-bootstrap",
		"source":         map[string]any{"test": true},
		"honors":         rows,
	})
	if err != nil {
		t.Fatal(err)
	}
	masterPath := filepath.Join(root, "honor-master.json")
	if err := os.WriteFile(masterPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	master, err := masterdata.LoadHonorRuntimeMaster(masterPath)
	if err != nil {
		t.Fatal(err)
	}
	requireClean := func(t *testing.T, state gamestate.State) {
		t.Helper()
		if len(state.Honors.HonorIDs) != 0 || !slices.Equal(state.Honors.DeckHonorIDs, []int{0, 0, 0, 0}) {
			t.Fatalf("new account inherited seed honors: %+v", state.Honors)
		}
	}
	t.Run("direct onboarding", func(t *testing.T) {
		state, err := accountstore.LoadSaveState(seedPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := accountstore.InitializeOnboardingSnapshot(&state, accountstore.PrimaryUserID+10); err != nil {
			t.Fatal(err)
		}
		requireClean(t, state)
	})
	storage, err := accountstore.OpenDatabase(filepath.Join(root, "save.json"), seedPath,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := storage.Close(); err != nil {
			t.Error(err)
		}
	})
	storage.SetCatalog(seed)
	if _, err := storage.LoadOrImport(); err != nil {
		t.Fatal(err)
	}
	accounts, err := accountstore.NewAccounts(storage)
	if err != nil {
		t.Fatal(err)
	}
	for index, name := range []string{"primary", "ordinary"} {
		identity, err := accounts.ResolveLogin([]string{
			"00000000-0000-4000-8000-000000000001",
			"00000000-0000-4000-8000-000000000002",
		}[index])
		if err != nil {
			t.Fatal(err)
		}
		if identity.UserID != accountstore.PrimaryUserID+index {
			t.Fatalf("%s account ID = %d", name, identity.UserID)
		}
		t.Run(name, func(t *testing.T) {
			state, err := accounts.LoadPersistentState(identity.UserID)
			if err != nil {
				t.Fatal(err)
			}
			requireClean(t, state)
			if _, err := masterdata.ApplyHonorRuntimeMaster(&state, master); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(state.Honors.HonorIDs, starterIDs) || !slices.Equal(state.Honors.DeckHonorIDs, []int{0, 0, 0, 0}) {
				t.Fatalf("new account defaults = %+v, want only %v", state.Honors, starterIDs)
			}
			// Exercise the actual reward receiver with its complete runtime fixture;
			// only collection ownership is transferred to this SQLite account.
			rewardState := testfixture.RuntimeState(t)
			rewardState.Honors = state.Honors
			if _, err := masterdata.ApplyHonorRuntimeMaster(&rewardState, master); err != nil {
				t.Fatal(err)
			}
			rewardState.Engagement.Presents = []gamestate.Present{{
				PresentID: 910001, Title: "称号奖励",
				Reward: gamestate.Reward{Type: 18, RewardTypeID: earnedHonorID, Num: 1, CardSkillLevels: []int16{}},
			}}
			player, err := game.New(rewardState)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := player.ReceivePresent(910001); err != nil {
				t.Fatalf("receive earned honor: %v", err)
			}
			if !player.SetHonorDeck([]int{earnedHonorID, 0, 0, 0}) {
				t.Fatal("earned honor could not be equipped")
			}
			state.Honors = player.Snapshot(rewardState).Honors
			if err := accounts.PersistState(identity.UserID, state); err != nil {
				t.Fatal(err)
			}
			fresh, err := accountstore.NewAccounts(storage)
			if err != nil {
				t.Fatal(err)
			}
			state, err = fresh.LoadPersistentState(identity.UserID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := masterdata.ApplyHonorRuntimeMaster(&state, master); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(state.Honors.HonorIDs, append(slices.Clone(starterIDs), earnedHonorID)) ||
				!slices.Equal(state.Honors.DeckHonorIDs, []int{earnedHonorID, 0, 0, 0}) {
				t.Fatalf("earned honor ownership or equipment lost after reload: %+v", state.Honors)
			}
		})
	}
}
