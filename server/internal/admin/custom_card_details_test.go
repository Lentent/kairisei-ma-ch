package admin

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestCustomCardDetailsSaveAndClientExport(t *testing.T) {
	s := customUnitSources()
	row := append(s.Cards[101], make([]string, 100-len(s.Cards[101]))...)
	for i := 44; i < 100; i++ {
		row[i] = "模板资料"
	}
	s.Cards[101] = row
	original := append([]string(nil), row...)
	a := &API{catalogByKey: map[string]AdminCatalogEntry{"6:101": {ResourceState: "ready"}}}
	c, err := s.template(101)
	if err != nil {
		t.Fatal(err)
	}
	c.ID = customCardFirstID
	defaults := customCardTemplateDetails(s, []customCard{c})[101]
	for _, field := range defaults.fields() {
		if field.text == nil || *field.text != row[field.column] {
			t.Fatal("missing template detail")
		}
	}
	description, empty, credit, height := "为保护伙伴而诞生的骑士。", "", "自制画师", "165cm"
	c.Details = &customCardDetails{Description: &description, Illustrator: &credit, VoiceActor: &empty, Height: &height}
	draft := customCardDraft{Cards: []customCard{c}}
	if err := a.validateCustomCards(&draft, s); err != nil {
		t.Fatal(err)
	}
	// Exercise the same JSON persistence/transfer boundary used by drafts.
	raw, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	var saved customCardDraft
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	generated, _, _, _, err := materializeCustomCard(saved.Cards[0], s)
	if err != nil {
		t.Fatal(err)
	}
	text, err := rewriteCustomCSV("", map[string][][]string{generated[0]: {generated}})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := customCSVRows([]byte(text))
	if err != nil || len(rows) != 1 || len(rows[0]) != len(row) {
		t.Fatalf("invalid exported CSV: %v", err)
	}
	if rows[0][44] != description || rows[0][45] != credit || rows[0][46] != "" || rows[0][64] != height {
		t.Fatal("export lost edited/cleared details")
	}
	for _, i := range []int{47, 48, 51, 52, 53, 62, 63, 65, 66, 67, 79, 80, 89, 90} {
		if rows[0][i] != row[i] {
			t.Fatalf("detail edit changed inherited column %d", i)
		}
	}
	if !reflect.DeepEqual(s.Cards[101], original) {
		t.Fatal("export mutated the template")
	}
	c.Details = nil
	generated, _, _, _, err = materializeCustomCard(c, s)
	if err != nil || !reflect.DeepEqual(generated[44:67], row[44:67]) {
		t.Fatalf("old draft lost inherited details: %v", err)
	}
}

func TestCustomCardDetailsValidationAndInheritance(t *testing.T) {
	row := make([]string, 67)
	row[44] = `"历史介绍,保留"`
	for _, tc := range []struct {
		text  string
		valid bool
	}{
		{"", true}, {"新的角色介绍。", true}, {strings.Repeat("字", 2000), true},
		{strings.Repeat("字", 2001), false}, {"   ", false}, {"a,b", false},
		{"a\"b", false}, {"a\nb", false}, {"a\rb", false}, {"a\x00b", false},
		{string([]byte{0xff}), false}, {row[44], true},
	} {
		c := customCard{Details: &customCardDetails{Description: &tc.text}}
		if err := validateCustomCardDetails(c, row); (err == nil) != tc.valid {
			t.Errorf("validation mismatch for %q: %v", tc.text, err)
		}
	}
	for _, field := range []string{"illustrator", "voice_actor", "gender", "measurements", "height", "weight", "hand"} {
		for _, n := range []int{100, 101} {
			raw, _ := json.Marshal(map[string]any{"details": map[string]string{field: strings.Repeat("字", n)}})
			var c customCard
			if err := json.Unmarshal(raw, &c); err != nil {
				t.Fatal(err)
			}
			if err := validateCustomCardDetails(c, row); (err == nil) != (n == 100) {
				t.Errorf("%s length %d: %v", field, n, err)
			}
		}
	}
	for _, raw := range []string{`{}`, `{"details":null}`, `{"details":{}}`, `{"details":{"description":null}}`, `{"details":{"description":""}}`, `{"details":{"description":"新介绍"}}`} {
		var c, saved customCard
		if err := json.Unmarshal([]byte(raw), &c); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(encoded, &saved); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(c.Details, saved.Details) {
			t.Fatal("JSON changed inheritance/clearing")
		}
	}
	empty := ""
	if err := applyCustomCardDetails(customCard{Details: &customCardDetails{Hand: &empty}}, make([]string, 62)); err == nil {
		t.Fatal("short template accepted an unsupported field")
	}
}
