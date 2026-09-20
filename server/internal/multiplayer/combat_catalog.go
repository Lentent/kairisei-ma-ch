package multiplayer

import (
	"bufio"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"kairisei.local/server/internal/protocol"
)

const maxCombatMasterBytes = 16 * 1024 * 1024

// CombatCatalog is an immutable in-memory projection of the official CN card
// and battle TextAssets. It deliberately contains no player or room state.
type CombatCatalog struct {
	Cards             map[int]CombatCardDefinition
	Spheres           map[int]CombatSphereDefinition
	Buddies           map[int]CombatBuddyDefinition
	BurstGauge        CombatBurstGaugeConfig
	TranceRates       [4][2][5]int
	PlayerSkills      map[int][]CombatSkillDefinition
	PlayerSkillRoles  map[int][]CombatSkillRole
	BurstSkills       map[int][]CombatSkillDefinition
	BurstSkillRoles   map[int][]CombatSkillRole
	SupportSkills     map[int][]CombatSkillDefinition
	SupportSkillRoles map[int][]CombatSkillRole
	SupportLoveRates  [3]int
	RoleParamRules    map[string]CombatRoleParameterRule
	EnemyParties      map[int]CombatEnemyParty
	Enemies           map[int]CombatEnemyDefinition
	EnemyLevels       map[int]CombatEnemyLevel
	EnemyAIOrders     map[int]CombatEnemyAIOrder
	EnemySkills       map[int][]CombatSkillDefinition
	EnemySkillRoles   map[int][]CombatSkillRole
}

type CombatSphereDefinition struct {
	ID             int
	SameSphereID   int
	Name           string
	Type           string
	MaxLevel       int
	SkillID        int
	PassiveSkillID int
	CallSkillID    int
	Count          int
	PlayCondition  string
	PlayParam      int
}

type CombatBurstGaugeConfig struct {
	NormalThreshold int
	Maximum         int
	BreakTurns      int
	DamageReduction int
	ReactionRates   [5]int
}

type CombatBuddyDefinition struct {
	ID             int
	SameBuddyID    int
	Name           string
	Rarity         string
	MaxLevel       int
	PassiveSkillID int
	Skills         [3]CombatBuddySkill
}

type CombatBuddySkill struct {
	SkillID       int
	CallSkillID   int
	RequiredLevel int
	Genre         string
	GaugeCost     int
	RechargeTurn  int
}

// CombatRoleParameterRule preserves the ten parameter-column types declared by
// the official CN skill_role_param_rule TextAsset. It is a client/master
// consumption contract, not a server-authored combat formula.
type CombatRoleParameterRule struct {
	Function   string
	Parameters [10]string
}

type CombatCardDefinition struct {
	// CardCsvData.profile.group_ids, not SkillData's target-hand groups.
	ProfileTags     [8]int
	LoveMax         int
	SupportSkillIDs [4]int
	ID              int
	BaseCardID      int
	Rarity          string
	MaxLevel        int
	NormalSkillID   int
	ArthurSkillID   int
	CallSkillID     int
	PassiveSkillID  int
	HPBase          int
	HPGrowth        int
	AttackBase      int
	AttackGrowth    int
	MagicBase       int
	MagicGrowth     int
	RecoveryBase    int
	RecoveryGrowth  int
}

type CombatSkillDefinition struct {
	DisplayRole       int // skill_value_role_no, one-based 1..5; zero means no value
	ID                int
	Name              string
	Kind              string
	Attribute         string
	Job               string
	DamageKind        string
	Cost              int
	PriorityPVE       int
	Rank              string
	HateRatio         int
	Target            string
	HandSelectCount   int
	HandAttribute     string
	HandKind          string
	HandMinCost       int
	HandMaxCost       int
	Groups            [3]int
	AppendTrigger     string
	AppendDuration    int
	AppendCondition   string
	AppendParameters  [5]string
	BranchCondition   string
	BranchParameters  [5]string
	BranchCondition2  string
	BranchParameters2 [5]string
	BranchPriority    int
	FunctionID        int
}

type CombatSkillRole struct {
	SourceSkillID int // Bound on an execution copy; immutable CSV SkillID is the role-set key.
	SkillID       int
	RoleIndex     int
	Effect2D      string
	Effect3D      string
	HitEffect     string
	HitPosition   string
	Function      string
	Target        string
	ExcludeSelf   bool
	Attributes    [9]bool
	// CSV masks are explicit, including an empty mask. Programmatic operator
	// roles without a target rule leave this false (not a native NULL mask).
	HasTargetAttributes bool
	Parameters          [10]string
	ChainRate           int
	HateLimit           int
}

type CombatEnemyParty struct {
	ID        int
	Placement string
	Slots     [4]CombatEnemyPartySlot
	BGMID     int
	FieldID   int
	HintID    int
}

type CombatEnemyPartySlot struct {
	EnemyID     int
	HPRate      int
	ParentIndex int
}

type CombatEnemyDefinition struct {
	TranceLimit     int
	OverheatLimit   int
	OverheatTurns   int
	OverheatResist  int
	ID              int
	RaceID          int
	ModelID         string
	ImageID         int
	Live2DName      string
	Name            string
	Attribute       string
	HP              int
	Attack          int
	Magic           int
	Recovery        int
	Defense         int
	MagicDefense    int
	DamageReduction int
	Size            string
	AttributeFixed  [5]int
}

type CombatEnemyLevel struct {
	ID             int
	HPBars         int
	AttributeRates [9]int
	// Indexed by native BAD_STATUS, including NULL=0. CSV has only 1..11.
	StatusResistances [15]int
	DOTReductions     [5]int
	InitialAwake      int
	ActionsPerTurn    int
	PassiveSkillID    int
	CallSkillIDs      [10]int
	Actions           []CombatEnemyAction
}

type CombatEnemyAction struct {
	Slot          int
	Category      string
	SkillID       int
	AIConditionID int
	Priority      int
	Target        string
	// EnemySkillData.target_param0..4 are Int64 after managed conversion, but
	// their CSV contract is symbolic for selectors such as USER_DEBUFF
	// (DARKNESS/CARD_TRAP_DAMAGE/WEAKNESS). Keep the source text and parse it
	// only in selectors whose native contract is numeric.
	TargetParams [5]string
	ActionCost   int
	MaxUses      int
	Rate         int
	CountOnMiss  bool
}

type CombatEnemyAIOrder struct {
	ID     int
	Fields []string
}

