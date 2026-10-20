package admin

import (
	"reflect"
	"testing"
)

func TestCustomCardAttributesValidateAndMaterialize(t *testing.T) {
	s := customUnitSources()
	s.Rules["ATTACK_AA"][7] = "ATTR"
	for _, role := range s.Roles {
		role[27] = "FIRE"
	}
	a := &API{catalogByKey: map[string]AdminCatalogEntry{"6:101": {ResourceState: "ready"}}}
	attributes := []string{"FIRE", "ICE", "WIND", "LIGHT", "DARK"}
	for i, first := range attributes {
		for j := i; j < len(attributes); j++ {
			value, mask := first, uint8(1<<i)
			if j != i {
				value += "_" + attributes[j]
				mask |= 1 << j
			}
			t.Run(value, func(t *testing.T) {
				c, err := s.template(101)
				if err != nil {
					t.Fatal(err)
				}
				c.ID, c.Attribute = customCardFirstID, value
				if j != i {
					c.Attribute = attributes[j] + "_" + first
				}
				d := customCardDraft{[]customCard{c}}
				if err := a.validateCustomCards(&d, s); err != nil {
					t.Fatal(err)
				}
				if d.Cards[0].Attribute != value {
					t.Fatalf("saved attribute = %s, want %s", d.Cards[0].Attribute, value)
				}
				originalSkills, originalRoles := cloneCustomRows(c.Skills), cloneCustomRows(c.Roles)
				_, skills, roles, card, err := materializeCustomCard(c, s)
				if err != nil {
					t.Fatal(err)
				}
				if card.FusionAttributes != mask {
					t.Fatalf("fusion mask = %d, want %d", card.FusionAttributes, mask)
				}
				for k := range skills {
					if skills[k][11] != value || roles[k][27] != value {
						t.Fatalf("branch %d attribute differs: skill=%s role=%s", k, skills[k][11], roles[k][27])
					}
				}
				if !reflect.DeepEqual(c.Skills, originalSkills) || !reflect.DeepEqual(c.Roles, originalRoles) {
					t.Fatal("export mutated draft skills or effects")
				}
			})
		}
	}
	for _, value := range []string{"", "NULL", "ALL", "fire", " FIRE", "FIRE_", "_ICE", "FIRE/ICE", "FIRE_FIRE", "FIRE_ICE_WIND", "FIRE_UNKNOWN"} {
		c, err := s.template(101)
		if err != nil {
			t.Fatal(err)
		}
		c.ID, c.Attribute = customCardFirstID, value
		if err := a.validateCustomCards(&customCardDraft{[]customCard{c}}, s); err == nil {
			t.Fatalf("saved invalid attribute %q", value)
		}
		if _, _, _, _, err := materializeCustomCard(c, s); err == nil {
			t.Fatalf("exported invalid attribute %q", value)
		}
	}
}

func TestCustomDualAttributesPreserveExplicitEffectAttribute(t *testing.T) {
	s := customUnitSources()
	s.Rules["ATTACK_AA"][7] = "ATTR"
	s.Roles[0][27], s.Roles[1][27] = "FIRE", "LIGHT"
	c, err := s.template(101)
	if err != nil {
		t.Fatal(err)
	}
	c.ID, c.Attribute = customCardFirstID, "ICE_WIND"
	_, _, roles, _, err := materializeCustomCard(c, s)
	if err != nil {
		t.Fatal(err)
	}
	if roles[0][27] != "ICE_WIND" || roles[1][27] != "LIGHT" {
		t.Fatalf("lost linked or explicitly different effect attribute: %s %s", roles[0][27], roles[1][27])
	}
}

func TestCustomDualAttributesPreserveSingleAttributeEffects(t *testing.T) {
	for _, code := range []string{"ENCHANT", "ATTR_DEF_UP", "ATTR_DEF_DOWN"} {
		t.Run(code, func(t *testing.T) {
			s := customUnitSources()
			// These consumers take one attribute, even when the card is dual.
			s.Rules[code] = append([]string(nil), s.Rules["ATTACK_AA"]...)
			s.Rules[code][5] = "ATTR"
			for _, role := range s.Roles {
				role[8], role[25] = code, "FIRE"
			}
			c, err := s.template(101)
			if err != nil {
				t.Fatal(err)
			}
			c.ID, c.Attribute = customCardFirstID, "ICE_WIND"
			_, _, roles, _, err := materializeCustomCard(c, s)
			if err != nil {
				t.Fatal(err)
			}
			if roles[0][25] != "FIRE" || roles[1][25] != "FIRE" {
				t.Fatal("dual card exported an unsupported dual selector", roles)
			}
			// Existing single-attribute cards retain their conversion behavior.
			c.Attribute = "ICE"
			_, _, roles, _, err = materializeCustomCard(c, s)
			if err != nil || roles[0][25] != "ICE" {
				t.Fatalf("single attribute conversion regressed: %v %v", roles, err)
			}
		})
	}
}

func TestCustomDualAttributeTemplateCanRemainDualOrBecomeSingle(t *testing.T) {
	s := customUnitSources()
	s.Rules["ATTACK_AA"][7] = "ATTR"
	for _, skill := range s.Skills {
		skill[11] = "FIRE_ICE"
	}
	for _, role := range s.Roles {
		role[27] = "FIRE_ICE"
	}
	c, err := s.template(101)
	if err != nil || c.Attribute != "FIRE_ICE" {
		t.Fatalf("dual template attribute lost: %s %v", c.Attribute, err)
	}
	c.ID = customCardFirstID
	for _, attribute := range []string{"FIRE_ICE", "LIGHT_DARK", "WIND"} {
		c.Attribute = attribute
		_, skills, roles, _, err := materializeCustomCard(c, s)
		if err != nil || skills[0][11] != attribute || roles[0][27] != attribute {
			t.Fatalf("dual template conversion to %s: %v %v %v", attribute, skills, roles, err)
		}
	}
}
