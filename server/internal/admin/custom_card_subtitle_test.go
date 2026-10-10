package admin

import (
	"reflect"
	"strings"
	"testing"
)

func TestCustomCardSkillSubtitleValidationAndExport(t *testing.T) {
	s := customUnitSources()
	s.Skills[0][2], s.Skills[1][2] = "原普通副名", "原职业副名"
	a := &API{catalogByKey: map[string]AdminCatalogEntry{"6:101": {ResourceState: "ready"}}}
	c, err := s.template(101)
	if err != nil {
		t.Fatal(err)
	}
	c.ID = customCardFirstID
	original := cloneCustomRows(s.Skills)
	for _, tc := range []struct {
		name  string
		text  string
		valid bool
	}{
		{"edited", "全体双攻强化", true},
		{"cleared", "", true},
		{"limit", strings.Repeat("字", 60), true},
		{"too long", strings.Repeat("字", 61), false},
		{"whitespace", "   ", false},
		{"newline", "副\n名", false},
		{"carriage return", "副\r名", false},
		{"nul", "副\x00名", false},
		{"invalid utf8", string([]byte{0xff}), false},
		{"csv separator", "副,名", false},
		{"csv quote", "副\"名", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			draft := c
			draft.Skills = cloneCustomRows(c.Skills)
			draft.Skills[0][2] = tc.text
			err := a.validateCustomCards(&customCardDraft{Cards: []customCard{draft}}, s)
			if !tc.valid {
				if err == nil {
					t.Fatal("accepted invalid subtitle")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			_, skills, _, _, err := materializeCustomCard(draft, s)
			if err != nil {
				t.Fatal(err)
			}
			csvText, err := rewriteCustomCSV("", map[string][][]string{skills[0][0]: {skills[0]}, skills[1][0]: {skills[1]}})
			if err != nil {
				t.Fatal(err)
			}
			rows, err := customCSVRows([]byte(csvText))
			if err != nil || len(rows) != 2 {
				t.Fatalf("exported rows: %v, %v", rows, err)
			}
			for _, row := range rows {
				want := "原职业副名"
				if row[0] == skills[0][0] {
					want = tc.text
				}
				if row[2] != want {
					t.Fatalf("exported subtitle = %q, want %q", row[2], want)
				}
			}
			if !reflect.DeepEqual(s.Skills, original) {
				t.Fatal("subtitle edit changed the source template")
			}
		})
	}
}
