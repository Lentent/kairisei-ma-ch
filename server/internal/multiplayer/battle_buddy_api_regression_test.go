package multiplayer

import (
	"reflect"
	"testing"
)

func TestBuddyHandFilterAndSharedCardProjection(t *testing.T) {
	engine, _ := nextBattleFixture(t)
	engine.catalog.Cards[1] = CombatCardDefinition{ID: 1, NormalSkillID: 1, ProfileTags: [8]int{11, 22}}
	engine.catalog.Cards[2] = CombatCardDefinition{ID: 2, NormalSkillID: 1, ProfileTags: [8]int{11}}
	player := &engine.players[0]
	player.Hand = [5]int{1, 2}
	player.Deck[1].CardID = 2
	// 79358 tests each positive include-tag independently and does not reject
	// a sealed card merely because this is a Buddy hand-targeting operation.
	player.Effects = []battleEffect{{Function: "CARD_SEAL", CardType: 1, Remaining: 1}}
	filter := CombatSkillDefinition{ID: 7, Target: "HAND_ALL", HandAttribute: "FIRE", Groups: [3]int{11, 22}}
	_, cards, err := engine.selectBurstSkillVariant(player, []CombatSkillDefinition{filter}, burstSkillSubmission{})
	if err != nil || !reflect.DeepEqual(cards, []int{1}) {
		t.Fatalf("HAND_ALL filter: cards=%v err=%v", cards, err)
	}
	filter.Target, filter.HandSelectCount = "HAND_SELECT", 1
	if _, _, err := engine.selectBurstSkillVariant(player, []CombatSkillDefinition{filter}, burstSkillSubmission{CardTypes: [5]int{1}}); err != nil {
		t.Fatalf("same selected sealed card must pass: %v", err)
	}
	filter.Target, filter.Groups[2] = "HAND_ALL", 22
	_, cards, err = engine.selectBurstSkillVariant(player, []CombatSkillDefinition{filter}, burstSkillSubmission{})
	if err != nil || len(cards) != 0 {
		t.Fatalf("exclude-tag filter: cards=%v err=%v", cards, err)
	}
	player.CardBurstSkills[1], player.CardCostDown[1] = []int{7, 8}, 2
	if got := playerCardStateResult(player, 1).Args; !reflect.DeepEqual(got, []int64{1, 1, 5, 1}) {
		t.Fatalf("seal and Buddy state must coexist: %v", got)
	}
	if got := engine.playerCardCostResult(player, 1, 1).Args; !reflect.DeepEqual(got, []int64{1, 1, 0, 2}) {
		t.Fatalf("discount is not the clamped cost: %v", got)
	}
}

func TestBuddyGaugeDamageAndUnavailableStates(t *testing.T) {
	engine := &BattleEngine{catalog: &CombatCatalog{BurstGauge: CombatBurstGaugeConfig{Maximum: 300, DamageReduction: 50}}}
	for _, tc := range []struct {
		name                                   string
		state, gauge, damage, breakTurns, want int
		gameOver                               bool
		rows                                   int
	}{
		{"normal does not lose gauge", burstGaugeNormal, 100, 100, 0, 100, false, 0},
		{"positive damage minimum", burstGaugeBurst, 100, 1, 0, 99, false, 1},
		{"role total damage", burstGaugeBurst, 100, 199, 0, 91, false, 1},
		{"zero gauge is not revived", burstGaugeBurst, 0, 100, 0, 0, false, 0},
		{"break cooldown", burstGaugeBurst, 100, 100, 1, 100, false, 0},
		{"retired player", burstGaugeBurst, 100, 100, 0, 100, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := battlePlayer{MemberType: 1, MaxHP: 1000, BurstState: tc.state, Burst: tc.gauge, BurstBreak: tc.breakTurns, GameOver: tc.gameOver}
			rows := engine.burstDamageGaugeResults(&p, tc.damage)
			if p.Burst != tc.want || len(rows) != tc.rows {
				t.Fatalf("gauge=%d rows=%v", p.Burst, rows)
			}
		})
	}
	engine.players[0] = battlePlayer{MemberType: 1, BurstState: burstGaugeBurst}
	if rows := engine.addPlayerBurstGauge(1, 0, 30); len(rows) != 0 || engine.players[0].Burst != 0 {
		t.Fatalf("zero BURST gauge accepted quick-up: %v", rows)
	}
}

func TestHeldCardConsumesBurstModifierBeforeRelease(t *testing.T) {
	engine, _ := nextBattleFixture(t)
	engine.enemies[0].HP, engine.enemies[0].MaxHP = 10000, 10000
	skill := engine.catalog.PlayerSkills[1][0]
	skill.DamageKind = "PHYSICS"
	engine.catalog.PlayerSkills[1][0] = skill
	engine.catalog.BurstSkills = map[int][]CombatSkillDefinition{2: {{ID: 2, FunctionID: 2}}}
	engine.catalog.BurstSkillRoles = map[int][]CombatSkillRole{2: {{Function: "ATTACK_MULTISTAGE", Parameters: [10]string{"", "PHYSICS", "3"}}}}
	player := &engine.players[0]
	player.CardBurstSkills[1], player.CardCostDown[1] = []int{2}, 1
	action := battleAction{memberType: 1, cardType: 1, cardID: 1, cardLevel: 1, target: 5, skill: skill}
	player.CardHolds = []battleCardHold{{Action: action, Remaining: 1}}
	// 664ee releases to the trash first, executes with modifiers, then clears
	// them through 45726. Redrawing must restore the original cost and hit count.
	rows, err := engine.executeCardHolds("")
	if err != nil {
		t.Fatal(err)
	}
	if damage := 10000 - engine.enemies[0].HP; damage != 600 {
		t.Fatalf("held skill lost its modifier before execution: damage=%d rows=%v", damage, rows)
	}
	if len(player.CardHolds) != 0 || !reflect.DeepEqual(player.Discard, []int{0}) {
		t.Fatal("held card did not return to the discard pool")
	}
	player.Hand[0], player.Discard = 1, nil
	if cost := engine.effectiveCardCost(player, 1, skill.Cost); cost != 1 {
		t.Fatalf("redrawn held card retained its discount: cost=%d", cost)
	}
	if _, err := engine.executePlayerAction(action, nil); err != nil {
		t.Fatal(err)
	}
	if damage := 9400 - engine.enemies[0].HP; damage != 200 {
		t.Fatalf("redrawn held card retained its hit modifier: damage=%d", damage)
	}
}
