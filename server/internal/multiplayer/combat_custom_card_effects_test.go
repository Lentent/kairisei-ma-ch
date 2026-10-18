package multiplayer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCustomCardEffectsLoading(t *testing.T) {
	root := t.TempDir()
	catalog := &CombatCatalog{Cards: map[int]CombatCardDefinition{98000001: {ID: 98000001}, 1: {ID: 1}}}
	if err := catalog.loadCustomCardEffects(root); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"schema_version":1,"cards":{"98000001":{"turn_single_to_all":true}}}`)
	if err := os.WriteFile(filepath.Join(root, CustomCardEffectsFile), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := catalog.loadCustomCardEffects(root); err != nil || !catalog.Cards[98000001].TurnSingleToAll || catalog.Cards[1].TurnSingleToAll {
		t.Fatalf("custom effect was lost or applied to an ordinary card: %v", err)
	}
	delete(catalog.Cards, 98000001)
	if err := catalog.loadCustomCardEffects(root); err == nil {
		t.Fatal("accepted effects for a missing card")
	}
	for _, invalid := range []string{
		`{"schema_version":2,"cards":{}}`,
		`{"schema_version":1,"cards":null}`,
		`{"schema_version":1,"cards":{"1":{"turn_single_to_all":true}}}`,
		`{"schema_version":1,"cards":{"98000001":{"unknown_effect":true}}}`,
		`{"schema_version":1,"cards":{}} {}`,
	} {
		if _, err := ParseCustomCardEffects([]byte(invalid)); err == nil {
			t.Fatalf("accepted invalid metadata: %s", invalid)
		}
	}
}
