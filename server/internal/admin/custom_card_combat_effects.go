package admin

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"

	"kairisei.local/server/internal/multiplayer"
)

const customCardEffectsPath = "_local/control/server/cn602-battle-master/" + multiplayer.CustomCardEffectsFile

func customSingleToAllRole(functionID int) []string {
	row := make([]string, 33)
	row[0], row[8], row[9] = strconv.Itoa(functionID), multiplayer.CombatFunctionTurnSingleToAll, "SELF"
	row[20] = "1" // Scoped effect version; empty preserves old team-wide markers.
	for i := 11; i < 20; i++ {
		row[i] = "1"
	}
	return row
}

func isCustomSpecialRole(row []string) bool {
	return len(row) > 8 && row[8] == multiplayer.CombatFunctionTurnSingleToAll
}

func validateCustomSpecialRole(row []string, source customRoleSource) error {
	expected := customSingleToAllRole(customRowInt(row, 0))
	if len(row) == len(expected) {
		if row[20] == "" && row[9] == "SELF" {
			expected[20] = "" // Previously exported marker.
		} else if row[20] == "1" && (row[9] == "SELF" || row[9] == "USER_ONE" || row[9] == "USER_ALL") {
			expected[9] = row[9]
		}
	}
	if source.CardID != 0 || source.Index != 0 || !reflect.DeepEqual(row, expected) {
		return errors.New("全体化效果仅可选择自身、己方一人或全队，类型及参数不可修改，请从添加特殊效果重新添加")
	}
	return nil
}

func validateCustomSpecialRoleTarget(row []string, skills [][]string) error {
	if row[20] != "1" || row[9] != "USER_ONE" {
		return nil
	}
	for _, skill := range skills {
		if (customRowInt(skill, 49) == customRowInt(row, 0) || customRowInt(skill, 49) == 0 && skill[0] == row[0]) && skill[19] != "USER_ONE" {
			return errors.New("全体化选择己方一人时，关联技能的目标须设为己方单体，出牌时选择生效角色")
		}
	}
	return nil
}

// The client only accepts native effect enums. A zero piercing operator is
// inert, preserves row indices and avoids NONE's empty-Arthur-skill sentinel.
func customClientRoleRows(rows [][]string) [][]string {
	result := cloneCustomRows(rows)
	for _, row := range result {
		if isCustomSpecialRole(row) {
			row[8] = "ATK_OP_PIERCING"
			row[20], row[21], row[22] = "0", "0", "0"
		}
	}
	return result
}

func buildCustomCardCombatEffects(d customCardDraft, root string, files, original map[string][]byte) error {
	effects := multiplayer.CustomCardEffects{SchemaVersion: 1, Cards: map[int]multiplayer.CustomCardCombatEffects{}}
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(customCardEffectsPath)))
	if err == nil {
		effects, err = multiplayer.ParseCustomCardEffects(raw)
		if err != nil {
			return err
		}
		original[customCardEffectsPath] = raw
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, card := range d.Cards {
		if card.TurnSingleToAll {
			effects.Cards[card.ID] = multiplayer.CustomCardCombatEffects{TurnSingleToAll: true}
		} else {
			delete(effects.Cards, card.ID)
		}
	}
	if raw == nil && len(effects.Cards) == 0 {
		return nil
	}
	files[customCardEffectsPath], err = resourceJSON(effects)
	return err
}
