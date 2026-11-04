package admin

import (
	"strconv"
	"testing"
)

func TestCustomPowerChainMagnitudes(t *testing.T) {
	skill := make([]string, 50)
	skill[0], skill[49] = "1900000000", "1900000000"
	makeRole := func(code string, params ...string) []string {
		r := make([]string, 33)
		r[0], r[8] = skill[0], code
		copy(r[20:], params)
		return r
	}
	attack := makeRole("ATTACK_AA", "13697", "0", "1000", "0", "1", "INT", "150", "FIRE_LIGHT", "MAGIC")
	buff := makeRole("ATK_UP_FIXED", "2", "INT", "1000", "4004", "0", "0")
	heal := makeRole("HEAL_FIXED", "4004", "0", "1000", "0", "MND")
	boost := append(customPowerBoostRole(1900000000), "4")
	for _, order := range [][][]string{{attack, buff, heal, boost}, {boost, attack, buff, heal}} {
		skills, roles, err := compileCustomCardChains(customCardFirstID, [][]string{skill}, cloneCustomRows(order))
		if err != nil {
			t.Fatal(err)
		}
		if len(skills) != 2 {
			t.Fatalf("branches %d", len(skills))
		}
		for _, branch := range skills {
			high := branch[37] == "4"
			for _, r := range roles {
				if r[0] != branch[49] {
					continue
				}
				if r[8] == customPowerBoost {
					t.Fatal("custom enum leaked")
				}
				switch r[8] {
				case "ATTACK_AA":
					want := 13697
					mult := 1000
					if high {
						want = 24654
						mult = 1800
					}
					if customRowInt(r, 20) != want || customRowInt(r, 22) != mult || r[24] != "1" || r[26] != "150" || r[30] != "" {
						t.Fatalf("attack %v", r)
					}
				case "ATK_UP_FIXED":
					want := 4004
					if high {
						want = 7207
					}
					if customRowInt(r, 23) != want || r[20] != "2" || r[22] != "1000" {
						t.Fatalf("buff %v", r)
					}
				case "HEAL_FIXED":
					want := 4004
					if high {
						want = 7207
					}
					if customRowInt(r, 20) != want {
						t.Fatalf("heal %v", r)
					}
				}
			}
		}
	}
	if attack[20] != "13697" {
		t.Fatal("draft mutated")
	}
}
func TestCustomPowerValidationAndMaterialize(t *testing.T) {
	s := customUnitSources()
	c, err := s.template(101)
	if err != nil {
		t.Fatal(err)
	}
	c.ID = customCardFirstID
	fn := customRowInt(c.Roles[0], 0)
	c.Roles = append(c.Roles, customPowerBoostRole(fn))
	c.RoleSources = append(c.RoleSources, customRoleSource{MinChain: 4})
	a := &API{catalogByKey: map[string]AdminCatalogEntry{"6:101": {ResourceState: "ready"}}}
	if err := a.validateCustomCards(&customCardDraft{Cards: []customCard{c}}, s); err != nil {
		t.Fatal(err)
	}
	_, _, roles, _, err := materializeCustomCard(c, s)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range roles {
		if r[8] == customPowerBoost {
			t.Fatal("custom enum exported")
		}
	}
	for _, p := range []string{"-1", "10001", "1.5", "bad"} {
		r := customPowerBoostRole(fn)
		r[20] = p
		if validateCustomSpecialRole(r, customRoleSource{}) == nil {
			t.Fatalf("accepted %s", p)
		}
	}
	r := customPowerBoostRole(fn)
	r[20] = strconv.Itoa(0)
	if validateCustomSpecialRole(r, customRoleSource{}) != nil {
		t.Fatal("zero boost rejected")
	}
	r = make([]string, 33)
	r[8], r[20] = "ATTACK_AA", "100000000"
	if applyCustomPower(r, 10000) == nil {
		t.Fatal("overflow accepted")
	}
}
