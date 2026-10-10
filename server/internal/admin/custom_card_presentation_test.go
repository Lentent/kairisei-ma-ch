package admin

import (
	"encoding/json"
	"reflect"
	"strconv"
	"testing"
)

func customPresentationFixture(t *testing.T) (*API, customCardSources, customCard) {
	t.Helper()
	s := customUnitSources()
	s.Cards[101][35] = "1"
	other := append([]string(nil), s.Cards[101]...)
	other[0], other[26], other[27], other[35] = "102", "21", "22", "3"
	s.Cards[102] = other
	card := s.Master.CardTemplates[0]
	card.CardID = 102
	s.Master.CardTemplates = append(s.Master.CardTemplates, card)
	s.Master.DeckRankPolicy.Cards[102] = s.Master.DeckRankPolicy.Cards[101]
	for _, skill := range s.Skills {
		skill[10], skill[19] = "ATTACK", "ENEMY_ONE"
	}
	for i, skill := range cloneCustomRows(s.Skills) {
		skill[0], skill[49] = strconv.Itoa(21+i), strconv.Itoa(21+i)
		s.Skills = append(s.Skills, skill)
	}
	for i, role := range cloneCustomRows(s.Roles) {
		role[0] = strconv.Itoa(21 + i)
		copy(role[1:8], []string{"chosen2d", "enemycutin", "chosen3d", "chosenhit", "TARGET", "chosencharge", "chosenmovie"})
		s.Roles = append(s.Roles, role)
	}
	c, err := s.template(101)
	if err != nil {
		t.Fatal(err)
	}
	c.ID = customCardFirstID
	a := &API{catalogByKey: map[string]AdminCatalogEntry{"6:101": {ResourceState: "ready"}, "6:102": {ResourceState: "ready"}}}
	return a, s, c
}

func TestCustomPresentationCutinAndActionAreIndependent(t *testing.T) {
	a, s, c := customPresentationFixture(t)
	before := cloneCustomRows(c.Roles)
	c.CutinTemplateID = 102
	row, _, roles, _, err := materializeCustomCard(c, s)
	if err != nil || row[35] != "3" || !reflect.DeepEqual(roles[0][1:8], before[0][1:8]) {
		t.Fatalf("cut-in selection must only change card style: %v", err)
	}
	c.CutinTemplateID = 0
	c.ActionSources = []customCardActionSource{{FunctionID: 11, CardID: 102, SourceFunctionID: 21}}
	c.Roles[0][20] = "777"
	if err := a.validateCustomCards(&customCardDraft{[]customCard{c}}, s); err != nil {
		t.Fatal(err)
	}
	row, _, roles, _, err = materializeCustomCard(c, s)
	if err != nil || row[35] != "1" || roles[0][1] != "chosen2d" || roles[0][3] != "chosen3d" || roles[0][20] != "777" || !reflect.DeepEqual(roles[1][1:8], before[1][1:8]) {
		t.Fatalf("action selection changed cut-in, power or another group: %v", err)
	}
	if !reflect.DeepEqual(c.Roles[0][1:8], before[0][1:8]) || row[36] != s.Cards[101][36] {
		t.Fatal("presentation export mutated draft or borrowed source artwork")
	}
	c.CutinTemplateID = 102
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var restored customCard
	if err := json.Unmarshal(raw, &restored); err != nil || restored.CutinTemplateID != 102 || !reflect.DeepEqual(restored.ActionSources, c.ActionSources) {
		t.Fatal("presentation selections did not survive draft JSON")
	}
	restored.CutinTemplateID, restored.ActionSources = 0, nil
	row, _, roles, _, err = materializeCustomCard(restored, s)
	if err != nil || row[35] != "1" || !reflect.DeepEqual(roles[0][1:8], before[0][1:8]) {
		t.Fatal("restoring template must reset both independent selections")
	}
}

func TestCustomPresentationRejectsInvalidOrIncompatibleSources(t *testing.T) {
	for _, name := range []string{"missing-cutin", "missing-card", "missing-function", "foreign-group", "duplicate", "incomplete", "kind", "target", "unavailable"} {
		t.Run(name, func(t *testing.T) {
			a, s, c := customPresentationFixture(t)
			c.ActionSources = []customCardActionSource{{FunctionID: 11, CardID: 102, SourceFunctionID: 21}}
			switch name {
			case "missing-cutin":
				c.CutinTemplateID = 999
			case "missing-card":
				c.ActionSources[0].CardID = 999
			case "missing-function":
				c.ActionSources[0].SourceFunctionID = 999
			case "foreign-group":
				c.ActionSources[0].FunctionID = 999
			case "duplicate":
				c.ActionSources = append(c.ActionSources, c.ActionSources[0])
			case "incomplete":
				s.Roles[2][3] = ""
			case "kind":
				s.Skills[2][10] = "HEAL"
			case "target":
				s.Skills[2][19] = "USER_ALL"
			case "unavailable":
				a.catalogByKey["6:102"] = AdminCatalogEntry{ResourceState: "unavailable"}
			}
			if err := a.validateCustomCards(&customCardDraft{[]customCard{c}}, s); err == nil {
				t.Fatal("invalid presentation reference accepted")
			}
		})
	}
}
