package admin

import (
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestCustomCardBorrowedEffectUsesSourceBranchAttribute(t *testing.T) {
	s := customUnitSources()
	s.Rules["ATTACK_AA"][7] = "ATTR"
	// The second template is ICE, but its second skill branch is WIND.
	row := append([]string(nil), s.Cards[101]...)
	row[0], row[26], row[27] = "102", "21", "22"
	s.Cards[102] = row
	card := s.Master.CardTemplates[0]
	card.CardID = 102
	s.Master.CardTemplates = append(s.Master.CardTemplates, card)
	s.Master.DeckRankPolicy.Cards[102] = gamestate.CardRankRule{ArthurType: 1}
	for i, id := range []string{"21", "22"} {
		skill := append([]string(nil), s.Skills[i]...)
		skill[0], skill[49], skill[11] = id, id, []string{"ICE", "WIND"}[i]
		role := append([]string(nil), s.Roles[i]...)
		role[0], role[27] = id, skill[11]
		s.Skills = append(s.Skills, skill)
		s.Roles = append(s.Roles, role)
	}
	c, err := s.template(101)
	if err != nil {
		t.Fatal(err)
	}
	c.ID, c.Attribute = customCardFirstID, "LIGHT"
	for i, original := range s.Roles[2:] {
		role := append([]string(nil), original...)
		role[0] = c.Roles[0][0]
		c.Roles = append(c.Roles, role)
		c.RoleSources = append(c.RoleSources, customRoleSource{CardID: 102, Index: i})
	}
	// An explicit off-element parameter must remain as authored by the source.
	role := append([]string(nil), s.Roles[2]...)
	role[0], role[27] = c.Roles[0][0], "DARK"
	s.Roles = append(s.Roles, append([]string(nil), role...))
	s.Roles[4][0] = "21"
	c.Roles = append(c.Roles, role)
	c.RoleSources = append(c.RoleSources, customRoleSource{CardID: 102, Index: 2})
	a := &API{catalogByKey: map[string]AdminCatalogEntry{"6:101": {ResourceState: "ready"}, "6:102": {ResourceState: "ready"}}}
	if err := a.validateCustomCards(&customCardDraft{Cards: []customCard{c}}, s); err != nil {
		t.Fatal(err)
	}
	_, _, roles, _, err := materializeCustomCard(c, s)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"LIGHT", "LIGHT", "DARK"} {
		if got := roles[2+i][27]; got != want {
			t.Fatalf("borrowed effect %d attribute = %s, want %s", i, got, want)
		}
	}
}
