package admin

import (
	"os"
	"strings"
	"testing"
)

func TestCustomEffectHelpUnitsAndParameterPositions(t *testing.T) {
	rules := map[string][]string{
		"ATTACK_AA":    {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "ATTR", "SKILL_PHYSICS_TYPE", ""},
		"ATK_UP_FIXED": {"VALUE", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE", "VALUE", "VALUE"},
		"REFLECTION":   {"VALUE", "VALUE", "VALUE", "SKILL_PHYSICS_TYPE"},
		"ATK_OP_DRAIN": {"VALUE", "VALUE", "VALUE"},
		"UNKNOWN_ROLE": {"VALUE", "ATTR", ""},
	}
	help := customCardEffectHelp(rules)
	for _, tc := range []struct {
		code       string
		index      int
		name, unit string
	}{
		{"ATTACK_AA", 4, "攻击次数", "count"}, {"ATTACK_AA", 6, "基础暴击率", "permille"},
		{"ATK_UP_FIXED", 2, "固定值倍率", "permille"}, {"REFLECTION", 1, "基础反弹倍率", "permyriad"},
		{"ATK_OP_DRAIN", 0, "伤害转回复比例", "percent"},
	} {
		p := help[tc.code].Parameters[tc.index]
		if p.Name != tc.name || p.Unit != tc.unit {
			t.Fatalf("%s parameter %d has misleading help: %+v", tc.code, tc.index+1, p)
		}
	}
	for code, types := range rules {
		if len(help[code].Parameters) != len(types) {
			t.Fatalf("%s parameter order changed", code)
		}
	}
	if help["UNKNOWN_ROLE"].Parameters[0].Unit != "" || !strings.Contains(help["UNKNOWN_ROLE"].Parameters[0].Description, "尚未确认") {
		t.Fatal("unknown parameter presented a guessed unit")
	}
	if help["ATTACK_AA"].Parameters[9].Name != "未使用" {
		t.Fatal("undefined parameter received an active label")
	}
	if _, exists := help["HEAL_FIXED"]; exists {
		t.Fatal("help includes a function outside supplied resource rules")
	}
}

func TestCustomEffectHelpCoversActualPlayerEffects(t *testing.T) {
	root := os.Getenv("CN602_CUSTOM_CARD_RESOURCE_SET")
	if root == "" {
		t.Skip("requires complete game resources")
	}
	s, err := (&API{collectionResourceRoot: root}).customCardSources()
	if err != nil {
		t.Fatal(err)
	}
	help := customCardEffectHelp(s.Rules)
	seen := map[string]bool{}
	for _, row := range s.Roles {
		code := row[8]
		seen[code] = true
		h, ok := help[code]
		if !ok || h.Name == code || strings.Contains(h.Description, "尚未") {
			t.Fatalf("player effect %s lacks readable explanation", code)
		}
		for i, kind := range s.Rules[code] {
			if kind != "" && (h.Parameters[i].Name == "" || h.Parameters[i].Description == "") {
				t.Fatalf("%s parameter %d lacks help", code, i+1)
			}
		}
	}
	t.Logf("all %d active player effect types have Chinese explanations", len(seen))
}
