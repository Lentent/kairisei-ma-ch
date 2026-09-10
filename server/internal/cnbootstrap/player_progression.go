package cnbootstrap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"kairisei.local/server/internal/release"
)

const maxCNPlayerProgressionRuntimeBytes = 64 * 1024

type cnPlayerProgressionRuntimeMaster struct {
	SchemaVersion int             `json:"schema_version"`
	ClientProfile string          `json:"client_profile"`
	State         string          `json:"state"`
	Evidence      json.RawMessage `json:"evidence"`
	release.PlayerProgressionPolicy
}

func loadCNPlayerProgressionRuntimeMaster(path string) (release.PlayerProgressionPolicy, error) {
	if path == "" {
		return release.PlayerProgressionPolicy{}, errors.New("CN player progression runtime path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return release.PlayerProgressionPolicy{}, fmt.Errorf("resolve CN player progression runtime: %w", err)
	}
	file, err := os.Open(absolute)
	if err != nil {
		return release.PlayerProgressionPolicy{}, fmt.Errorf("open CN player progression runtime: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return release.PlayerProgressionPolicy{}, fmt.Errorf("stat CN player progression runtime: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxCNPlayerProgressionRuntimeBytes {
		return release.PlayerProgressionPolicy{}, errors.New("CN player progression runtime must be a non-empty regular JSON file within the size limit")
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxCNPlayerProgressionRuntimeBytes+1))
	decoder.DisallowUnknownFields()
	var master cnPlayerProgressionRuntimeMaster
	if err := decoder.Decode(&master); err != nil {
		return release.PlayerProgressionPolicy{}, fmt.Errorf("decode CN player progression runtime: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return release.PlayerProgressionPolicy{}, err
	}
	if err := validateCNPlayerProgressionRuntimeMaster(master); err != nil {
		return release.PlayerProgressionPolicy{}, err
	}
	return master.PlayerProgressionPolicy, nil
}

func validateCNPlayerProgressionRuntimeMaster(master cnPlayerProgressionRuntimeMaster) error {
	policy := master.PlayerProgressionPolicy
	validSourceState := func(value string) bool {
		return value == "CONFIRMED" || value == "INFERRED" || value == "PLACEHOLDER"
	}
	if master.SchemaVersion != 2 || master.ClientProfile != "cn602-bootstrap" || master.State != "PASS" ||
		len(master.Evidence) == 0 || bytes.Equal(bytes.TrimSpace(master.Evidence), []byte("null")) ||
		policy.ConfigVersion <= 0 || policy.MaxLevel < 2 || policy.MaxLevel > 1000 ||
		!validSourceState(policy.Experience.SourceState) || policy.Experience.Base <= 0 || policy.Experience.PerLevel < 0 ||
		!validSourceState(policy.BattlePoints.SourceState) || policy.BattlePoints.Base <= 0 ||
		policy.BattlePoints.LevelsPerPoint <= 0 || policy.BattlePoints.Maximum < policy.BattlePoints.Base ||
		!validSourceState(policy.Friends.SourceState) || policy.Friends.Minimum <= 0 ||
		policy.Friends.Denominator <= 0 || policy.Friends.Numerator <= 0 || policy.Friends.Maximum < policy.Friends.Minimum ||
		policy.Friends.HelperReward.SourceState != "PLACEHOLDER" ||
		policy.Friends.HelperReward.OtherPerPartner <= 0 ||
		policy.Friends.HelperReward.FriendPerPartner <= policy.Friends.HelperReward.OtherPerPartner ||
		policy.Friends.HelperReward.MaximumPartners != 3 ||
		!validSourceState(policy.JobParameters.SourceState) || policy.JobParameters.MaxStatusLevel <= 0 ||
		len(policy.JobParameters.Maximum) != 5 || !validSourceState(policy.LevelUpRecovery.SourceState) ||
		!policy.LevelUpRecovery.AP || !policy.LevelUpRecovery.BP || !policy.Migration.PreserveLevel ||
		!policy.Migration.PreserveWithinLevelExperience || !policy.Migration.PreservePointFillState {
		return errors.New("CN player progression runtime identity or policy is invalid")
	}
	if policy.ExperienceRequired(policy.MaxLevel-1) <= 0 || policy.CumulativeExperience(policy.MaxLevel) <= 0 ||
		policy.BattlePointMaximum(policy.MaxLevel) != policy.BattlePoints.Maximum ||
		policy.FriendMaximum(policy.MaxLevel) != policy.Friends.Maximum {
		return errors.New("CN player progression runtime curve is invalid")
	}
	for index, value := range policy.JobParameters.Maximum {
		if value.HP < 0 || value.Attack < 0 || value.Magic < 0 || value.Mind < 0 ||
			(index > 0 && value.HP == 0) {
			return fmt.Errorf("CN player progression job slot %d is invalid", index)
		}
	}
	zero := release.JobParameter{}
	if policy.JobParameters.Maximum[0] != zero {
		return errors.New("CN player progression job slot zero must remain empty")
	}
	return nil
}

func applyCNPlayerProgressionRuntimeMaster(
	state *release.State,
	policy release.PlayerProgressionPolicy,
) (bool, error) {
	if state.PlayerProgressionConfigVersion > policy.ConfigVersion {
		return false, errors.New("CN player progression save is newer than the runtime policy")
	}
	state.PlayerProgressionPolicy = policy
	if state.PlayerProgressionConfigVersion == policy.ConfigVersion {
		if err := validateCNPlayerProgressionState(state.User, policy); err != nil {
			return false, err
		}
		return false, nil
	}
	if state.User.Level < 1 || state.User.Level > policy.MaxLevel {
		return false, errors.New("CN player progression migration has an invalid level")
	}
	oldBPMax := state.User.BPMax
	wasBPFull := oldBPMax > 0 && state.User.BP >= oldBPMax
	preservedWithinLevelExperience := state.User.NowLevelExperience
	if state.User.Level == policy.MaxLevel {
		preservedWithinLevelExperience = 0
	} else {
		required := policy.ExperienceRequired(state.User.Level)
		if preservedWithinLevelExperience >= required {
			preservedWithinLevelExperience = required - 1
		}
	}
	state.User.Experience = policy.CumulativeExperience(state.User.Level) + preservedWithinLevelExperience
	state.User.NowLevelExperience = preservedWithinLevelExperience
	state.User.NextLevelExperience = policy.ExperienceRequired(state.User.Level) - preservedWithinLevelExperience
	state.User.BPMax = policy.BattlePointMaximum(state.User.Level)
	if wasBPFull {
		state.User.BP = state.User.BPMax
	} else if state.User.BP > state.User.BPMax {
		state.User.BP = state.User.BPMax
	}
	state.User.FriendMax = policy.FriendMaximum(state.User.Level)
	state.User.Jobs = policy.JobsAtLevel(state.User.Level)
	state.PlayerProgressionConfigVersion = policy.ConfigVersion
	if err := validateCNPlayerProgressionState(state.User, policy); err != nil {
		return false, err
	}
	return true, nil
}

func validateCNPlayerProgressionState(user release.User, policy release.PlayerProgressionPolicy) error {
	if user.Level < 1 || user.Level > policy.MaxLevel || user.Experience < 0 || user.NowLevelExperience < 0 ||
		user.BPMax != policy.BattlePointMaximum(user.Level) || user.FriendMax != policy.FriendMaximum(user.Level) ||
		!slices.Equal(user.Jobs, policy.JobsAtLevel(user.Level)) {
		return errors.New("CN player progression state does not match the runtime policy")
	}
	cumulative := policy.CumulativeExperience(user.Level)
	if user.Level == policy.MaxLevel {
		if user.Experience != cumulative || user.NowLevelExperience != 0 || user.NextLevelExperience != 0 {
			return errors.New("CN maximum-level progression state is invalid")
		}
		return nil
	}
	required := policy.ExperienceRequired(user.Level)
	if user.NowLevelExperience >= required || user.Experience != cumulative+user.NowLevelExperience ||
		user.NextLevelExperience != required-user.NowLevelExperience {
		return errors.New("CN player EXP state is invalid")
	}
	return nil
}
