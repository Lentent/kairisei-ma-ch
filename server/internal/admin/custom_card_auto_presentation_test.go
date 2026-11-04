package admin

import (
	"os"
	"reflect"
	"testing"
)

func TestCustomHealingPresentation(t *testing.T) {
	mkSkill := func(id string) []string {
		r := make([]string, 50)
		r[0], r[49], r[10], r[19], r[11] = id, id, "SORCERY", "ENEMY_ALL", "FIRE"
		return r
	}
	mkRole := func(id, code, script string) []string {
		r := make([]string, 33)
		r[0], r[8], r[1], r[3], r[20] = id, code, script, "native_playlist", "4004"
		return r
	}
	source := customCardSources{Skills: [][]string{mkSkill("12"), mkSkill("13")}, Roles: [][]string{mkRole("12", "ATTACK_AA", "player_skill_magic_heal_0c"), mkRole("13", "ATTACK_AA", "player_skill_magic_buff_heal_0sp")}}
	for _, buff := range []bool{false, true} {
		group := [][]string{mkRole("1", "ATTACK_AA", "player_skill_magic_buff_0c"), mkRole("1", "HEAL_FIXED", "")}
		if buff {
			group = append(group, mkRole("1", "ATK_UP_FIXED", ""))
		}
		before := cloneCustomRows(group)
		if err := resolveCustomHealingPresentation([][]string{mkSkill("1")}, group, source); err != nil {
			t.Fatal(err)
		}
		if !customDirectionSupportsHeal(group[0][1], buff) {
			t.Fatal("incomplete direction")
		}
		for i, r := range group {
			if !reflect.DeepEqual(r[8:], before[i][8:]) || !reflect.DeepEqual(r[1:8], group[0][1:8]) {
				t.Fatal("changed effect or inconsistent direction")
			}
		}
		again := cloneCustomRows(group)
		if err := resolveCustomHealingPresentation([][]string{mkSkill("1")}, group, source); err != nil || !reflect.DeepEqual(group, again) {
			t.Fatal("not idempotent")
		}
	}
	group := [][]string{mkRole("1", "ATTACK_AA", "player_skill_magic_buff_0c"), mkRole("1", "HEAL_FIXED", "")}
	if resolveCustomHealingPresentation([][]string{mkSkill("1")}, group, customCardSources{}) == nil {
		t.Fatal("silently exported broken animation")
	}
	group = group[:1]
	before := cloneCustomRows(group)
	if err := resolveCustomHealingPresentation([][]string{mkSkill("1")}, group, source); err != nil || !reflect.DeepEqual(group, before) {
		t.Fatal("changed non-healing card")
	}
}
func TestCustomHealingPresentationRealResource(t *testing.T) {
	root := os.Getenv("CN602_CUSTOM_CARD_RESOURCE_SET")
	if root == "" {
		t.Skip("requires resources")
	}
	a := &API{collectionResourceRoot: root}
	s, err := a.customCardSources()
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.template(10108005)
	if err != nil {
		t.Fatal(err)
	}
	c.ID = customCardFirstID
	donor, err := s.template(10108007)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range donor.Roles {
		if r[8] == "HEAL_FIXED" {
			copyRole := append([]string(nil), r...)
			copyRole[0] = c.Skills[1][49]
			c.Roles = append(c.Roles, copyRole)
			c.RoleSources = append(c.RoleSources, customRoleSource{CardID: 10108007, Index: i})
			break
		}
	}
	_, skills, roles, _, err := materializeCustomCard(c, s)
	if err != nil {
		t.Fatal(err)
	}
	if err = resolveCustomHealingPresentation(skills, roles, s); err != nil {
		t.Fatal(err)
	}
	for _, r := range roles {
		if r[8] == "HEAL_FIXED" && !customDirectionSupportsHeal(r[1], true) {
			t.Fatal("real attack/buff/heal direction incomplete")
		}
	}
	if _, err = a.buildCustomCardPatch(customCardDraft{Cards: []customCard{c}}, s); err != nil {
		t.Fatal(err)
	}
}
