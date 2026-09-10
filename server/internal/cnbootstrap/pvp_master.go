package cnbootstrap

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"kairisei.local/server/internal/httpapi"
	"kairisei.local/server/internal/release"
)

const maxCNPVPMasterBytes = 1024 * 1024

func loadCNPVPRuntimeMaster(masterPath string) (httpapi.PVPConfig, error) {
	if masterPath == "" {
		return httpapi.PVPConfig{}, errors.New("CN PVP runtime config is required")
	}
	content, err := os.ReadFile(masterPath)
	if err != nil {
		return httpapi.PVPConfig{}, fmt.Errorf("read CN PVP runtime config: %w", err)
	}
	if len(content) == 0 || len(content) > maxCNPVPMasterBytes {
		return httpapi.PVPConfig{}, errors.New("CN PVP runtime config must be non-empty and at most one MiB")
	}
	var config httpapi.PVPConfig
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return httpapi.PVPConfig{}, fmt.Errorf("decode CN PVP runtime config: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return httpapi.PVPConfig{}, errors.New("CN PVP runtime config has trailing JSON")
		}
		return httpapi.PVPConfig{}, fmt.Errorf("decode trailing CN PVP runtime config: %w", err)
	}
	if config.SchemaVersion != 2 || config.ClientProfile != "cn602-bootstrap" || config.ConfigVersion <= 0 {
		return httpapi.PVPConfig{}, errors.New("CN PVP runtime config identity is invalid")
	}
	if config.PolicyEvidence != "PLACEHOLDER_LOCAL_PVP_SERVICE_POLICY" {
		return httpapi.PVPConfig{}, errors.New("CN PVP local service policy evidence is invalid")
	}
	if config.ChallengeMax != 5 || config.CostInitial <= 0 || config.TurnMax <= 0 || config.HoldMax <= 0 ||
		config.RankWinPoint <= 0 || config.RankLosePoint > 0 || config.ReplayResult < 0 || config.ReplayResult > 1 {
		return httpapi.PVPConfig{}, errors.New("CN PVP runtime rules are invalid")
	}
	if config.EngineMode != "server_replay" && config.EngineMode != "client_native_local" {
		return httpapi.PVPConfig{}, errors.New("CN PVP runtime engine mode is invalid")
	}
	if config.EngineMode == "server_replay" && len(config.ResultCommands) == 0 {
		return httpapi.PVPConfig{}, errors.New("CN PVP server replay mode requires result commands")
	}
	if len(config.Fields) != 8 || len(config.Ranks) == 0 {
		return httpapi.PVPConfig{}, errors.New("CN PVP runtime config is missing official fields or ranks")
	}
	seenFields := make(map[int]struct{}, len(config.Fields))
	for _, field := range config.Fields {
		if field.FieldID <= 0 || field.MapID <= 0 || field.BGMID <= 0 {
			return httpapi.PVPConfig{}, errors.New("CN PVP runtime config contains an invalid field")
		}
		if _, exists := seenFields[field.FieldID]; exists {
			return httpapi.PVPConfig{}, errors.New("CN PVP runtime config contains a duplicate field")
		}
		seenFields[field.FieldID] = struct{}{}
	}
	ranks := append([]httpapi.PVPRankConfig(nil), config.Ranks...)
	sort.Slice(ranks, func(left, right int) bool { return ranks[left].PointMin < ranks[right].PointMin })
	nextPoint := 0
	for _, rank := range ranks {
		if rank.RankID < 0 || rank.Name == "" || rank.PointMin != nextPoint || rank.PointMax < rank.PointMin || rank.WinCoin <= 0 {
			return httpapi.PVPConfig{}, errors.New("CN PVP runtime config contains an invalid rank range")
		}
		nextPoint = rank.PointMax + 1
	}
	if err := validateCNPVPOfficialSources(masterPath, config); err != nil {
		return httpapi.PVPConfig{}, err
	}
	return config, nil
}

