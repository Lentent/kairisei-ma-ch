package multiplayer

import "testing"

func TestCustomRoleChainPrerequisite(t *testing.T) {
	row := make([]string, 34)
	row[0], row[8], row[33] = "1900000001", "ATTACK_AA", "3"
	role, err := parseCombatSkillRole(row)
	if err != nil || role.MinChain != 3 {
		t.Fatalf("parse: %+v %v", role, err)
	}
	// An unmet prerequisite must return before accessing actors or applying effects.
	var engine *BattleEngine
	if results, err := engine.executePlayerRole(battleAction{}, role, 2); err != nil || len(results) != 0 {
		t.Fatalf("unmet condition: %v %v", results, err)
	}
	row[33] = "5"
	if _, err := parseCombatSkillRole(row); err == nil {
		t.Fatal("accepted invalid chain")
	}
	row = row[:33]
	role, err = parseCombatSkillRole(row)
	if err != nil || role.MinChain != 0 {
		t.Fatal("legacy role changed")
	}
}
