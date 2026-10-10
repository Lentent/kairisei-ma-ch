package admin

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestCustomCardDialogueInheritanceValidationAndExport(t *testing.T) {
	s := customUnitSources()
	row := append(s.Cards[101], make([]string, 100-len(s.Cards[101]))...)
	row[79], row[80], row[89], row[90] = "原攻击台词", "原支援台词", "123456", "789012"
	s.Cards[101] = row
	original := append([]string(nil), row...)
	a := &API{catalogByKey: map[string]AdminCatalogEntry{"6:101": {ResourceState: "ready"}}}
	c, err := s.template(101)
	if err != nil {
		t.Fatal(err)
	}
	c.ID = customCardFirstID
	defaults := customCardTemplateDialogues(s, []customCard{c})[101]
	if defaults.Attack != row[79] || defaults.Support != row[80] {
		t.Fatal("template defaults do not match the client dialogue columns")
	}
	for _, tc := range []struct {
		name  string
		text  string
		valid bool
	}{
		{"edited", "库巴姬会守护大家！", true},
		{"cleared", "", true},
		{"limit", strings.Repeat("字", 100), true},
		{"too long", strings.Repeat("字", 101), false},
		{"whitespace", "   ", false},
		{"newline", "台\n词", false},
		{"carriage return", "台\r词", false},
		{"nul", "台\x00词", false},
		{"invalid utf8", string([]byte{0xff}), false},
		{"separator", "台,词", false},
		{"quote", "台\"词", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, support := range []bool{false, true} {
				draft := c
				if support {
					draft.SupportDialogue = &tc.text
				} else {
					draft.AttackDialogue = &tc.text
				}
				err := a.validateCustomCards(&customCardDraft{Cards: []customCard{draft}}, s)
				if !tc.valid {
					if err == nil {
						t.Fatal("accepted invalid dialogue")
					}
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				generated, _, _, _, err := materializeCustomCard(draft, s)
				if err != nil {
					t.Fatal(err)
				}
				text, err := rewriteCustomCSV("", map[string][][]string{generated[0]: {generated}})
				if err != nil {
					t.Fatal(err)
				}
				rows, err := customCSVRows([]byte(text))
				if err != nil || len(rows) != 1 {
					t.Fatalf("invalid exported card: %v", err)
				}
				wantAttack, wantSupport := row[79], row[80]
				if support {
					wantSupport = tc.text
				} else {
					wantAttack = tc.text
				}
				if rows[0][79] != wantAttack || rows[0][80] != wantSupport || rows[0][89] != row[89] || rows[0][90] != row[90] {
					t.Fatal("dialogue edit affected the other line or voice IDs")
				}
			}
		})
	}
	generated, _, _, _, err := materializeCustomCard(c, s)
	if err != nil || !reflect.DeepEqual(generated[67:92], row[67:92]) {
		t.Fatal("old draft lost inherited comments or voice IDs")
	}
	if !reflect.DeepEqual(s.Cards[101], original) {
		t.Fatal("export mutated the source template")
	}
}

func TestCustomCardDialogueJSONKeepsInheritanceAndClearingDistinct(t *testing.T) {
	for _, raw := range []string{`{}`, `{"support_dialogue":null}`, `{"support_dialogue":""}`, `{"support_dialogue":"新台词"}`} {
		var c customCard
		if err := json.Unmarshal([]byte(raw), &c); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		var decoded customCard
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(c.SupportDialogue, decoded.SupportDialogue) {
			t.Fatal("JSON round trip changed inheritance/clear semantics")
		}
		if c.SupportDialogue == nil && strings.Contains(string(encoded), "support_dialogue") {
			t.Fatal("inherited dialogue was materialized into an override")
		}
	}
	if err := validateCustomCardDialogue(customCard{SupportDialogue: new(string)}, make([]string, 62)); err == nil {
		t.Fatal("short template must fail safely when overriding dialogue")
	}
}
