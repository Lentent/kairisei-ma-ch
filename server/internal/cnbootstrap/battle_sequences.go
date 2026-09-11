package cnbootstrap

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"

	"kairisei.local/server/internal/release"
)

// These explicit bindings restore omitted optional phases. The official enemy
// master and client AWAKE transition remain authoritative; never infer a second
// party by adding an offset to arbitrary Boss IDs at runtime.
//
//go:embed battle_sequences.json
var cnBattleSequencesJSON []byte

func projectCNBattleRuntime(master cnBattleRuntimeMaster) (cnBattleRuntimeMaster, error) {
	var policy struct {
		SchemaVersion int `json:"schema_version"`
		Sequences     []struct {
			BossID  int                              `json:"boss_id"`
			Battles []release.TeamBattleReplayBattle `json:"battles"`
		} `json:"sequences"`
	}
	if err := json.Unmarshal(cnBattleSequencesJSON, &policy); err != nil {
		return cnBattleRuntimeMaster{}, err
	}
	if policy.SchemaVersion != 1 {
		return cnBattleRuntimeMaster{}, fmt.Errorf("unsupported CN battle sequence policy")
	}
	master.Replays = slices.Clone(master.Replays)
	for _, sequence := range policy.Sequences {
		for i := range master.Replays {
			replay := &master.Replays[i]
			if replay.BossID != sequence.BossID {
				continue
			}
			if len(sequence.Battles) < 2 || sequence.Battles[0].EnemyPartyID != replay.EnemyPartyID || sequence.Battles[0].EnemyType != replay.EnemyType {
				return cnBattleRuntimeMaster{}, fmt.Errorf("CN battle %d sequence does not match its first phase", replay.BossID)
			}
			// Accept either the original omitted-phase definition or an already
			// materialized complete definition, not an unrelated edited sequence.
			if len(replay.Battles) > 1 && !slices.Equal(replay.Battles, sequence.Battles) {
				return cnBattleRuntimeMaster{}, fmt.Errorf("CN battle %d has a conflicting phase sequence", replay.BossID)
			}
			replay.Battles = slices.Clone(sequence.Battles)
			if err := validateCNTeamBattleReplaySegments(*replay); err != nil {
				return cnBattleRuntimeMaster{}, err
			}
		}
	}
	// Native solo reads TeamBattleBossInfo.awake.is_cost before its first
	// Start. The server engine's cost offset alone cannot affect that path.
	awakeBosses := make(map[int]bool)
	for _, replay := range master.Replays {
		if len(replay.Battles) < 2 {
			continue
		}
		for _, wave := range replay.Battles[1:] {
			if wave.EnemyType == 4 {
				awakeBosses[replay.BossID] = true
			}
		}
	}
	var err error
	master.Groups, err = projectCNSoloAwakeCost(master.Groups, "10", awakeBosses)
	if err != nil {
		return cnBattleRuntimeMaster{}, err
	}
	master.PastBossGroups, err = projectCNSoloAwakeCost(master.PastBossGroups, "13", awakeBosses)
	if err != nil {
		return cnBattleRuntimeMaster{}, err
	}
	// Own-deck entries must inherit the same complete phases as their source.
	return projectCNBattleOwnDeckVariants(master)
}

func projectCNSoloAwakeCost(groups []json.RawMessage, bossKey string, awakeBosses map[int]bool) ([]json.RawMessage, error) {
	result := slices.Clone(groups)
	for i, raw := range groups {
		var group map[string]json.RawMessage
		if err := json.Unmarshal(raw, &group); err != nil {
			return nil, err
		}
		var bosses []map[string]json.RawMessage
		if err := json.Unmarshal(group[bossKey], &bosses); err != nil {
			return nil, err
		}
		changed := false
		for _, boss := range bosses {
			var id int
			if err := json.Unmarshal(boss["0"], &id); err != nil {
				return nil, err
			}
			if !awakeBosses[id] {
				continue
			}
			var awake map[string]json.RawMessage
			if err := json.Unmarshal(boss["18"], &awake); err != nil {
				return nil, err
			}
			if awake == nil {
				return nil, fmt.Errorf("boss %d has no awake configuration", id)
			}
			awake["1"] = json.RawMessage("1")
			boss["18"], _ = json.Marshal(awake)
			changed = true
		}
		if changed {
			group[bossKey], _ = json.Marshal(bosses)
			result[i], _ = json.Marshal(group)
		}
	}
	return result, nil
}
