package admin

import "testing"

func TestNativeCustomChainBranches(t *testing.T) {
	skill := make([]string, 50)
	skill[0], skill[49] = "1900000000", "1900000000"
	role := make([]string, 34)
	role[0], role[8], role[20], role[33] = skill[0], "ATTACK_AA", "100", "2"
	free := append([]string(nil), role...)
	free[33] = "0"
	skills, roles, err := compileCustomCardChains(customCardFirstID, [][]string{skill}, [][]string{role, free})
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 2 {
		t.Fatalf("branches: %d", len(skills))
	}
	for _, chain := range []int{0, 1, 2, 3, 4} {
		var selected []string
		for _, s := range skills {
			if chain >= customRowInt(s, 37) && chain <= customRowInt(s, 38) {
				selected = s
			}
		}
		if selected == nil {
			t.Fatalf("chain %d has no branch", chain)
		}
		var group [][]string
		for _, r := range roles {
			if r[0] == selected[49] {
				group = append(group, r)
			}
		}
		client := customClientRoleRows(group)
		if len(client) != 2 || client[1][8] != "ATTACK_AA" {
			t.Fatal("unconditional effect lost")
		}
		want := "ATTACK_AA"
		if chain < 2 {
			want = "ATK_OP_PIERCING"
		}
		if client[0][8] != want {
			t.Fatalf("chain %d: %s", chain, client[0][8])
		}
		if len(client[0]) != 33 {
			t.Fatal("client extension leaked")
		}
	}
	skill[36], skill[37], skill[38] = "TURN", "2", "3"
	skills, _, err = compileCustomCardChains(customCardFirstID, [][]string{skill}, [][]string{role})
	if err != nil || skills[0][36] != "TURN" || skills[0][42] != "DECK_COMBO_COUNT" {
		t.Fatalf("template condition lost: %v", err)
	}
	skill[42] = "SELF_HP_PER"
	if _, _, err = compileCustomCardChains(customCardFirstID, [][]string{skill}, [][]string{role}); err == nil {
		t.Fatal("overwrote template condition")
	}
	skill[36], skill[42] = "", ""
	if _, _, err = compileCustomCardChains(customCardFirstID, [][]string{skill, skill, skill}, [][]string{role}); err == nil {
		t.Fatal("accepted native branch overflow")
	}
}

func TestCustomChainPreservesDisplayAndTemplateConditions(t *testing.T) {
	skill := make([]string, 50)
	skill[0], skill[49], skill[5], skill[18] = "1900000000", "1900000000", "1", "100"
	attack := make([]string, 33)
	attack[0], attack[8], attack[20] = skill[0], "ATTACK_AA", "100"
	marker := customPowerBoostRole(1900000000)
	branches, _, err := compileCustomCardChains(customCardFirstID, [][]string{skill}, [][]string{attack, append(marker, "4")})
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 2 {
		t.Fatal(branches)
	}
	for _, b := range branches {
		if b[5] != "1" || b[18] != "100" {
			t.Fatal("lost first role display or hate", b)
		}
	}
	skill[36], skill[42] = "TURN", "SELF_HP_PER"
	branches, roles, err := compileCustomCardChains(customCardFirstID, [][]string{skill, skill}, [][]string{attack, customPowerBoostRole(1900000000)})
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 2 || branches[0][36] != "TURN" || branches[0][42] != "SELF_HP_PER" || roles[0][20] != "180" {
		t.Fatal("unconditional boost consumed a condition or scaled twice", branches, roles)
	}
}
