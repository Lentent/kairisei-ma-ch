package cnbootstrap

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"kairisei.local/server/internal/release"
)

func TestCNPastBossDropCatalogUsesObtainableCards(t *testing.T) {
	raw := json.RawMessage(`{"0":700400010,"4":"boss","7":[11],"8":[12],"13":[{"0":41,"9":900,"12":[{"0":11,"1":0}]}]}`)
	zero, possible := 0, 500000
	profiles := []release.TeamBattleRewardProfile{{BossID: 41,
		ResultRewards: []release.Reward{{Type: 6, Num: 1, RewardTypeID: 11}},
		EnemyDrops: []release.TeamBattleEnemyDrop{
			{Reward: release.Reward{Type: 6, Num: 1, RewardTypeID: 21}, ChancePerMillion: &possible},
			{Reward: release.Reward{Type: 6, Num: 1, RewardTypeID: 11}, ChancePerMillion: &zero},
			{Reward: release.Reward{Type: 13, Num: 3, RewardTypeID: 20000001}},
		},
	}}
	transitions := []release.EvolutionTransition{
		{FromCardID: 11, ToCardID: 12}, // similarly named, wrong family
		{FromCardID: 21, ToCardID: 22}, {FromCardID: 22, ToCardID: 23},
		{FromCardID: 21, ToCardID: 99, Type: 1}, // GuaiLi is not normal evolution
	}
	before, _ := json.Marshal(profiles)
	got, err := projectCNPastBossDropCatalog([]json.RawMessage{raw}, profiles, transitions)
	if err != nil {
		t.Fatal(err)
	}
	var group struct {
		Cards   []int `json:"7"`
		Evolved []int `json:"8"`
		Bosses  []struct {
			Picture int `json:"9"`
			Cards   []struct {
				ID int `json:"0"`
			} `json:"12"`
		} `json:"13"`
	}
	if err := json.Unmarshal(got[0], &group); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(group.Cards, []int{21}) || !slices.Equal(group.Evolved, []int{23}) ||
		len(group.Bosses[0].Cards) != 1 || group.Bosses[0].Cards[0].ID != 21 || group.Bosses[0].Picture != 900 {
		t.Fatalf("wrong actual reward/normal evolution/portrait: %s", got[0])
	}
	after, _ := json.Marshal(profiles)
	if !slices.Equal(before, after) || string(raw) != `{"0":700400010,"4":"boss","7":[11],"8":[12],"13":[{"0":41,"9":900,"12":[{"0":11,"1":0}]}]}` {
		t.Fatal("view changed the source reward table or master")
	}
	profiles[0].EnemyDrops = []release.TeamBattleEnemyDrop{}
	if _, err := projectCNPastBossDropCatalog([]json.RawMessage{raw}, profiles, transitions); err == nil {
		t.Fatal("an explicitly empty drop table must not advertise aggregate rewards")
	}
	profiles[0].EnemyDrops = nil
	got, err = projectCNPastBossDropCatalog([]json.RawMessage{raw}, profiles, transitions)
	if err != nil || json.Unmarshal(got[0], &group) != nil || !slices.Equal(group.Cards, []int{11}) {
		t.Fatalf("legacy aggregate reward fallback failed: %v", err)
	}
}

// This runs only with the existing opt-in full-resource construction gate.
// Audit the shipped catalogs, including all own-deck variants and normal quests,
// without touching a player account, listening socket, or device.
func auditCompleteRuntimeDropCatalog(t *testing.T, state release.State, cards cnCardRuntimeMaster, battlePath string) {
	t.Helper()
	master, err := loadCNBattleRuntimeMaster(battlePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applyCNCardRuntimeMaster(&state, cards); err != nil {
		t.Fatal(err)
	}
	if err := applyCNBattleRuntimeMaster(&state, master); err != nil {
		t.Fatal(err)
	}
	normal, err := loadCNNormalQuestRuntimeMaster(filepath.Join(filepath.Dir(battlePath), "cn602-normal-quest-runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := applyCNNormalQuestRuntimeMaster(&state, normal); err != nil {
		t.Fatal(err)
	}
	cardIDs := func(rewards []release.Reward, ordinaryOnly bool) []int {
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
			actual := []release.Reward{}
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
		var before, after cnBattlePastBossGroupIdentity
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
