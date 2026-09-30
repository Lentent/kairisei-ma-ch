package gamestate

import "testing"

func TestItemGachaHonorValidation(t *testing.T) {
	r := Reward{Type: 18, RewardTypeID: 26093002, Num: 1, CardSkillLevels: []int16{}}
	p := []WeightedReward{{Reward: r, Weight: 10}}
	if err := ValidateItemGachaRewardPool(p); err != nil {
		t.Fatal(err)
	}
	if p[0].Reward.Type != 18 {
		t.Fatal("validation mutated honor reward")
	}
	if ValidateGachaReward(r) == nil {
		t.Fatal("ordinary gacha restrictions changed")
	}
	for _, num := range []int{0, 2} {
		r.Num = num
		if ValidateItemGachaReward(r) == nil {
			t.Fatal("accepted invalid honor quantity")
		}
	}
	p[0].Weight = 0
	if ValidateItemGachaRewardPool(p) == nil {
		t.Fatal("accepted zero weight")
	}
}
