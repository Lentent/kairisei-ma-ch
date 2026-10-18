package multiplayer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Server-only metadata keeps new behavior out of the client's fixed role enums.
const CustomCardEffectsFile = "custom_card_effects.json"

type CustomCardCombatEffects struct {
	TurnSingleToAll bool `json:"turn_single_to_all,omitempty"`
}

type CustomCardEffects struct {
	SchemaVersion int                             `json:"schema_version"`
	Cards         map[int]CustomCardCombatEffects `json:"cards"`
}

func ParseCustomCardEffects(raw []byte) (CustomCardEffects, error) {
	var effects CustomCardEffects
	if len(raw) > maxCombatMasterBytes {
		return effects, errors.New("custom card effects exceed the size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&effects); err != nil {
		return effects, fmt.Errorf("decode custom card effects: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return effects, errors.New("custom card effects must contain one JSON value")
	}
	if effects.SchemaVersion != 1 || effects.Cards == nil {
		return effects, errors.New("invalid custom card effects schema")
	}
	for id := range effects.Cards {
		if id < 98000001 || id > 98999999 {
			return effects, fmt.Errorf("custom card effect ID %d is outside the custom range", id)
		}
	}
	return effects, nil
}

func (catalog *CombatCatalog) loadCustomCardEffects(root string) error {
	path := filepath.Join(root, CustomCardEffectsFile)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil // Existing resource sets do not need an extension file.
	}
	resolved, err := resolveCombatMasterFile(path)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(resolved)
	if err != nil {
		return err
	}
	effects, err := ParseCustomCardEffects(raw)
	if err != nil {
		return err
	}
	for id, effect := range effects.Cards {
		card, ok := catalog.Cards[id]
		if !ok {
			return fmt.Errorf("custom card effects refer to unavailable card %d", id)
		}
		card.TurnSingleToAll = effect.TurnSingleToAll
		catalog.Cards[id] = card
	}
	return nil
}
