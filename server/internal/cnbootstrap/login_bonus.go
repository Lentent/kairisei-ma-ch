package cnbootstrap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"kairisei.local/server/internal/release"
)

const maxCNLoginBonusRuntimeBytes = 64 * 1024

var cnLoginBonusDayPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

type cnLoginBonusRuntimeMaster struct {
	SchemaVersion int             `json:"schema_version"`
	ClientProfile string          `json:"client_profile"`
	State         string          `json:"state"`
	Evidence      json.RawMessage `json:"evidence"`
	release.LoginBonusPolicy
}

func loadCNLoginBonusRuntimeMaster(path string) (release.LoginBonusPolicy, error) {
	if path == "" {
		return release.LoginBonusPolicy{}, errors.New("CN login bonus runtime path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return release.LoginBonusPolicy{}, fmt.Errorf("resolve CN login bonus runtime: %w", err)
	}
	file, err := os.Open(absolute)
	if err != nil {
		return release.LoginBonusPolicy{}, fmt.Errorf("open CN login bonus runtime: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return release.LoginBonusPolicy{}, fmt.Errorf("stat CN login bonus runtime: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxCNLoginBonusRuntimeBytes {
		return release.LoginBonusPolicy{}, errors.New("CN login bonus runtime must be a non-empty regular JSON file within the size limit")
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxCNLoginBonusRuntimeBytes+1))
	decoder.DisallowUnknownFields()
	var master cnLoginBonusRuntimeMaster
	if err := decoder.Decode(&master); err != nil {
		return release.LoginBonusPolicy{}, fmt.Errorf("decode CN login bonus runtime: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return release.LoginBonusPolicy{}, err
	}
	if err := validateCNLoginBonusRuntimeMaster(master); err != nil {
		return release.LoginBonusPolicy{}, err
	}
	return cloneCNLoginBonusPolicy(master.LoginBonusPolicy), nil
}

func validateCNLoginBonusRuntimeMaster(master cnLoginBonusRuntimeMaster) error {
	policy := master.LoginBonusPolicy
	if master.SchemaVersion != 1 || master.ClientProfile != "cn602-bootstrap" || master.State != "PASS" ||
		len(master.Evidence) == 0 || bytes.Equal(bytes.TrimSpace(master.Evidence), []byte("null")) ||
		policy.ConfigVersion <= 0 || policy.DayBoundaryOffsetMinutes < -12*60 ||
		policy.DayBoundaryOffsetMinutes > 14*60 || len(policy.Cycle) == 0 || len(policy.Cycle) > 31 ||
		len(policy.Beginner) == 0 || len(policy.Beginner) > 30 ||
		len(policy.TotalMilestones) == 0 || len(policy.TotalMilestones) > 100 ||
		len(policy.SourceState) != 5 || policy.SourceState["dto_and_popup_flow"] != "CONFIRMED" ||
		policy.SourceState["inactive_array_nullability"] != "CONFIRMED" ||
		policy.SourceState["calendar_claim_policy"] != "INFERRED" ||
		policy.SourceState["daily_reward_schedule"] != "PLACEHOLDER" ||
		policy.SourceState["beginner_total_reward_schedule"] != "PLACEHOLDER" {
		return errors.New("CN login bonus runtime identity or source state is invalid")
	}
	if err := validateCNLoginBonusSchedule("daily", policy.Cycle, true); err != nil {
		return err
	}
	if err := validateCNLoginBonusSchedule("beginner", policy.Beginner, true); err != nil {
		return err
	}
	if err := validateCNLoginBonusSchedule("total", policy.TotalMilestones, false); err != nil {
		return err
	}
	return nil
}

func validateCNLoginBonusSchedule(name string, schedule []release.LoginBonusDay, consecutive bool) error {
	previousDay := 0
	for index, day := range schedule {
		if day.Day <= previousDay || (consecutive && day.Day != index+1) || day.Comment == "" ||
			day.Reward.Num <= 0 || (day.Reward.Type != 4 && day.Reward.Type != 10) ||
			day.Reward.RewardTypeID != 0 || day.Reward.CardLevel != 0 || day.Reward.CardFame != 0 ||
			day.Reward.CardLove != 0 || day.Reward.CardSkillLevels == nil {
			return fmt.Errorf("CN %s login bonus entry %d is invalid", name, index+1)
		}
		previousDay = day.Day
	}
	return nil
}

func applyCNLoginBonusRuntimeMaster(
	state *release.State,
	policy release.LoginBonusPolicy,
) (bool, error) {
	if state.LoginBonus.ConfigVersion > policy.ConfigVersion {
		return false, errors.New("CN login bonus save is newer than the runtime policy")
	}
	state.LoginBonusPolicy = cloneCNLoginBonusPolicy(policy)
	if state.LoginBonus.ConfigVersion == policy.ConfigVersion {
		if err := validateCNLoginBonusState(state.LoginBonus, policy); err != nil {
			return false, err
		}
		return false, nil
	}
	state.LoginBonus.ConfigVersion = policy.ConfigVersion
	if state.LoginBonus.CycleDay > len(policy.Cycle) {
		state.LoginBonus.CycleDay = 0
		state.LoginBonus.LastClaimDay = ""
	}
	if state.LoginBonus.BeginnerDay > len(policy.Beginner) {
		state.LoginBonus.BeginnerDay = len(policy.Beginner)
	}
	if err := validateCNLoginBonusState(state.LoginBonus, policy); err != nil {
		return false, err
	}
	return true, nil
}

func validateCNLoginBonusState(state release.LoginBonusState, policy release.LoginBonusPolicy) error {
	if state.ConfigVersion != policy.ConfigVersion || state.CycleDay < 0 ||
		state.CycleDay > len(policy.Cycle) || state.BeginnerDay < 0 ||
		state.BeginnerDay > len(policy.Beginner) || state.TotalClaims < state.CycleDay ||
		state.TotalClaims < state.BeginnerDay ||
		(state.LastClaimDay == "" && (state.CycleDay != 0 || state.BeginnerDay != 0 || state.TotalClaims != 0)) ||
		(state.LastClaimDay != "" && !cnLoginBonusDayPattern.MatchString(state.LastClaimDay)) {
		return errors.New("CN login bonus account state is invalid")
	}
	return nil
}

func cloneCNLoginBonusPolicy(policy release.LoginBonusPolicy) release.LoginBonusPolicy {
	policy.Cycle = cloneCNLoginBonusSchedule(policy.Cycle)
	policy.Beginner = cloneCNLoginBonusSchedule(policy.Beginner)
	policy.TotalMilestones = cloneCNLoginBonusSchedule(policy.TotalMilestones)
	clonedSourceState := make(map[string]string, len(policy.SourceState))
	for key, value := range policy.SourceState {
		clonedSourceState[key] = value
	}
	policy.SourceState = clonedSourceState
	return policy
}

func cloneCNLoginBonusSchedule(schedule []release.LoginBonusDay) []release.LoginBonusDay {
	cloned := append([]release.LoginBonusDay(nil), schedule...)
	for index := range cloned {
		cloned[index].Reward.CardSkillLevels = append(
			[]int16{}, cloned[index].Reward.CardSkillLevels...,
		)
	}
	return cloned
}
