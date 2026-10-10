package admin

import (
	"reflect"
	"testing"
)

func TestCustomCardTargetEditingValidationAndExport(t *testing.T) {
	for _, tc := range []struct {
		name, kind, function, skillTarget, effectTarget string
		valid                                           bool
	}{
		{"all attack", "ATTACK", "ATTACK_AA", "ENEMY_ALL", "SELECT", true},
		{"single attack override", "ATTACK", "ATTACK_AA", "ENEMY_ALL", "ENEMY_ONE", true},
		{"all attack override", "ATTACK", "ATTACK_AA", "ENEMY_ONE", "ENEMY_ALL", true},
		{"all allies", "SUPPORT", "ATK_UP_FIXED", "USER_ALL", "SELECT", true},
		{"single ally", "SUPPORT", "ATK_UP_FIXED", "USER_ONE", "USER_ONE", true},
		{"self override", "SUPPORT", "ATK_UP_FIXED", "USER_ALL", "SELF", true},
		{"all allies override", "SUPPORT", "ATK_UP_FIXED", "SELF", "USER_ALL", true},
		{"legacy all allies", "SUPPORT", "ATK_UP_FIXED", "USER_ALL", "FRIEND_ALL", true},
		{"enemy debuff", "JAMMING", "ATK_BREAK_FIXED", "ENEMY_ALL", "ENEMY_ALL", true},
		{"invalid skill target", "SUPPORT", "ATK_UP_FIXED", "UNKNOWN", "SELECT", false},
		{"invalid effect target", "SUPPORT", "ATK_UP_FIXED", "USER_ALL", "UNKNOWN", false},
		{"attack on allies", "ATTACK", "ATTACK_AA", "USER_ALL", "SELECT", false},
		{"attack effect on allies", "ATTACK", "ATTACK_AA", "ENEMY_ONE", "USER_ALL", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := customUnitSources()
			for _, skill := range s.Skills {
				skill[10], skill[19] = tc.kind, "ENEMY_ONE"
				if tc.kind != "ATTACK" {
					skill[19] = "SELF"
				}
			}
			s.Rules[tc.function] = s.Rules["ATTACK_AA"]
			for _, role := range s.Roles {
				role[8], role[9] = tc.function, "SELECT"
			}
			c, err := s.template(101)
			if err != nil {
				t.Fatal(err)
			}
			c.ID = customCardFirstID
			originalSkills, originalRoles := cloneCustomRows(s.Skills), cloneCustomRows(s.Roles)
			c.Skills[0][19], c.Roles[0][9] = tc.skillTarget, tc.effectTarget
			a := &API{catalogByKey: map[string]AdminCatalogEntry{"6:101": {ResourceState: "ready"}}}
			err = a.validateCustomCards(&customCardDraft{Cards: []customCard{c}}, s)
			if !tc.valid {
				if err == nil {
					t.Fatal("accepted invalid target")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			_, skills, roles, _, err := materializeCustomCard(c, s)
			if err != nil {
				t.Fatal(err)
			}
			for _, exported := range []struct {
				rows  [][]string
				index int
				want  string
			}{{skills, 19, tc.skillTarget}, {roles, 9, tc.effectTarget}} {
				text, err := rewriteCustomCSV("", map[string][][]string{exported.rows[0][0]: {exported.rows[0]}})
				if err != nil {
					t.Fatal(err)
				}
				rows, err := customCSVRows([]byte(text))
				if err != nil || len(rows) != 1 || rows[0][exported.index] != exported.want {
					t.Fatalf("exported target: %v, %v", rows, err)
				}
			}
			if skills[1][19] != s.Skills[1][19] || roles[1][9] != s.Roles[1][9] || !reflect.DeepEqual(s.Skills, originalSkills) || !reflect.DeepEqual(s.Roles, originalRoles) {
				t.Fatal("target edit changed another skill or source template")
			}
		})
	}
}

func TestCustomCardTargetBranchesAndActionSource(t *testing.T) {
	a, s, c := customPresentationFixture(t)
	branch := append([]string(nil), s.Skills[0]...)
	branch[49] = "12"
	s.Skills = append(s.Skills, branch)
	c, err := s.template(101)
	if err != nil {
		t.Fatal(err)
	}
	c.ID = customCardFirstID
	c.Skills[0][19] = "ENEMY_ALL"
	if err := a.validateCustomCards(&customCardDraft{Cards: []customCard{c}}, s); err == nil {
		t.Fatal("accepted inconsistent targets for one skill root")
	}
	c.Skills[2][19] = "ENEMY_ALL"
	c.ActionSources = []customCardActionSource{{FunctionID: 11, CardID: 102, SourceFunctionID: 21}}
	if err := a.validateCustomCards(&customCardDraft{Cards: []customCard{c}}, s); err == nil {
		t.Fatal("accepted action with the old target scope")
	}
	s.Skills[2][19] = "ENEMY_ALL" // Source card 102's normal skill.
	if err := a.validateCustomCards(&customCardDraft{Cards: []customCard{c}}, s); err != nil {
		t.Fatalf("action compatible with edited scope was rejected: %v", err)
	}
	c.Skills[0][36] = "UNKNOWN_CONDITION"
	if err := a.validateCustomCards(&customCardDraft{Cards: []customCard{c}}, s); err == nil {
		t.Fatal("target editing unlocked trigger conditions")
	}
}
