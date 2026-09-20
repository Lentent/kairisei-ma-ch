package cnbootstrap

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
)

// This runs only with the existing opt-in full-resource construction gate.
// Audit the shipped catalogs, including all own-deck variants and normal quests,
// without touching a player account, listening socket, or device.
func auditCompleteRuntimeDropCatalog(t *testing.T, state gamestate.State, cards masterdata.CardRuntimeMaster, battlePath string) {
	t.Helper()
	master, err := masterdata.LoadBattleRuntimeMaster(battlePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := masterdata.ApplyCardRuntimeMaster(&state, cards); err != nil {
		t.Fatal(err)
	}
	if err := masterdata.ApplyBattleRuntimeMaster(&state, master); err != nil {
		t.Fatal(err)
	}
	normal, err := masterdata.LoadNormalQuestRuntimeMaster(filepath.Join(filepath.Dir(battlePath), "cn602-normal-quest-runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := masterdata.ApplyNormalQuestRuntimeMaster(&state, normal); err != nil {
		t.Fatal(err)
	}
	cardIDs := func(rewards []gamestate.Reward, ordinaryOnly bool) []int {
		ids := []int{}
		for _, reward := range rewards {
			if reward.Type == 6 || (!ordinaryOnly && reward.Type == 13) {
				ids = append(ids, reward.RewardTypeID)
			}
		}
		slices.Sort(ids)
		return slices.Compact(ids)
	}
	byBoss := map[int][]int{}
	for _, profile := range state.TeamBattleRewards {
		if profile.EnemyDrops != nil {
			actual := []gamestate.Reward{}
			for _, drop := range profile.EnemyDrops {
				if drop.ChancePerMillion == nil || *drop.ChancePerMillion > 0 {
					actual = append(actual, drop.Reward)
				}
			}
			if !slices.Equal(cardIDs(actual, false), cardIDs(profile.ResultRewards, false)) {
				t.Fatalf("boss %d aggregate cards differ from per-enemy drops", profile.BossID)
			}
		}
		byBoss[profile.BossID] = cardIDs(profile.ResultRewards, true)
	}
	changedGroups, archiveBosses := 0, 0
	for i, raw := range state.TeamBattlePastBossGroups {
		var before, after masterdata.BattlePastBossGroupIdentity
		if json.Unmarshal(master.PastBossGroups[i], &before) != nil || json.Unmarshal(raw, &after) != nil {
			t.Fatal("bad archive")
		}
		if !reflect.DeepEqual(before.Cards, after.Cards) {
			changedGroups++
		}
		union := []int{}
		for _, boss := range after.Bosses {
			union = append(union, byBoss[boss.BossID]...)
			archiveBosses++
		}
		slices.Sort(union)
		slices.Sort(after.Cards)
		if !slices.Equal(slices.Compact(union), after.Cards) {
			t.Fatalf("archive %d shows cards that do not drop", after.GroupID)
		}
	}
	t.Logf("drop audit: %d reward profiles (normal/event/key/tower/own-deck), %d archive groups, %d archive difficulties; %d group portraits corrected; all actual card IDs agree", len(state.TeamBattleRewards), len(state.TeamBattlePastBossGroups), archiveBosses, changedGroups)
}
