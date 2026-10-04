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
		"not 50 slots":   func(p *GachaProfile) { p.BoxRounds[4].Rewards[0].Stock = 49 },
		"missing loop":   func(p *GachaProfile) { p.BoxRounds = p.BoxRounds[:10] },
		"multiple draws": func(p *GachaProfile) { p.CardNum, p.CardNumMax = 10, 10 },
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
}
