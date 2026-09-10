package multiplayer

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestSupportLoveAndNativeOpeningLifecycle(t *testing.T) {
	base, members := nextBattleFixture(t)
	c := base.catalog
	c.SupportLoveRates = [3]int{25, 50, 100}
	c.Cards[2] = CombatCardDefinition{ID: 2, LoveMax: 10000, SupportSkillIDs: [4]int{0, 11, 12, 13}}
	c.SupportSkills = map[int][]CombatSkillDefinition{}
	c.SupportSkillRoles = map[int][]CombatSkillRole{}
	for _, id := range []int{11, 12, 13} {
		c.SupportSkills[id] = []CombatSkillDefinition{{ID: id, FunctionID: id, Target: "SELF"}}
		c.SupportSkillRoles[id] = []CombatSkillRole{{Function: "DAMAGE_BOOST", Target: "SELECT", Parameters: [10]string{"99", "1000", "", "", "", "FIRE_DARK", "PHYSICS", "0", "0"}}}
	}
	for _, sample := range []struct{ love, want int }{{0, 0}, {2499, 0}, {2500, 11}, {4999, 11}, {5000, 12}, {9999, 12}, {10000, 13}, {10001, 13}} {
		skill, _, err := c.CardSupportSkill(BattleCard{CardID: 2, Level: 1, Love: sample.love})
		if err != nil || skill.ID != sample.want {
			t.Fatalf("love %d: skill=%d err=%v", sample.love, skill.ID, err)
		}
	}
	members[0].SupportCards = []BattleCard{{CardType: 12, CardID: 2, Level: 60, Love: 10000}}
	copyMember := cloneMember(members[0])
	copyMember.SupportCards[0].Love = 0
	if members[0].SupportCards[0].Love != 10000 {
		t.Fatal("room snapshot shares support deck")
	}
	rows, err := roomDeckRows(&room{RoomSnapshot: RoomSnapshot{Members: members}})
	if err != nil || !strings.Contains(strings.Join(rows, "\n"), "23,1,12,2,60") {
		t.Fatalf("support identity not sent before start: %v", err)
	}
	engine, err := newBattleEngine(c, RoomSpec{EnemyPartyID: 1, Seed: 602, HoldMax: 5, CostInitial: 3}, members)
	if err != nil {
		t.Fatal(err)
	}
	wantRNG := engine.rng
	// Start now owns the four opening shuffles (nine native words each).
	// This DAMAGE_BOOST passive itself must not consume another word.
	for range 36 {
		wantRNG.next()
	}
	results, err := engine.Start()
	if err != nil {
		t.Fatal(err)
	}
	if engine.rng != wantRNG || len(engine.players[0].Effects) != 1 || len(engine.players[1].Effects) != 0 {
		t.Fatal("support setup consumed extra RNG or leaked SELF effects")
	}
	foundHeader := false
	for _, row := range results {
		if row.Command == resultCardSkill {
			foundHeader = reflect.DeepEqual(row.Args, []int64{1, 12, 13, 1, 60, 0, 0, 0, 13, 0, 1, 0})
		}
	}
	if !foundHeader {
		t.Fatal("native passive CARD_SKILL header missing")
	}
	skill, roles, err := c.CardSkill(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	engine.enemies[0].HP, engine.enemies[0].MaxHP = 10000, 10000
	_, err = engine.executePlayerAttack(battleAction{memberType: 1, cardID: 1, cardType: 1, cardLevel: 1, target: 5, skill: skill, roles: roles}, roles[0], 1)
	if err != nil || engine.enemies[0].HP != 9600 {
		t.Fatalf("EX damage not used by actual attack: hp=%d err=%v", engine.enemies[0].HP, err)
	}
	resume, err := engine.ResumeResults()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range resume {
		if row.Command == resultResumeBuff && row.Args[1] == 1 {
			t.Fatal("resume incorrectly serialized PASSIVE as a temporary UI buff")
		}
	}
	if effect := engine.players[0].Effects[0]; effect.Attribute != "FIRE_DARK" || effect.Rate != 1000 {
		t.Fatal("read-only resume lost passive state")
	}
	engine.turn = 100
	if _, err := engine.tickPersistentEffects(); err != nil {
		t.Fatal(err)
	}
	if len(engine.players[0].Effects) != 1 || engine.players[0].Effects[0].Remaining != 99 {
		t.Fatal("EX passive incorrectly counted down with turn buffs")
	}
	engine.phase, engine.endType = battlePhaseEnded, 1
	next, err := engine.NextBattle(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := next.Start(); err != nil {
		t.Fatal(err)
	}
	if len(next.players[0].Effects) != 1 || next.players[0].SupportDeck != engine.players[0].SupportDeck || len(engine.players[0].Effects) != 1 {
		t.Fatal("wave handover duplicated/lost support or mutated preceding state")
	}
}

func TestBeginningDrawPassiveStartupAndResume(t *testing.T) {
	engine, _ := nextBattleFixture(t)
	c := engine.catalog
	card := c.Cards[1]
	card.PassiveSkillID = 99
	c.Cards[1] = card
	c.SupportSkills = map[int][]CombatSkillDefinition{99: {{ID: 99, FunctionID: 99, Target: "SELF"}}}
	c.SupportSkillRoles = map[int][]CombatSkillRole{99: {{Function: "BEGINNING_DRAW", Target: "SELECT"}}}
	rows, err := engine.Start()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, row := range rows {
		if row.Command == resultCardSkill && row.Args[2] == 99 {
			count++
		}
	}
	if count != 40 {
		t.Fatalf("main-card startup passive headers=%d, want 40", count)
	}
	engine.turn = 2
	if _, err = engine.tickPersistentEffects(); err != nil {
		t.Fatal(err)
	}
	for _, player := range engine.players {
		if len(player.Effects) != 10 {
			t.Fatal("zero-duration native PASSIVE disappeared at turn end")
		}
		for i, effect := range player.Effects {
			if effect.CardType != i+1 || effect.Remaining != 0 || effect.Function != "BEGINNING_DRAW" {
				t.Fatalf("bad card-bound passive: %+v", effect)
			}
		}
	}
	resume, err := engine.ResumeResults()
	if err != nil {
		t.Fatal(err)
	}
	count = 0
	for _, row := range resume {
		if row.Command == resultResumeBuff && row.Args[2] == 412 {
			count++
		}
	}
	if count != 0 || len(engine.players[0].Effects) != 10 {
		t.Fatalf("native resume must retain passive state without projecting RESUME_BUFF, got %d rows", count)
	}
}

func TestNativeResumeBuffListBoundary(t *testing.T) {
	for list := 0; list < 8; list++ {
		_, ok := resumeBuffResult(1, battleEffect{Function: "ATK_UP_FIXED", ListType: list, Remaining: 3})
		want := list == 0 || list == 3 || list == 5 || list == 6
		if ok != want {
			t.Fatalf("native RESUME_BUFF list %d inclusion=%v want %v", list, ok, want)
		}
	}
}

func TestSupportHealCriticalAndConditions(t *testing.T) {
	engine, _ := nextBattleFixture(t)
	role := CombatSkillRole{Function: "HEAL_BOOST", Parameters: [10]string{"99", "100", "", "1000", "", "LIGHT_DARK", "2", "4"}}
	effect, err := sphereSupportEffect(role, 60, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	engine.players[0].Effects = []battleEffect{effect}
	engine.players[1].HP = 1
	skill := CombatSkillDefinition{Attribute: "LIGHT", Cost: 2, Target: "USER_ONE"}
	heal := CombatSkillRole{Function: "HEAL_FIXED", Target: "USER_ONE", Parameters: [10]string{"100", "0", "0", "0", "MND"}}
	_, err = engine.executeFixedHeal(battleAction{memberType: 1, target: 2, skill: skill, cardLevel: 60}, heal, 1)
	if err != nil || engine.players[1].HP != 401 {
		t.Fatalf("caster's heal boost not applied: HP=%d err=%v", engine.players[1].HP, err)
	}
	for _, sample := range []struct {
		attr       string
		cost, want int
	}{{"DARK", 4, 400}, {"FIRE", 2, 100}, {"LIGHT", 1, 100}, {"LIGHT", 5, 100}} {
		if got := supportHealValue(engine.players[0].Effects, CombatSkillDefinition{Attribute: sample.attr, Cost: sample.cost}, 100); got != sample.want {
			t.Fatalf("heal filter %+v got %d", sample, got)
		}
	}
	role = CombatSkillRole{Function: "CRITICAL_BOOST", Parameters: [10]string{"99", "1000", "0", "LIGHT_DARK", "MAGIC", "2", "4"}}
	effect, err = sphereSupportEffect(role, 60, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range []struct {
		attr, physics string
		cost, want    int
	}{{"LIGHT", "MAGIC", 2, 1000}, {"DARK", "MAGIC", 4, 1000}, {"LIGHT", "PHYSICS", 2, 0}, {"FIRE", "MAGIC", 2, 0}, {"LIGHT", "MAGIC", 5, 0}} {
		if got := nativeCriticalRate([]battleEffect{effect}, 0, CombatSkillDefinition{Attribute: sample.attr, DamageKind: sample.physics, Cost: sample.cost}); got != sample.want {
			t.Fatalf("critical filter %+v got %d", sample, got)
		}
	}
	effect.CostMin = 0
	if got := nativeCriticalRate([]battleEffect{effect}, 0); got != 1000 {
		t.Fatalf("native AI empty-skill critical metric lost EX boost: %d", got)
	}
	parameterBoost := battleEffect{Function: "ATK_UP_BOOST", ListType: 1, Parameter: "ATK", Attribute: "NULL", Rate: 3000, Remaining: 99}
	if got := sphereSupportBoostedValue([]battleEffect{parameterBoost, parameterBoost}, "ATK_UP_BOOST", "FIRE", "ATK", 100); got != 700 {
		t.Fatalf("parameter boosts incorrectly share the damage boost cap: %d", got)
	}
}

func TestOfficialEXSupportClosure(t *testing.T) {
	root := "../../../_local/control/server/"
	if _, err := os.Stat(root + "cn602-card-master/card.csv"); os.IsNotExist(err) {
		t.Skip("official master unavailable in source-only checkout")
	}
	c, err := LoadCombatCatalog(root+"cn602-card-master/card.csv", root+"cn602-battle-master")
	if err != nil {
		t.Fatal(err)
	}
	if c.SupportLoveRates != [3]int{25, 50, 100} {
		t.Fatalf("official love thresholds changed: %v", c.SupportLoveRates)
	}
	if err := c.ValidateCardSupportFunctionCoverage(); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"DAMAGE_BOOST": false, "ATK_UP_BOOST": false, "DEF_UP_BOOST": false, "ATK_BREAK_BOOST": false, "GUARD_BREAK_BOOST": false, "HEAL_BOOST": false, "CRITICAL_BOOST": false}
	for _, card := range c.Cards {
		_, roles, err := c.CardSupportSkill(BattleCard{CardID: card.ID, Level: card.MaxLevel, Love: card.LoveMax})
		if err != nil {
			t.Fatal(err)
		}
		for _, role := range roles {
			want[role.Function] = true
		}
	}
	for function, found := range want {
		if !found {
			t.Fatalf("official EX family %s not covered", function)
		}
	}
}
