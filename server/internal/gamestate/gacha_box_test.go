package gamestate

import "testing"

func TestGachaBoxRules(t *testing.T) {
	p := GachaProfile{CardNum: 1, CardNumMax: 1, BoxRounds: make([]GachaBoxRound, 11)}
	for i := range p.BoxRounds {
		p.BoxRounds[i].Rewards = []GachaBoxReward{{Stock: 50, Reward: Reward{Type: 4, Num: 10, CardSkillLevels: []int16{}}}}
	}
	if err := ValidateGachaRules(p); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*GachaProfile){
		"no stock":       func(p *GachaProfile) { p.BoxRounds[4].Rewards[0].Stock = 0 },
		"missing loop":   func(p *GachaProfile) { p.BoxRounds = p.BoxRounds[:10] },
		"too many draws": func(p *GachaProfile) { p.CardNum, p.CardNumMax = 12, 12 },
		"variable draws": func(p *GachaProfile) { p.CardNum, p.CardNumMax = 1, 10 },
		"round cap":      func(p *GachaProfile) { p.PlayCountMax = 999 },
		"ordinary rules": func(p *GachaProfile) { p.CardIDs = []int{1} },
	} {
		t.Run(name, func(t *testing.T) {
			bad := CloneGachas([]GachaProfile{p})[0]
			mutate(&bad)
			if err := ValidateGachaRules(bad); err == nil {
				t.Fatal("invalid box accepted")
			}
		})
	}
	if p.BoxRounds[4].Rewards[0].Stock != 50 {
		t.Fatal("cloned templates are shared")
	}
	for _, count := range []int{1, 10, 11} {
		multi := CloneGachas([]GachaProfile{p})[0]
		multi.CardNum, multi.CardNumMax = count, count
		if err := ValidateGachaBoxRules(multi); err != nil {
			t.Fatalf("fixed %d draws rejected: %v", count, err)
		}
	}
}

func TestGachaBoxVariableStocksAndIndependentRewardLimit(t *testing.T) {
	p := GachaProfile{CardNum: 1, CardNumMax: 1, BoxRounds: make([]GachaBoxRound, GachaBoxTemplates)}
	for i := range p.BoxRounds {
		p.BoxRounds[i].Rewards = []GachaBoxReward{{Stock: (i + 1) * 10, Reward: Reward{Type: 4, Num: 1, CardSkillLevels: []int16{}}}}
	}
	if err := ValidateGachaBoxRules(p); err != nil {
		t.Fatal("different round sizes must be valid", err)
	}
	reward := Reward{Type: 4, Num: 1, CardSkillLevels: []int16{}}
	for _, stock := range []int{1, 10, 20, 100, GachaBoxMaxStock} {
		if err := ValidateGachaBoxRewards([]GachaBoxReward{{Stock: stock, Reward: reward}}); err != nil {
			t.Fatalf("stock %d: %v", stock, err)
		}
	}
	entries := make([]GachaBoxReward, GachaBoxMaxRewards)
	for i := range entries {
		entries[i] = GachaBoxReward{Stock: 2, Reward: reward}
	}
	if err := ValidateGachaBoxRewards(entries); err != nil {
		t.Fatal("reward row limit must be independent from total stock", err)
	}
	entries = append(entries, GachaBoxReward{Stock: 1, Reward: reward})
	if err := ValidateGachaBoxRewards(entries); err == nil {
		t.Fatal("too many reward rows accepted")
	}
	for name, stocks := range map[string][]int{
		"zero": {0}, "negative": {-1}, "individual upper bound": {GachaBoxMaxStock + 1},
		"total upper bound": {GachaBoxMaxStock, 1},
	} {
		t.Run(name, func(t *testing.T) {
			entries := make([]GachaBoxReward, len(stocks))
			for i, stock := range stocks {
				entries[i] = GachaBoxReward{Stock: stock, Reward: reward}
			}
			if err := ValidateGachaBoxRewards(entries); err == nil {
				t.Fatal("invalid stock accepted")
			}
		})
	}
	progress := map[int]GachaBoxProgress{70000001: {Round: 1, Remaining: []GachaBoxReward{{Stock: 99, Reward: reward}}}}
	if err := ValidateGachaBoxes(progress); err != nil {
		t.Fatal("variable remaining inventory rejected", err)
	}
}