func LoadCombatCatalog(cardMasterPath string, battleMasterRoot string) (*CombatCatalog, error) {
	cardPath, err := resolveCombatMasterFile(cardMasterPath)
	if err != nil {
		return nil, fmt.Errorf("resolve card master: %w", err)
	}
	battleRoot, err := filepath.Abs(battleMasterRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve battle master root: %w", err)
	}
	info, err := os.Stat(battleRoot)
	if err != nil || !info.IsDir() {
		return nil, errors.New("battle master root is unavailable")
	}

	catalog := &CombatCatalog{
		Cards:             make(map[int]CombatCardDefinition),
		Spheres:           make(map[int]CombatSphereDefinition),
		Buddies:           make(map[int]CombatBuddyDefinition),
		PlayerSkills:      make(map[int][]CombatSkillDefinition),
		PlayerSkillRoles:  make(map[int][]CombatSkillRole),
		BurstSkills:       make(map[int][]CombatSkillDefinition),
		BurstSkillRoles:   make(map[int][]CombatSkillRole),
		SupportSkills:     make(map[int][]CombatSkillDefinition),
		SupportSkillRoles: make(map[int][]CombatSkillRole),
		RoleParamRules:    make(map[string]CombatRoleParameterRule),
		EnemyParties:      make(map[int]CombatEnemyParty),
		Enemies:           make(map[int]CombatEnemyDefinition),
		EnemyLevels:       make(map[int]CombatEnemyLevel),
		EnemyAIOrders:     make(map[int]CombatEnemyAIOrder),
		EnemySkills:       make(map[int][]CombatSkillDefinition),
		EnemySkillRoles:   make(map[int][]CombatSkillRole),
	}
	if err := readCombatCardCSV(cardPath, func(row []string) error {
		definition, err := parseCombatCard(row)
		if err != nil {
			return err
		}
		if _, exists := catalog.Cards[definition.ID]; exists {
			return fmt.Errorf("duplicate card ID %d", definition.ID)
		}
		catalog.Cards[definition.ID] = definition
		return nil
	}); err != nil {
		return nil, fmt.Errorf("load card master: %w", err)
	}

	load := func(name string, consume func([]string) error) error {
		path, err := resolveCombatFileWithin(battleRoot, name)
		if err != nil {
			return err
		}
		if err := readCombatRows(path, name != "trance_gauge_reaction_rate.csv", consume); err != nil {
			return fmt.Errorf("load %s: %w", name, err)
		}
		return nil
	}
	if err := load("buddy.csv", func(row []string) error {
		value, err := parseCombatBuddy(row)
		if err != nil {
			return err
		}
		if _, exists := catalog.Buddies[value.ID]; exists {
			return fmt.Errorf("duplicate buddy ID %d", value.ID)
		}
		catalog.Buddies[value.ID] = value
		return nil
	}); err != nil {
		return nil, err
	}
	if err := load("sphr.csv", func(row []string) error {
		value, err := parseCombatSphere(row)
		if err != nil {
			return err
		}
		if _, exists := catalog.Spheres[value.ID]; exists {
			return fmt.Errorf("duplicate sphere ID %d", value.ID)
		}
		catalog.Spheres[value.ID] = value
		return nil
	}); err != nil {
		return nil, err
	}
	if err := load("trance_gauge_reaction_rate.csv", func(row []string) error {
		kinds := map[string]int{"DAMAGE": 0, "ETC_DAMAGE": 1, "DEBUFF": 2, "COVERING": 3}
		kind, ok := kinds[row[0]]
		if !ok || len(row) < 11 {
			return errors.New("invalid trance reaction row")
		}
		for state := range 2 {
			for cost := range 5 {
				catalog.TranceRates[kind][state][cost] = optionalCombatInt(row, 1+state*5+cost)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := load("burst_gauge.csv", func(row []string) error {
		if catalog.BurstGauge.Maximum != 0 {
			return errors.New("duplicate burst gauge row")
		}
		if len(row) < 4 {
			return errors.New("burst gauge row is too short")
		}
		catalog.BurstGauge = CombatBurstGaugeConfig{
			NormalThreshold: optionalCombatInt(row, 0), Maximum: optionalCombatInt(row, 1),
			BreakTurns: optionalCombatInt(row, 2), DamageReduction: optionalCombatInt(row, 3),
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := load("burst_gauge_reaction_rate.csv", func(row []string) error {
		if len(row) < len(catalog.BurstGauge.ReactionRates) {
			return errors.New("burst gauge reaction-rate row is too short")
		}
		for index := range catalog.BurstGauge.ReactionRates {
			catalog.BurstGauge.ReactionRates[index] = optionalCombatInt(row, index)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := load("skill_player.csv", func(row []string) error {
		value, err := parseCombatSkill(row)
		if err == nil {
			catalog.PlayerSkills[value.ID] = append(catalog.PlayerSkills[value.ID], value)
		}
		return err
	}); err != nil {
		return nil, err
	}
	if err := load("skill_role_player.csv", func(row []string) error {
		value, err := parseCombatSkillRole(row)
		if err == nil {
			value.RoleIndex = len(catalog.PlayerSkillRoles[value.SkillID])
			catalog.PlayerSkillRoles[value.SkillID] = append(catalog.PlayerSkillRoles[value.SkillID], value)
		}
		return err
	}); err != nil {
		return nil, err
	}
	if err := load("burst_skill.csv", func(row []string) error {
		value, err := parseCombatSkill(row)
		if err == nil {
			catalog.BurstSkills[value.ID] = append(catalog.BurstSkills[value.ID], value)
		}
		return err
	}); err != nil {
		return nil, err
	}
	if err := load("burst_skill_role.csv", func(row []string) error {
		value, err := parseCombatSkillRole(row)
		if err == nil {
			value.RoleIndex = len(catalog.BurstSkillRoles[value.SkillID])
			catalog.BurstSkillRoles[value.SkillID] = append(catalog.BurstSkillRoles[value.SkillID], value)
		}
		return err
	}); err != nil {
		return nil, err
	}
	if err := load("support_skill.csv", func(row []string) error {
		value, err := parseCombatSkill(row)
		if err == nil {
			catalog.SupportSkills[value.ID] = append(catalog.SupportSkills[value.ID], value)
		}
		return err
	}); err != nil {
		return nil, err
	}
	if err := load("support_skill_lvup.csv", func(row []string) error {
		level := optionalCombatInt(row, 0)
		if level < 1 || level > len(catalog.SupportLoveRates) {
			return fmt.Errorf("invalid support skill level %d", level)
		}
		catalog.SupportLoveRates[level-1] = optionalCombatInt(row, 1)
		return nil
	}); err != nil {
		return nil, err
	}
	if rates := catalog.SupportLoveRates; rates[0] <= 0 || rates[1] <= rates[0] || rates[2] <= rates[1] || rates[2] > 100 {
		return nil, fmt.Errorf("invalid support love thresholds %v", rates)
	}
	if err := load("support_skill_role.csv", func(row []string) error {
		value, err := parseCombatSkillRole(row)
		if err == nil {
			value.RoleIndex = len(catalog.SupportSkillRoles[value.SkillID])
			catalog.SupportSkillRoles[value.SkillID] = append(catalog.SupportSkillRoles[value.SkillID], value)
		}
		return err
	}); err != nil {
		return nil, err
	}
	parameterRulePath, err := resolveCombatFileWithin(battleRoot, "skill_role_param_rule.csv")
	if err != nil {
		return nil, err
	}
	if err := readCombatParameterRules(parameterRulePath, catalog.RoleParamRules); err != nil {
		return nil, fmt.Errorf("load skill_role_param_rule.csv: %w", err)
	}
	if err := load("enemy_party.csv", func(row []string) error {
		value, err := parseCombatEnemyParty(row)
		if err != nil {
			return err
		}
		if _, exists := catalog.EnemyParties[value.ID]; exists {
			return fmt.Errorf("duplicate enemy party ID %d", value.ID)
		}
		catalog.EnemyParties[value.ID] = value
		return nil
	}); err != nil {
		return nil, err
	}
	if err := load("enemy.csv", func(row []string) error {
		value, err := parseCombatEnemy(row)
		if err != nil {
			return err
		}
		if _, exists := catalog.Enemies[value.ID]; exists {
			return fmt.Errorf("duplicate enemy ID %d", value.ID)
		}
		catalog.Enemies[value.ID] = value
		return nil
	}); err != nil {
		return nil, err
	}
	if err := load("enemy_lvup.csv", func(row []string) error {
		value, err := parseCombatEnemyLevel(row)
		if err != nil {
			return err
		}
		if _, exists := catalog.EnemyLevels[value.ID]; exists {
			return fmt.Errorf("duplicate enemy level ID %d", value.ID)
		}
		catalog.EnemyLevels[value.ID] = value
		return nil
	}); err != nil {
		return nil, err
	}
	if err := load("enemy_ai_order.csv", func(row []string) error {
		id, err := requiredCombatInt(row, 0, "enemy AI ID")
		if err != nil {
			return err
		}
		if _, exists := catalog.EnemyAIOrders[id]; exists {
			return fmt.Errorf("duplicate enemy AI ID %d", id)
		}
		fields := make([]string, maxInt(38, len(row)))
		copy(fields, row)
		catalog.EnemyAIOrders[id] = CombatEnemyAIOrder{ID: id, Fields: fields}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := load("skill_enemy.csv", func(row []string) error {
		value, err := parseCombatSkill(row)
		if err == nil {
			catalog.EnemySkills[value.ID] = append(catalog.EnemySkills[value.ID], value)
		}
		return err
	}); err != nil {
		return nil, err
	}
	if err := load("skill_role_enemy.csv", func(row []string) error {
		value, err := parseCombatSkillRole(row)
		if err == nil {
			value.RoleIndex = len(catalog.EnemySkillRoles[value.SkillID])
			catalog.EnemySkillRoles[value.SkillID] = append(catalog.EnemySkillRoles[value.SkillID], value)
		}
		return err
	}); err != nil {
		return nil, err
	}
	if err := catalog.Validate(); err != nil {
		return nil, err
	}
	return catalog, nil
}

func (c *CombatCatalog) Validate() error {
	if c == nil || len(c.Cards) == 0 || len(c.Spheres) == 0 || len(c.Buddies) == 0 || len(c.PlayerSkills) == 0 || len(c.PlayerSkillRoles) == 0 ||
		len(c.BurstSkills) == 0 || len(c.BurstSkillRoles) == 0 || len(c.SupportSkills) == 0 || len(c.SupportSkillRoles) == 0 ||
		len(c.RoleParamRules) == 0 ||
		len(c.EnemyParties) == 0 || len(c.Enemies) == 0 || len(c.EnemyLevels) == 0 ||
		len(c.EnemyAIOrders) == 0 || len(c.EnemySkills) == 0 || len(c.EnemySkillRoles) == 0 {
		return errors.New("combat catalog is incomplete")
	}
	if c.BurstGauge.NormalThreshold <= 0 || c.BurstGauge.Maximum < c.BurstGauge.NormalThreshold || c.BurstGauge.BreakTurns <= 0 {
		return errors.New("combat burst gauge config is invalid")
	}
	for index, rate := range c.BurstGauge.ReactionRates {
		if rate <= 0 {
			return fmt.Errorf("combat burst gauge reaction rate %d is invalid", index+1)
		}
	}
	for sphereID, sphere := range c.Spheres {
		if sphere.SameSphereID <= 0 || sphere.MaxLevel <= 0 || sphere.Count <= 0 ||
			(sphere.Type != "NORMAL" && sphere.Type != "CHALICE") {
			return fmt.Errorf("official sphere %d has invalid core data", sphereID)
		}
		if len(c.PlayerSkills[sphere.SkillID]) == 0 {
			return fmt.Errorf("official sphere %d references missing player skill %d", sphereID, sphere.SkillID)
		}
		if sphere.PassiveSkillID != 0 && len(c.SupportSkills[sphere.PassiveSkillID]) == 0 {
			return fmt.Errorf("official sphere %d references missing support skill %d", sphereID, sphere.PassiveSkillID)
		}
		if sphere.CallSkillID != 0 && len(c.PlayerSkills[sphere.CallSkillID]) == 0 {
			return fmt.Errorf("official sphere %d references missing call skill %d", sphereID, sphere.CallSkillID)
		}
		switch sphere.PlayCondition {
		case "", "NONE":
		case "TURN_AFTER":
			if sphere.PlayParam < 0 {
				return fmt.Errorf("official sphere %d has invalid TURN_AFTER parameter", sphereID)
			}
		default:
			return fmt.Errorf("official sphere %d has unsupported play condition %q", sphereID, sphere.PlayCondition)
		}
	}
	for partyID, party := range c.EnemyParties {
		seen := 0
		for _, slot := range party.Slots {
			if slot.EnemyID == 0 {
				continue
			}
			seen++
			if _, exists := c.Enemies[slot.EnemyID]; !exists {
				return fmt.Errorf("enemy party %d references missing enemy %d", partyID, slot.EnemyID)
			}
			if _, exists := c.EnemyLevels[slot.EnemyID]; !exists {
				return fmt.Errorf("enemy party %d references missing enemy level %d", partyID, slot.EnemyID)
			}
		}
		if seen == 0 {
			return fmt.Errorf("enemy party %d is empty", partyID)
		}
	}
	return nil
}

// ValidateRoleParameterContracts checks the exact official parameter columns
// used by the status families simulated by the Go engine. The column meaning
// remains data-driven; numeric zero is valid for scale terms and appointed
// darkness slots, so only syntax and schema are gated here.
func (c *CombatCatalog) ValidateRoleParameterContracts() error {
	expected := map[string][]string{
		"ATTACK_AA":                     {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "ATTR", "SKILL_PHYSICS_TYPE"},
		"ATK_OP_DRAIN":                  {"VALUE", "VALUE", "VALUE"},
		"ATK_OP_DRAIN_ALL":              {"VALUE", "VALUE", "VALUE"},
		"ATK_OP_REVENGE":                {"VALUE", "VALUE", "SKILL_ROLE_BATTLE_PARAM", "VALUE"},
		"ATK_OP_NOW_TURN_REVENGE":       {"VALUE", "VALUE", "SKILL_ROLE_BATTLE_PARAM", "VALUE"},
		"ATK_OP_PIERCING":               {"VALUE", "VALUE"},
		"ATK_OP_DAMAGE_INCREASE":        {"VALUE", "VALUE", "VALUE", "VALUE", "SKILL_ROLE_BATTLE_PARAM", "VALUE"},
		"ATK_OP_ATTR_RATE_DOWN_INVALID": {},
		"DEAL_BONUS":                    {"VALUE"},
		"DEAL_PENALTY":                  {"VALUE"},
		"DEAL_PENALTY_TURN_APPOINT":     {"VALUE", "VALUE"},
		"HEAL_FIXED":                    {"VALUE", "VALUE", "VALUE", "VALUE", "SKILL_ROLE_BATTLE_PARAM"},
		"HEAL_BY_TARGET_MAXHP":          {"VALUE", "VALUE", "VALUE", "VALUE"},
		"HEAL_BY_SELF_PARAM":            {"SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE", "VALUE", "VALUE"},
		"REGENERATE_FIXED":              {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "SKILL_ROLE_BATTLE_PARAM"},
		"REGENERATE_BY_SELF_PARAM":      {"VALUE", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE", "VALUE", "VALUE"},
		"DESTRUCT":                      {"VALUE", "VALUE", "VALUE"},
		"ATTR_SEE":                      {"VALUE"},
		"HP_CUT":                        {"VALUE", "VALUE"},
		"BURST_GAUGE_QUICK_UP":          {"VALUE", "VALUE"},
		"ENEMY_AI_TRIGGER_FLAG_SET":     {"VALUE", "VALUE"},
		"ENEMY_AWAKE_FLAG_SET":          {"VALUE"},
		"FORCE_BATTLE_END":              {},
		"HEAL_REVERSE":                  {"VALUE", "VALUE", "VALUE"},
		"ATTR_HIDE":                     {"VALUE"},
		"ENDURE":                        {"VALUE", "VALUE", "VALUE"},
		"ENEMY_CURSE":                   {"VALUE", "VALUE", "VALUE"},
		"GUTS":                          {"VALUE", "VALUE", "VALUE"},
		"PARAM_LIMIT_BREAK_FIXED":       {"VALUE", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE", "VALUE", "VALUE"},
		"ATK_UP_BY_SELF_PARAM":          {"VALUE", "SKILL_ROLE_BATTLE_PARAM", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE", "VALUE", "VALUE"},
		"ATK_UP_FIXED":                  {"VALUE", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE", "VALUE", "VALUE"},
		"DEF_UP_BY_SELF_PARAM":          {"VALUE", "SKILL_ROLE_BATTLE_PARAM", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE", "VALUE", "VALUE"},
		"DEF_UP_FIXED":                  {"VALUE", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE", "VALUE", "VALUE"},
		"ATK_BREAK_BY_SELF_PARAM":       {"VALUE", "SKILL_ROLE_BATTLE_PARAM", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE", "VALUE", "VALUE"},
		"ATK_BREAK_FIXED":               {"VALUE", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE", "VALUE", "VALUE"},
		"GUARD_BREAK_BY_SELF_PARAM":     {"VALUE", "SKILL_ROLE_BATTLE_PARAM", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE", "VALUE", "VALUE"},
		"GUARD_BREAK_FIXED":             {"VALUE", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE", "VALUE", "VALUE"},
		"CRITICAL_UP":                   {"VALUE", "VALUE", "VALUE"},
		"CRITICAL_DOWN":                 {"VALUE", "VALUE", "VALUE"},
		"WEAKNESS":                      {"VALUE", "VALUE", "VALUE"},
		"ENCHANT":                       {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "ATTR"},
		"COVERING":                      {"VALUE", "VALUE", "VALUE", "ATTR", "SKILL_PHYSICS_TYPE"},
		"COST_BLOCK":                    {"VALUE", "VALUE"},
		"REVIVE":                        {"VALUE", "VALUE", "VALUE"},
		"BLESS":                         {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "VALUE"},
		"BLESS_TURN_UP":                 {"VALUE", "ATTR"},
		"BLESS_TURN_DOWN":               {"VALUE", "ATTR"},
		"DARKNESS_RANDOM":               {"VALUE", "VALUE", "VALUE"},
		"DARKNESS_APPOINT":              {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "VALUE"},
		"CARD_TRAP_DAMAGE":              {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "SKILL_ROLE_BATTLE_PARAM"},
		"CARD_SEAL":                     {"VALUE", "VALUE", "VALUE", "VALUE", "SKILL_KIND_BIT", "ATTR_BIT", "VALUE", "VALUE", "VALUE"},
		"CARD_SEAL_REGIST":              {"VALUE", "VALUE", "VALUE"},
		"DARKNESS_REGIST":               {"VALUE", "VALUE", "VALUE"},
		"DEBUFF_REGIST":                 {"VALUE", "VALUE", "SKILL_ROLE_KIND_DEBUFF"},
		"STAN":                          {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE"},
		"ATTR_DEF_UP":                   {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "ATTR"},
		"ATTR_DEF_DOWN":                 {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "ATTR"},
		"BUFF_RELEASE":                  {"VALUE", "VALUE"},
		"DEBUFF_RELEASE":                {"VALUE", "VALUE"},
		"BUFF_RELEASE_ONE":              {"VALUE", "VALUE", "SKILL_ROLE_KIND_BUFF", "SKILL_ROLE_KIND_BUFF"},
		"DEBUFF_RELEASE_ONE":            {"VALUE", "VALUE", "SKILL_ROLE_KIND_DEBUFF", "SKILL_ROLE_KIND_DEBUFF"},
		"BUFF_RELEASE_ONE_NUM":          {"VALUE", "VALUE", "SKILL_ROLE_KIND_BUFF", "SKILL_ROLE_KIND_BUFF", "VALUE"},
		"DEBUFF_RELEASE_ONE_NUM":        {"VALUE", "VALUE", "SKILL_ROLE_KIND_DEBUFF", "SKILL_ROLE_KIND_DEBUFF", "VALUE"},
		"BUFF_RELEASE_RANDOM":           {"VALUE", "VALUE", "VALUE"},
		"DEBUFF_RELEASE_RANDOM":         {"VALUE", "VALUE", "VALUE"},
		"BUFF_RELEASE_OLD":              {"VALUE", "VALUE", "VALUE"},
		"DEBUFF_RELEASE_OLD":            {"VALUE", "VALUE", "VALUE"},
		"DAMAGE_BOOST":                  {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "ATTR", "SKILL_PHYSICS_TYPE", "VALUE", "VALUE"},
		"ATK_UP_BOOST":                  {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "ATTR", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE"},
		"DEF_UP_BOOST":                  {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "ATTR", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE"},
		"ATK_BREAK_BOOST":               {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "ATTR", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE"},
		"GUARD_BREAK_BOOST":             {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "ATTR", "SKILL_ROLE_BATTLE_PARAM", "VALUE", "VALUE"},
		"HEAL_BOOST":                    {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "ATTR", "VALUE", "VALUE"},
		"CRITICAL_BOOST":                {"VALUE", "VALUE", "VALUE", "ATTR", "SKILL_PHYSICS_TYPE", "VALUE", "VALUE"},
		"DAMAGE_CUT2":                   {"VALUE", "VALUE", "VALUE", "VALUE", "VALUE", "ATTR", "SKILL_PHYSICS_TYPE"},
	}
	for function, columns := range expected {
		rule, exists := c.RoleParamRules[function]
		if !exists {
			return fmt.Errorf("official CN parameter rule %s is missing", function)
		}
		for index := range rule.Parameters {
			wanted := ""
			if index < len(columns) {
				wanted = columns[index]
			}
			if rule.Parameters[index] != wanted {
				return fmt.Errorf("official CN parameter rule %s column %d is %q, want %q", function, index+1, rule.Parameters[index], wanted)
			}
		}
	}
	validateRoles := func(label string, roles map[int][]CombatSkillRole) error {
		for skillID, variants := range roles {
			for _, role := range variants {
				columns, relevant := expected[role.Function]
				if !relevant {
					continue
				}
				for index, column := range columns {
					if column != "VALUE" {
						continue
					}
					text := strings.TrimSpace(role.Parameters[index])
					if text == "" || strings.EqualFold(text, "ALL") {
						continue
					}
					value, err := strconv.Atoi(text)
					if err != nil || value < 0 {
						// The official CN master contains a few legacy fixed/self-family
						// cells whose enum-like text remains in VALUE columns. Managed
						// SkillDataContainer.getRoleParam calls
						// Global.parseInt(text, 0), so these rows intentionally reach
						// libbattle5 as numeric zero. Keep the exception exact instead
						// of treating arbitrary malformed VALUE cells as valid.
						fixedP2Fallback := index == 2 && text == "MND" && (role.Function == "ATK_UP_FIXED" || role.Function == "DEF_UP_FIXED" || role.Function == "ATK_BREAK_FIXED" || role.Function == "GUARD_BREAK_FIXED")
						fixedP5Fallback := index == 5 && text == "DARK" && role.Function == "ATK_UP_FIXED"
						selfP5Fallback := index == 5 && text == "INT" && role.Function == "ATK_UP_BY_SELF_PARAM"
						if fixedP2Fallback || fixedP5Fallback || selfP5Fallback {
							continue
						}
						return fmt.Errorf("official CN %s skill %d %s numeric parameter %d is invalid", label, skillID, role.Function, index+1)
					}
				}
				if (role.Function == "DEAL_BONUS" || role.Function == "DEAL_PENALTY" || role.Function == "DEAL_PENALTY_TURN_APPOINT") && combatParameterIntForValidation(role.Parameters[0]) <= 0 {
					return fmt.Errorf("official CN %s skill %d %s primary value is not positive", label, skillID, role.Function)
				}
			}
		}
		return nil
	}
	if err := validateRoles("player", c.PlayerSkillRoles); err != nil {
		return err
	}
	if err := validateRoles("enemy", c.EnemySkillRoles); err != nil {
		return err
	}
	return validateRoles("support", c.SupportSkillRoles)
}

func combatParameterIntForValidation(value string) int {
	parsed, _ := strconv.Atoi(strings.TrimSpace(value))
	return parsed
}

// ValidatePlayerFunctionCoverage is the startup capability gate for every
// official CN card in the supplied card master. It prevents an unregistered
// primary effect from surviving until a live battle happens to draw it.
func (c *CombatCatalog) ValidatePlayerFunctionCoverage() error {
	if err := c.ValidateCardSupportFunctionCoverage(); err != nil {
		return err
	}
	missing := make(map[string]struct{})
	validateSkill := func(skillID int) {
		if skillID <= 0 {
			return
		}
		for _, variant := range c.PlayerSkills[skillID] {
			functionID := variant.FunctionID
			if functionID == 0 {
				functionID = skillID
			}
			for _, role := range c.PlayerSkillRoles[functionID] {
				if role.Function != "" && !playerCombatFunctionRegistered(role.Function) {
					missing[role.Function] = struct{}{}
				} else if combatFunctionNeedsBuffCode(role.Function) {
					if _, exists := battleBuffCodes[role.Function]; !exists {
						missing[role.Function+"(buff-code)"] = struct{}{}
					}
				}
			}
		}
	}
	for _, card := range c.Cards {
		if _, err := c.cardBeginningDraw(card); err != nil {
			return err
		}
		if card.CallSkillID != 0 {
			if _, _, err := c.CardCallSkill(card.ID); err != nil {
				return err
			}
			for _, skill := range c.PlayerSkills[card.CallSkillID] {
				if err := validateAppendSkillDefinition("card call", skill); err != nil {
					return err
				}
				if len(c.PlayerSkillRoles[skill.FunctionID]) == 0 {
					return fmt.Errorf("card %d call skill role %d is incomplete", card.ID, skill.FunctionID)
				}
			}
		}
		for _, skillID := range []int{card.NormalSkillID, card.ArthurSkillID, card.CallSkillID} {
			validateSkill(skillID)
		}
	}
	for _, sphere := range c.Spheres {
		validateSkill(sphere.SkillID)
		validateSkill(sphere.CallSkillID)
	}
	if len(missing) == 0 {
		return nil
	}
	functions := make([]string, 0, len(missing))
	for function := range missing {
		functions = append(functions, function)
	}
	sort.Strings(functions)
	return fmt.Errorf("unregistered official CN player combat functions: %s", strings.Join(functions, ", "))
}

// ValidateBurstFunctionCoverage gates every buddy active/passive role reachable
// from the immutable CN 6.0.2 master. A newly introduced official function must
// fail server startup instead of surfacing halfway through a multiplayer turn.
func (c *CombatCatalog) ValidateBurstFunctionCoverage() error {
	missing := make(map[string]struct{})
	for skillID, variants := range c.BurstSkills {
		for _, variant := range variants {
			if _, ok := combatSkillTargetCode(variant.Target); !ok {
				return fmt.Errorf("official burst skill %d has unsupported SKILL_TARGET %q", skillID, variant.Target)
			}
			roles := c.BurstSkillRoles[variant.FunctionID]
			if len(roles) == 0 {
				return fmt.Errorf("official burst skill %d references missing role set %d", skillID, variant.FunctionID)
			}
		}
	}
	for _, roles := range c.BurstSkillRoles {
		for _, role := range roles {
			if role.Function != "" && !burstCombatFunctionRegistered(role.Function) {
				missing[role.Function] = struct{}{}
			} else if combatFunctionNeedsBuffCode(role.Function) {
				if _, exists := battleBuffCodes[role.Function]; !exists {
					missing[role.Function+"(buff-code)"] = struct{}{}
				}
			}
		}
	}
	for buddyID, buddy := range c.Buddies {
		if buddy.PassiveSkillID != 0 && len(c.BurstSkills[buddy.PassiveSkillID]) == 0 {
			return fmt.Errorf("official buddy %d references missing passive skill %d", buddyID, buddy.PassiveSkillID)
		}
		for slot, skill := range buddy.Skills {
			if skill.SkillID != 0 && len(c.BurstSkills[skill.SkillID]) == 0 {
				return fmt.Errorf("official buddy %d slot %d references missing burst skill %d", buddyID, slot+1, skill.SkillID)
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	functions := make([]string, 0, len(missing))
	for function := range missing {
		functions = append(functions, function)
	}
	sort.Strings(functions)
	return fmt.Errorf("unregistered official CN burst combat functions: %s", strings.Join(functions, ", "))
}

func burstCombatFunctionRegistered(function string) bool {
	switch function {
	case "ATK_UP_FIXED", "DEF_UP_FIXED", "PARAM_LIMIT_BREAK_FIXED", "BURST_GAUGE_QUICK_UP", "ENCHANT",
		"NEED_COST_DOWN_BURST", "ATTACK_MULTISTAGE", "ADD_ATK_OP_PIERCING", "DAMAGE_BOOST_ORDER_TRIBAL",
		"ATK_UP_BOOST_ORDER_TRIBAL", "DEF_UP_BOOST_ORDER_TRIBAL", "CRITICAL_DAMAGE_BOOST", "DISCARD_DRAW",
		"LIMIT_BREAK_BONUS", "PARAM_UP_SKILL_BONUS":
		return true
	default:
		return false
	}
}

// ValidateEnemyFunctionCoverage gates the complete official CN enemy role
// catalog, including bosses which are not currently published in the UI.
func (c *CombatCatalog) ValidateEnemyFunctionCoverage() error {
	missing := make(map[string]struct{})
	for _, roles := range c.EnemySkillRoles {
		for _, role := range roles {
			if role.Function != "" && !enemyCombatFunctionRegistered(role.Function) {
				missing[role.Function] = struct{}{}
			} else if combatFunctionNeedsBuffCode(role.Function) {
				if _, exists := battleBuffCodes[role.Function]; !exists {
					missing[role.Function+"(buff-code)"] = struct{}{}
				}
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	functions := make([]string, 0, len(missing))
	for function := range missing {
		functions = append(functions, function)
	}
	sort.Strings(functions)
	return fmt.Errorf("unregistered official CN enemy combat functions: %s", strings.Join(functions, ", "))
}

func (c *CombatCatalog) CardSkill(cardID int, arthurType int) (CombatSkillDefinition, []CombatSkillRole, error) {
	card, exists := c.Cards[cardID]
	if !exists {
		return CombatSkillDefinition{}, nil, fmt.Errorf("card %d is unavailable", cardID)
	}
	skillID := card.NormalSkillID
	if c.cardUsesArthurSkill(cardID, arthurType) {
		skillID = card.ArthurSkillID
	}
	variants := c.PlayerSkills[skillID]
	if len(variants) == 0 {
		return CombatSkillDefinition{}, nil, fmt.Errorf("card %d skill %d is incomplete", cardID, skillID)
	}
	roles := c.PlayerSkillRoles[variants[0].FunctionID]
	if len(roles) == 0 {
		return CombatSkillDefinition{}, nil, fmt.Errorf("card %d skill role %d is incomplete", cardID, variants[0].FunctionID)
	}
	return variants[0], append([]CombatSkillRole(nil), roles...), nil
}

// 49c52 -> 79676 compares the base Arthur skill's exact Arthur/job value.
// NULL is not a wildcard. 795fb rejects the three-explicit-NONE sentinel;
// missing extend slots are zero-initialized NULL, not NONE.
func (c *CombatCatalog) cardUsesArthurSkill(cardID, arthurType int) bool {
	card := c.Cards[cardID]
	variants := c.PlayerSkills[card.ArthurSkillID]
	job := combatArthurJob(arthurType)
	if card.ArthurSkillID <= 0 || len(variants) == 0 || job == "" || variants[0].Job != job {
		return false
	}
	if len(variants) >= 3 {
		empty := true
		for _, variant := range variants[:3] {
			roles := c.PlayerSkillRoles[variant.FunctionID]
			if len(roles) == 0 || roles[0].Function != "NONE" {
				empty = false
				break
			}
		}
		if empty {
			return false
		}
	}
	return true
}

func (c *CombatCatalog) SphereSkill(sphereID int) (CombatSkillDefinition, []CombatSkillRole, error) {
	sphere, exists := c.Spheres[sphereID]
	if !exists {
		return CombatSkillDefinition{}, nil, fmt.Errorf("sphere %d is unavailable", sphereID)
	}
	variants := c.PlayerSkills[sphere.SkillID]
	if len(variants) == 0 {
		return CombatSkillDefinition{}, nil, fmt.Errorf("sphere %d skill %d is incomplete", sphereID, sphere.SkillID)
	}
	roles := c.PlayerSkillRoles[variants[0].FunctionID]
	if len(roles) == 0 {
		return CombatSkillDefinition{}, nil, fmt.Errorf("sphere %d skill role %d is incomplete", sphereID, variants[0].FunctionID)
	}
	return variants[0], append([]CombatSkillRole(nil), roles...), nil
}

func (c *CombatCatalog) SphereCallSkill(sphereID int) (CombatSkillDefinition, []CombatSkillRole, error) {
	sphere, exists := c.Spheres[sphereID]
	if !exists {
		return CombatSkillDefinition{}, nil, fmt.Errorf("sphere %d is unavailable", sphereID)
	}
	if sphere.CallSkillID == 0 {
		return CombatSkillDefinition{}, nil, nil
	}
	variants := c.PlayerSkills[sphere.CallSkillID]
	if len(variants) == 0 {
		return CombatSkillDefinition{}, nil, fmt.Errorf("sphere %d call skill %d is incomplete", sphereID, sphere.CallSkillID)
	}
	roles := c.PlayerSkillRoles[variants[0].FunctionID]
	if len(roles) == 0 {
		return CombatSkillDefinition{}, nil, fmt.Errorf("sphere %d call skill role %d is incomplete", sphereID, variants[0].FunctionID)
	}
	return variants[0], append([]CombatSkillRole(nil), roles...), nil
}

func (c *CombatCatalog) CardCallSkill(cardID int) (CombatSkillDefinition, []CombatSkillRole, error) {
	card, exists := c.Cards[cardID]
	if !exists {
		return CombatSkillDefinition{}, nil, fmt.Errorf("card %d is unavailable", cardID)
	}
	if card.CallSkillID == 0 {
		return CombatSkillDefinition{}, nil, nil
	}
	variants := c.PlayerSkills[card.CallSkillID]
	if len(variants) == 0 {
		return CombatSkillDefinition{}, nil, fmt.Errorf("card %d call skill %d is incomplete", cardID, card.CallSkillID)
	}
	roles := c.PlayerSkillRoles[variants[0].FunctionID]
	if len(roles) == 0 {
		return CombatSkillDefinition{}, nil, fmt.Errorf("card %d call skill role %d is incomplete", cardID, variants[0].FunctionID)
	}
	return variants[0], append([]CombatSkillRole(nil), roles...), nil
}

func combatArthurJob(arthurType int) string {
	switch arthurType {
	case 1:
		return "MERCENARY"
	case 2:
		return "MILLIONAIRE"
	case 3:
		return "THIEF"
	case 4:
		return "SINGER"
	default:
		return ""
	}
}

func resolveCombatMasterFile(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxCombatMasterBytes {
		return "", errors.New("combat master file is unavailable or outside the size boundary")
	}
	return absolute, nil
}

func resolveCombatFileWithin(root string, name string) (string, error) {
	path, err := resolveCombatMasterFile(filepath.Join(root, name))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("combat master file escapes its root")
	}
	return path, nil
}

// Cards use CSV.parserFast in the original client. Localized dialogue must
// not carry an RFC CSV quote state into the next card's physical line.
func readCombatCardCSV(path string, consume func([]string) error) error {
	stream, err := os.Open(path)
	if err != nil {
		return err
	}
	defer stream.Close()
	lines := bufio.NewScanner(io.LimitReader(stream, maxCombatMasterBytes+1))
	lines.Buffer(make([]byte, 4096), maxCombatMasterBytes)
	rows := 0
	for lines.Scan() {
		row := protocol.SplitCSVLine(lines.Text())
		row[0] = strings.TrimPrefix(row[0], "\ufeff")
		if !isDecimalCombatID(row[0]) {
			continue
		}
		if err := consume(row); err != nil {
			return fmt.Errorf("numeric card row %d: %w", rows+1, err)
		}
		rows++
	}
	if err := lines.Err(); err != nil {
		return err
	}
	if rows == 0 {
		return errors.New("combat card master contains no numeric rows")
	}
	return nil
}

func readCombatCSV(path string, consume func([]string) error) error {
	return readCombatRows(path, true, consume)
}

func readCombatRows(path string, numeric bool, consume func([]string) error) error {
	stream, err := os.Open(path)
	if err != nil {
		return err
	}
	defer stream.Close()
	reader := csv.NewReader(io.LimitReader(stream, maxCombatMasterBytes+1))
	reader.FieldsPerRecord = -1
	// The official CN tables contain a small number of literal quote characters
	// inside otherwise unquoted display-text fields. Python's exporter preserves
	// those bytes, so accept that source dialect here instead of rewriting the
	// immutable master data.
	reader.LazyQuotes = true
	rows := 0
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if len(row) == 0 {
			continue
		}
		row[0] = strings.TrimPrefix(row[0], "\ufeff")
		if row[0] == "" || strings.HasPrefix(row[0], "#") || numeric && !isDecimalCombatID(row[0]) {
			continue
		}
		if err := consume(row); err != nil {
			return fmt.Errorf("data row %d: %w", rows+1, err)
		}
		rows++
	}
	if rows == 0 {
		return errors.New("combat master contains no data rows")
	}
	return nil
}

func readCombatParameterRules(path string, rules map[string]CombatRoleParameterRule) error {
	stream, err := os.Open(path)
	if err != nil {
		return err
	}
	defer stream.Close()
	reader := csv.NewReader(io.LimitReader(stream, maxCombatMasterBytes+1))
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if len(row) == 0 {
			continue
		}
		function := strings.TrimSpace(strings.TrimPrefix(row[0], "\ufeff"))
		if function == "" || strings.HasPrefix(function, "#") {
			continue
		}
		if _, duplicate := rules[function]; duplicate {
			return fmt.Errorf("duplicate skill role parameter rule %s", function)
		}
		rule := CombatRoleParameterRule{Function: function}
		for index := range rule.Parameters {
			rule.Parameters[index] = combatField(row, 3+index)
		}
		rules[function] = rule
	}
	if len(rules) == 0 {
		return errors.New("combat parameter rules contain no functions")
	}
	return nil
}

func parseCombatCard(row []string) (CombatCardDefinition, error) {
	if len(row) < 28 {
		return CombatCardDefinition{}, errors.New("card row is too short")
	}
	id, err := requiredCombatInt(row, 0, "card ID")
	if err != nil {
		return CombatCardDefinition{}, err
	}
	card := CombatCardDefinition{
		ID: id, BaseCardID: optionalCombatInt(row, 1), Rarity: combatField(row, 7),
		MaxLevel: optionalCombatInt(row, 23), NormalSkillID: optionalCombatInt(row, 26),
		ArthurSkillID: optionalCombatInt(row, 27), CallSkillID: optionalCombatInt(row, 32), PassiveSkillID: optionalCombatInt(row, 33),
		LoveMax:         optionalCombatInt(row, 24),
		SupportSkillIDs: [4]int{optionalCombatInt(row, 28), optionalCombatInt(row, 29), optionalCombatInt(row, 30), optionalCombatInt(row, 31)},
		HPBase:          optionalCombatInt(row, 10),
		HPGrowth:        optionalCombatInt(row, 11), AttackBase: optionalCombatInt(row, 13),
		AttackGrowth: optionalCombatInt(row, 14), MagicBase: optionalCombatInt(row, 16),
		MagicGrowth: optionalCombatInt(row, 17), RecoveryBase: optionalCombatInt(row, 19),
		RecoveryGrowth: optionalCombatInt(row, 20),
	}
	for index := range card.ProfileTags {
		if tag := optionalCombatInt(row, 54+index); tag != -1 {
			card.ProfileTags[index] = tag
		}
	}
	return card, nil
}

func parseCombatSphere(row []string) (CombatSphereDefinition, error) {
	if len(row) < 20 {
		return CombatSphereDefinition{}, errors.New("sphere row is too short")
	}
	id, err := requiredCombatInt(row, 0, "sphere ID")
	if err != nil {
		return CombatSphereDefinition{}, err
	}
	return CombatSphereDefinition{
		ID: id, SameSphereID: optionalCombatInt(row, 1), Name: combatField(row, 2),
		Type: strings.ToUpper(combatField(row, 4)), MaxLevel: maxInt(1, optionalCombatInt(row, 7)),
		SkillID: optionalCombatInt(row, 13), PassiveSkillID: optionalCombatInt(row, 14),
		CallSkillID: optionalCombatInt(row, 15), Count: optionalCombatInt(row, 17),
		PlayCondition: strings.ToUpper(combatField(row, 18)), PlayParam: optionalCombatInt(row, 19),
	}, nil
}

func parseCombatBuddy(row []string) (CombatBuddyDefinition, error) {
	if len(row) < 25 {
		return CombatBuddyDefinition{}, errors.New("buddy row is too short")
	}
	id, err := requiredCombatInt(row, 0, "buddy ID")
	if err != nil {
		return CombatBuddyDefinition{}, err
	}
	value := CombatBuddyDefinition{
		ID: id, SameBuddyID: optionalCombatInt(row, 1), Name: combatField(row, 2),
		Rarity: combatField(row, 3), MaxLevel: optionalCombatInt(row, 5),
		PassiveSkillID: optionalCombatInt(row, 6),
	}
	for index := range value.Skills {
		offset := 7 + index*6
		value.Skills[index] = CombatBuddySkill{
			SkillID: optionalCombatInt(row, offset), CallSkillID: optionalCombatInt(row, offset+1),
			RequiredLevel: optionalCombatInt(row, offset+2), Genre: combatField(row, offset+3),
			GaugeCost: optionalCombatInt(row, offset+4), RechargeTurn: optionalCombatInt(row, offset+5),
		}
	}
	return value, nil
}

func parseCombatSkill(row []string) (CombatSkillDefinition, error) {
	if len(row) < 50 {
		return CombatSkillDefinition{}, errors.New("skill row is too short")
	}
	id, err := requiredCombatInt(row, 0, "skill ID")
	if err != nil {
		return CombatSkillDefinition{}, err
	}
	value := CombatSkillDefinition{
		ID: id, Name: combatField(row, 1), DisplayRole: optionalCombatInt(row, 5), Kind: combatField(row, 10),
		Attribute: combatField(row, 11), Job: combatField(row, 12),
		DamageKind: combatField(row, 13), Cost: optionalCombatInt(row, 14),
		PriorityPVE: optionalCombatInt(row, 15), Rank: combatField(row, 17),
		HateRatio: optionalCombatInt(row, 18), Target: combatField(row, 19),
		HandSelectCount: optionalCombatInt(row, 20), HandAttribute: combatField(row, 21),
		HandKind: combatField(row, 22), HandMinCost: optionalCombatInt(row, 23), HandMaxCost: optionalCombatInt(row, 24),
		AppendTrigger: combatField(row, 28), AppendDuration: optionalCombatInt(row, 29), AppendCondition: combatField(row, 30),
		BranchCondition: combatField(row, 36), BranchCondition2: combatField(row, 42), BranchPriority: optionalCombatInt(row, 48),
		FunctionID: optionalCombatInt(row, 49),
	}
	for index := range value.Groups {
		value.Groups[index] = optionalCombatInt(row, 25+index)
	}
	for index := range value.BranchParameters {
		value.AppendParameters[index] = combatField(row, 31+index)
		value.BranchParameters[index] = combatField(row, 37+index)
		value.BranchParameters2[index] = combatField(row, 43+index)
	}
	if value.FunctionID == 0 {
		value.FunctionID = value.ID
	}
	return value, nil
}

func parseCombatSkillRole(row []string) (CombatSkillRole, error) {
	if len(row) < 32 {
		return CombatSkillRole{}, errors.New("skill role row is too short")
	}
	id, err := requiredCombatInt(row, 0, "skill role ID")
	if err != nil {
		return CombatSkillRole{}, err
	}
	value := CombatSkillRole{
		SkillID: id, Effect2D: combatField(row, 1), Effect3D: combatField(row, 3),
		HitEffect: combatField(row, 4), HitPosition: combatField(row, 5),
		Function: combatField(row, 8), Target: combatField(row, 9),
		ExcludeSelf: optionalCombatInt(row, 10) == 1, HasTargetAttributes: true,
		ChainRate: optionalCombatInt(row, 30), HateLimit: optionalCombatInt(row, 31),
	}
	for index := range value.Attributes {
		value.Attributes[index] = optionalCombatInt(row, 11+index) > 0
	}
	for index := range value.Parameters {
		value.Parameters[index] = combatField(row, 20+index)
	}
	return value, nil
}

func parseCombatEnemyParty(row []string) (CombatEnemyParty, error) {
	if len(row) < 16 {
		return CombatEnemyParty{}, errors.New("enemy party row is too short")
	}
	id, err := requiredCombatInt(row, 0, "enemy party ID")
	if err != nil {
		return CombatEnemyParty{}, err
	}
	value := CombatEnemyParty{ID: id, Placement: combatField(row, 1), BGMID: optionalCombatInt(row, 14), FieldID: optionalCombatInt(row, 15), HintID: optionalCombatInt(row, 16)}
	for index := range value.Slots {
		base := 2 + index*3
		value.Slots[index] = CombatEnemyPartySlot{EnemyID: optionalCombatInt(row, base), HPRate: optionalCombatInt(row, base+1), ParentIndex: optionalCombatInt(row, base+2)}
		if value.Slots[index].HPRate == 0 {
			value.Slots[index].HPRate = 1
		}
	}
	return value, nil
}

func parseCombatEnemy(row []string) (CombatEnemyDefinition, error) {
	if len(row) < 25 {
		return CombatEnemyDefinition{}, errors.New("enemy row is too short")
	}
	id, err := requiredCombatInt(row, 0, "enemy ID")
	if err != nil {
		return CombatEnemyDefinition{}, err
	}
	return CombatEnemyDefinition{
		ID: id, RaceID: optionalCombatInt(row, 1), ModelID: combatField(row, 2),
		ImageID: optionalCombatInt(row, 4), Live2DName: combatField(row, 5), Name: combatField(row, 6),
		Attribute: combatField(row, 7), HP: optionalCombatInt(row, 8), Attack: optionalCombatInt(row, 9),
		Magic: optionalCombatInt(row, 10), Recovery: optionalCombatInt(row, 11),
		Defense: optionalCombatInt(row, 12), MagicDefense: optionalCombatInt(row, 13),
		DamageReduction: optionalCombatInt(row, 14), Size: combatField(row, 24),
		AttributeFixed: [5]int{optionalCombatInt(row, 15), optionalCombatInt(row, 16), optionalCombatInt(row, 17), optionalCombatInt(row, 18), optionalCombatInt(row, 19)},
		TranceLimit:    optionalCombatInt(row, 20), OverheatLimit: optionalCombatInt(row, 21),
		OverheatTurns: optionalCombatInt(row, 22), OverheatResist: optionalCombatInt(row, 23),
	}, nil
}

func parseCombatEnemyLevel(row []string) (CombatEnemyLevel, error) {
	if len(row) < 313 {
		return CombatEnemyLevel{}, errors.New("enemy level row is too short")
	}
	id, err := requiredCombatInt(row, 0, "enemy level ID")
	if err != nil {
		return CombatEnemyLevel{}, err
	}
	value := CombatEnemyLevel{
		ID: id, HPBars: optionalCombatInt(row, 1), InitialAwake: optionalCombatInt(row, 27),
		ActionsPerTurn: optionalCombatInt(row, 28), PassiveSkillID: optionalCombatInt(row, 29),
	}
	for index := range value.AttributeRates {
		value.AttributeRates[index] = optionalCombatInt(row, 2+index)
	}
	for index := 1; index <= 11; index++ {
		value.StatusResistances[index] = optionalCombatInt(row, 10+index)
	}
	for index := range value.DOTReductions {
		value.DOTReductions[index] = optionalCombatInt(row, 22+index)
	}
	for index := range value.CallSkillIDs {
		value.CallSkillIDs[index] = optionalCombatInt(row, 30+index)
	}
	actionSlot := 0
	appendAction := func(category string, offset int) {
		skillID := optionalCombatInt(row, offset)
		if skillID == 0 {
			actionSlot++
			return
		}
		action := CombatEnemyAction{
			Slot: actionSlot, Category: category, SkillID: skillID, AIConditionID: optionalCombatInt(row, offset+1),
			Priority: optionalCombatInt(row, offset+2), Target: combatField(row, offset+3),
			ActionCost: optionalCombatInt(row, offset+9), MaxUses: optionalCombatInt(row, offset+10),
			Rate: optionalCombatInt(row, offset+11), CountOnMiss: optionalCombatInt(row, offset+12) != 0,
		}
		for index := range action.TargetParams {
			action.TargetParams[index] = combatField(row, offset+4+index)
		}
		value.Actions = append(value.Actions, action)
		actionSlot++
	}
	appendAction("normal", 40)
	for offset := 53; offset <= 222; offset += 13 {
		appendAction("skill", offset)
	}
	for offset := 235; offset <= 287; offset += 13 {
		appendAction("special", offset)
	}
	appendAction("death", 300)
	return value, nil
}

func combatField(row []string, index int) string {
	if index < 0 || index >= len(row) {
		return ""
	}
	value := strings.TrimSpace(row[index])
	if strings.EqualFold(value, "NULL") {
		return ""
	}
	return value
}

func optionalCombatInt(row []string, index int) int {
	value := combatField(row, index)
	if value == "" {
		return 0
	}
	parsed, _ := strconv.Atoi(value)
	return parsed
}

func requiredCombatInt(row []string, index int, label string) (int, error) {
	value := combatField(row, index)
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s is invalid", label)
	}
	return parsed, nil
}

func isDecimalCombatID(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