func validateCNPVPOfficialSources(masterPath string, config httpapi.PVPConfig) error {
	if len(config.OfficialSources) != 2 {
		return errors.New("CN PVP runtime config must bind both official field and rank tables")
	}
	absoluteMaster, err := filepath.Abs(masterPath)
	if err != nil {
		return fmt.Errorf("resolve CN PVP runtime config: %w", err)
	}
	projectRoot := filepath.Clean(filepath.Join(filepath.Dir(absoluteMaster), "..", ".."))
	expectedRoot := filepath.Join(projectRoot, "_local", "control", "server", "cn602-pvp-master")
	contents := make(map[string][]byte, len(config.OfficialSources))
	for _, source := range config.OfficialSources {
		if source.Role != "field" && source.Role != "rank_cn" {
			return fmt.Errorf("CN PVP runtime config has unknown official source role %q", source.Role)
		}
		if _, duplicate := contents[source.Role]; duplicate {
			return fmt.Errorf("CN PVP runtime config repeats official source role %q", source.Role)
		}
		if source.Path == "" || filepath.IsAbs(source.Path) || strings.Contains(source.Path, `\`) ||
			source.Bytes <= 0 || len(source.SHA256) != 64 {
			return errors.New("CN PVP official source record is invalid")
		}
		absoluteSource := filepath.Clean(filepath.Join(projectRoot, filepath.FromSlash(source.Path)))
		relative, err := filepath.Rel(expectedRoot, absoluteSource)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			return errors.New("CN PVP official source escaped its generated master directory")
		}
		content, err := os.ReadFile(absoluteSource)
		if err != nil {
			return fmt.Errorf("read CN PVP official %s table: %w", source.Role, err)
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(content))
		if int64(len(content)) != source.Bytes || digest != source.SHA256 {
			return fmt.Errorf("CN PVP official %s table identity mismatch", source.Role)
		}
		contents[source.Role] = content
	}
	fields, err := parseCNPVPFieldTable(contents["field"])
	if err != nil {
		return err
	}
	ranks, err := parseCNPVPRankTable(contents["rank_cn"])
	if err != nil {
		return err
	}
	if !equalCNPVPFields(fields, config.Fields) || !equalCNPVPRanks(ranks, config.Ranks) {
		return errors.New("CN PVP runtime config differs from the official field or rank table")
	}
	return nil
}

func parseCNPVPFieldTable(content []byte) ([]httpapi.PVPFieldConfig, error) {
	reader := csv.NewReader(bytes.NewReader(content))
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse CN PVP official field table: %w", err)
	}
	result := make([]httpapi.PVPFieldConfig, 0, 8)
	for _, row := range rows {
		if len(row) < 3 || !isDecimal(row[0]) {
			continue
		}
		fieldID, _ := strconv.Atoi(strings.TrimSpace(row[0]))
		mapID, mapErr := strconv.Atoi(strings.TrimSpace(row[1]))
		bgmID, bgmErr := strconv.Atoi(strings.TrimSpace(row[2]))
		if mapErr != nil || bgmErr != nil {
			return nil, errors.New("CN PVP official field table contains a non-numeric row")
		}
		result = append(result, httpapi.PVPFieldConfig{FieldID: fieldID, MapID: mapID, BGMID: bgmID})
	}
	return result, nil
}

func parseCNPVPRankTable(content []byte) ([]httpapi.PVPRankConfig, error) {
	reader := csv.NewReader(bytes.NewReader(content))
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse CN PVP official rank table: %w", err)
	}
	result := make([]httpapi.PVPRankConfig, 0, 23)
	for _, row := range rows {
		if len(row) < 5 || !isDecimal(row[0]) {
			continue
		}
		values := make([]int, 4)
		for index, raw := range []string{row[0], row[2], row[3], row[4]} {
			value, parseErr := strconv.Atoi(strings.TrimSpace(raw))
			if parseErr != nil {
				return nil, errors.New("CN PVP official rank table contains a non-numeric row")
			}
			values[index] = value
		}
		result = append(result, httpapi.PVPRankConfig{
			RankID: values[0], Name: strings.TrimSpace(row[1]), PointMin: values[1],
			PointMax: values[2], WinCoin: values[3],
		})
	}
	return result, nil
}

func isDecimal(value string) bool {
	_, err := strconv.Atoi(strings.TrimSpace(value))
	return err == nil
}

func equalCNPVPFields(left, right []httpapi.PVPFieldConfig) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func equalCNPVPRanks(left, right []httpapi.PVPRankConfig) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func normalizeCNPVPState(state *release.State, config httpapi.PVPConfig) bool {
	changed := false
	if state.PVPConfigVersion == 0 {
		state.PVP.Challenge = config.ChallengeMax
		changed = true
	}
	if state.PVPConfigVersion < config.ConfigVersion {
		state.PVPConfigVersion = config.ConfigVersion
		changed = true
	}
	if state.PVP.NextBattleID <= 0 {
		state.PVP.NextBattleID = 1
		changed = true
	}
	if state.PVP.DefenseDecks == nil {
		state.PVP.DefenseDecks = []release.PVPDeckSelection{}
		changed = true
	}
	if state.PVP.History == nil {
		state.PVP.History = []release.PVPMatch{}
		changed = true
	}
	return changed
}
