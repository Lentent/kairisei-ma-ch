package httpapi

import (
	"encoding/json"
	"testing"

	"kairisei.local/server/internal/release"
)

func TestPastBossUsesAccountProgress(t *testing.T) {
	source := []json.RawMessage{json.RawMessage(`{"0":7,"13":[{"0":101,"10":0},{"0":102,"10":0}]}`)}
	progress := json.RawMessage(`{"0":0,"9":[],"10":[{"10":[{"0":101,"10":2},{"0":102,"10":1}]}],"11":[],"12":[]}`)
	got, err := pastBossProgress(source, progress)
	if err != nil {
		t.Fatal(err)
	}
	var group struct {
		Bosses []struct {
			State int `json:"10"`
		} `json:"13"`
	}
	if err := json.Unmarshal(got[0], &group); err != nil {
		t.Fatal(err)
	}
	if group.Bosses[0].State != 2 || group.Bosses[1].State != 1 {
		t.Fatalf("progress %+v", group)
	}
	if string(source[0]) != `{"0":7,"13":[{"0":101,"10":0},{"0":102,"10":0}]}` {
		t.Fatal("shared archive changed")
	}
}

func TestCardRewardRemembersPreviouslyCollectedCard(t *testing.T) {
	s := &store{
		cardProgression:   release.CardProgressionPolicy{ConfigVersion: 1, FusionGoldPerMaterialPerBaseLevel: 1},
		cardCollectionIDs: map[int]struct{}{10: {}},
		cardTemplates:     map[int]cardInfo{10: {CardID: 10, LevelMax: 1}},
		cardDefinitions:   map[int]release.Card{10: {CardID: 10, LevelMax: 1, ExperienceTableID: 1, FameMax: 100}},
		cardExperience:    map[int][]int{1: {}}, nextUniqueID: 1,
	}
	result := presentReceiveResult{}
	_, owned := s.gachaStateWithOwnership()
	if _, ok := owned[10]; !ok {
		t.Fatal("previously collected card is missing from gacha lineup ownership")
	}
	if err := s.applyRewardLocked(gachaCardReward(10), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Rewards) != 1 || result.Rewards[0].IsNew != 0 {
		t.Fatalf("previously collected reward: %+v", result.Rewards)
	}
}
