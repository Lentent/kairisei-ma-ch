package multiplayer

import (
	"kairisei.local/server/internal/gamestate"
	"testing"
)

func TestCustomBossStatsAreFrozenAndAppliedToEveryWave(t *testing.T) {
	base, members := nextBattleFixture(t)
	rates := [9]int{100, 50, 100, 100, 100, 100, 100, 100, 100}
	rows := []gamestate.TeamBattleEnemyOverride{{BattleIndex: 0, EnemyIndex: 0, Stats: gamestate.TeamBattleEnemyStats{EnemyID: 1, HP: 1234, Attack: 345, Magic: 456, Defense: 67, MagicDefense: 89, Attribute: "ICE", AttributeRates: &rates}}, {BattleIndex: 1, EnemyIndex: 0, Stats: gamestate.TeamBattleEnemyStats{EnemyID: 1, HP: 5678, Attack: 789, Attribute: "FIRE"}}}
	engine, err := newBattleEngine(base.catalog, RoomSpec{EnemyPartyID: 1, CostInitial: 3, HoldMax: 5, Seed: 602, EnemyOverrides: rows}, members)
	if err != nil {
		t.Fatal(err)
	}
	rows[0].Stats.HP = 1
	rates[1] = 0
	if engine.enemies[0].MaxHP != 1234 || engine.enemies[0].Attack != 345 || engine.enemies[0].BaseAttribute != "ICE" || engine.enemies[0].Level.AttributeRates[1] != 50 {
		t.Fatal("copy stats were not frozen or applied")
	}
	results, err := engine.Start()
	if err != nil {
		t.Fatal(err)
	}
	seenParam, seenAttribute := false, false
	for _, r := range results {
		if r.Command == resultBattleParam && r.Args[0] == 5 {
			seenParam = r.Args[1] == 1234 && r.Args[3] == 345
		}
		if r.Command == resultRewrite && r.Args[0] == 5 {
			seenAttribute = r.Args[1] == 2
		}
	}
	if !seenParam || !seenAttribute {
		t.Fatal("client did not receive copy HP, attack and attribute")
	}
	engine.phase, engine.endType = battlePhaseEnded, 1
	next, err := engine.NextBattle(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if next.enemies[0].MaxHP != 5678 || next.enemies[0].Attack != 789 || engine.enemies[0].MaxHP != 1234 {
		t.Fatal("repeated source party lost per-wave stats or mutated previous engine")
	}
	if base.catalog.Enemies[1].HP != 100 || base.catalog.Enemies[1].Attribute != "FIRE" || base.catalog.EnemyLevels[1].AttributeRates[1] != 100 {
		t.Fatal("copy changed the shared template")
	}
	rows[0].Stats.EnemyID = 999
	if _, err = newBattleEngine(base.catalog, RoomSpec{EnemyPartyID: 1, CostInitial: 3, HoldMax: 5, EnemyOverrides: rows}, members); err == nil {
		t.Fatal("unrelated enemy identity accepted")
	}
}
