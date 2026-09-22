package game

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

var teamBattleCategoryKeys = [...]string{"9", "10", "11", "12"}

// TeamBattleCatalog is owned by one request. Decode the wire document once, retain
// fields outside the publication contract, then apply expiry, progression and
// publication rules to this view. It never aliases the account's saved JSON.
type TeamBattleCatalog struct {
	fields map[string]any
	groups map[string][]teamBattleCatalogGroup
}

type teamBattleCatalogGroup struct {
	fields map[string]any
	id     int
	areaID int
	bosses []teamBattleCatalogBoss
}

type teamBattleCatalogBoss struct {
	fields map[string]any
	id     int
	model  int
}

func decodeTeamBattleCatalog(raw json.RawMessage) (*TeamBattleCatalog, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	// Preserve integer precision and unmodeled fields when re-encoding the DTO.
	decoder.UseNumber()
	var fields map[string]any
	if err := decoder.Decode(&fields); err != nil {
		return nil, fmt.Errorf("decode team battle catalog: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("team battle catalog has trailing JSON")
	}
	if fields == nil {
		return nil, errors.New("team battle catalog is not an object")
	}
	catalog := &TeamBattleCatalog{fields: fields, groups: make(map[string][]teamBattleCatalogGroup, 4)}
	for _, key := range teamBattleCategoryKeys {
		values, ok := fields[key].([]any)
		if !ok && fields[key] != nil {
			return nil, fmt.Errorf("team battle category %s is not a list", key)
		}
		groups := make([]teamBattleCatalogGroup, 0, len(values))
		for _, value := range values {
			group, err := decodeTeamBattleCatalogGroup(value)
			if err != nil {
				return nil, err
			}
			groups = append(groups, group)
		}
		catalog.groups[key] = groups
	}
	return catalog, nil
}

func decodeTeamBattleCatalogGroup(value any) (teamBattleCatalogGroup, error) {
	fields, ok := value.(map[string]any)
	if !ok {
		return teamBattleCatalogGroup{}, errors.New("team battle group is not an object")
	}
	id, idErr := teamBattleCatalogInteger(fields["0"])
	areaID, areaErr := teamBattleCatalogInteger(fields["9"])
	_, stageErr := teamBattleCatalogInteger(fields["1"])
	if idErr != nil || areaErr != nil || stageErr != nil {
		return teamBattleCatalogGroup{}, errors.New("decode team battle group identity")
	}
	values, ok := fields["10"].([]any)
	if !ok && fields["10"] != nil {
		return teamBattleCatalogGroup{}, errors.New("team battle bosses are not a list")
	}
	group := teamBattleCatalogGroup{fields: fields, id: int(id), areaID: int(areaID)}
	for _, value := range values {
		boss, ok := value.(map[string]any)
		if !ok {
			return teamBattleCatalogGroup{}, errors.New("team battle boss is not an object")
		}
		id, idErr := teamBattleCatalogInteger(boss["0"])
		model, modelErr := teamBattleCatalogInteger(boss["14"])
		if idErr != nil || modelErr != nil {
			return teamBattleCatalogGroup{}, errors.New("decode team battle boss identity")
		}
		for _, key := range []string{"10", "26"} {
			if _, err := teamBattleCatalogInteger(boss[key]); err != nil {
				return teamBattleCatalogGroup{}, fmt.Errorf("decode team battle boss %d field %s: %w", id, key, err)
			}
		}
		if difficulty, exists := boss["4"]; exists && difficulty != nil {
			if _, ok := difficulty.(string); !ok {
				return teamBattleCatalogGroup{}, errors.New("team battle difficulty is not a string")
			}
		}
		group.bosses = append(group.bosses, teamBattleCatalogBoss{fields: boss, id: int(id), model: int(model)})
	}
	return group, nil
}

func teamBattleCatalogInteger(value any) (int64, error) {
	if value == nil {
		return 0, nil
	}
	number, ok := value.(json.Number)
	if !ok {
		return 0, errors.New("team battle numeric field is not an integer")
	}
	return number.Int64()
}

func (group teamBattleCatalogGroup) wire() map[string]any {
	bosses := make([]any, 0, len(group.bosses))
	for _, boss := range group.bosses {
		bosses = append(bosses, boss.fields)
	}
	group.fields["10"] = bosses
	return group.fields
}

// Groups returns the stock numeric-field DTOs for one category. Adapters should
// treat them as read-only. They never alias another request or saved progress.
func (catalog *TeamBattleCatalog) Groups(key string) []any {
	groups := make([]any, 0, len(catalog.groups[key]))
	for _, group := range catalog.groups[key] {
		groups = append(groups, group.wire())
	}
	return groups
}

func (catalog *TeamBattleCatalog) HasBoss(bossID int) bool {
	if bossID <= 0 {
		return false
	}
	for _, key := range teamBattleCategoryKeys {
		for _, group := range catalog.groups[key] {
			for _, boss := range group.bosses {
				if boss.id == bossID {
					return true
				}
			}
		}
	}
	return false
}

func (catalog *TeamBattleCatalog) MarshalJSON() ([]byte, error) {
	for _, key := range teamBattleCategoryKeys {
		catalog.fields[key] = catalog.Groups(key)
	}
	return json.Marshal(catalog.fields)
}

func (catalog *TeamBattleCatalog) expireUserBuffs(nowUnix int64) (bool, error) {
	// Validate before changing anything, so malformed expiry data cannot cause
	// a partial update of the saved account state.
	var expired []map[string]any
	for _, key := range teamBattleCategoryKeys {
		for _, group := range catalog.groups[key] {
			for _, boss := range group.bosses {
				value, exists := boss.fields["16"]
				if !exists {
					return false, errors.New("team battle boss has no buff expiry")
				}
				expiration, err := teamBattleCatalogInteger(value)
				if err != nil {
					return false, err
				}
				if expiration > 0 && expiration <= nowUnix {
					expired = append(expired, boss.fields)
				}
			}
		}
	}
	for _, boss := range expired {
		boss["10"], boss["16"] = json.Number("0"), json.Number("0")
	}
	return len(expired) > 0, nil
}

func (catalog *TeamBattleCatalog) filterDisabled(disabled map[int]bool) {
	if len(disabled) == 0 {
		return
	}
	for _, key := range teamBattleCategoryKeys {
		for index := range catalog.groups[key] {
			group := &catalog.groups[key][index]
			kept := make([]teamBattleCatalogBoss, 0, len(group.bosses))
			for _, boss := range group.bosses {
				if !disabled[boss.id] {
					kept = append(kept, boss)
				}
			}
			group.bosses = kept
		}
	}
}
