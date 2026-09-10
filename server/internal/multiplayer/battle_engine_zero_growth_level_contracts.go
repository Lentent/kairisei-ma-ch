package multiplayer

import (
	"fmt"
	"strings"
)

type enemyZeroGrowthFamily struct {
	function        string
	rows            int
	roleIDs         int
	variants        int
	outerSkills     int
	actionRefs      int
	passiveRefs     int
	callRefs        int
	levels          int
	rowDigest       string
	roleDigest      string
	outerDigest     string
	levelDigest     string
	referenceDigest string
}

// validateEnemyZeroGrowthLevelContracts owns the active ordinary-enemy
// families whose current master coefficients make managed level 0 and level 1
// identical. Keeping their complete reachable matrices explicit prevents a
// later producer change from hiding behind a legacy level-zero caller.
func validateEnemyZeroGrowthLevelContracts(catalog *CombatCatalog) error {
	families := []enemyZeroGrowthFamily{
		{
			function: "PARAM_LIMIT_BREAK_FIXED", rows: 18, roleIDs: 9, variants: 9, outerSkills: 9,
			actionRefs: 0, passiveRefs: 0, callRefs: 22, levels: 22,
			rowDigest:       "bd1521498a12aa9ba46fb9f056225bc7111b508d775ad7ece08c34edbf1db606",
			roleDigest:      "07fae35ad65e2730045ddab50466fff9126cbe342af7569e89b233d767f5b807",
			outerDigest:     "8888adc99262158c1817b00b40db9840f7411858a8ed4e0b8e52336630d19a83",
			levelDigest:     "f474155120885e021ddcf0039ba6764ed6bdcb9dacc30055ca51a62c932d7ed8",
			referenceDigest: "6e923f5f3cdf1d6b3b058c71a5b4cbcc072d35962bca28bc598d71470c6148f8",
		},
		{
			function: "HEAL_BY_SELF_PARAM", rows: 43, roleIDs: 43, variants: 45, outerSkills: 45,
			actionRefs: 125, passiveRefs: 0, callRefs: 0, levels: 117,
			rowDigest:       "a0c3fa3569aacebf3f5016a9b6e0c99fe68da3dd3970243a0355aca97a313992",
			roleDigest:      "82f892fd76bcc816fd4bf67cf48cdf1f9dd91851a99aa7875717975aaf4561d1",
			outerDigest:     "c5420149a3df480dcc6ba4bb60beed08182640c7cc20f80a78e51cdf60672bcc",
			levelDigest:     "5eeca573a3941f502a3736d7d84975ace7504101ddaa147361af6820a50d59d2",
			referenceDigest: "0b2bb314ad6fc7ee4d5bb76f4c759d3aec2b042d43fc1246309dce3b75c20765",
		},
		{
			function: "DOT_VALUE_UP", rows: 180, roleIDs: 180, variants: 204, outerSkills: 179,
			actionRefs: 346, passiveRefs: 0, callRefs: 29, levels: 131,
			rowDigest:       "a24356837bda2e0e960c8505abc58b43786915033735849d862a3bc1ff22c886",
			roleDigest:      "8a065bfa986deb7c5383051d74bed51b7c41f354f96811c0f964ba62a767976e",
			outerDigest:     "a51a40944a4f4d218c5f5358160fdc49ce6f485618213be3f4d142d6bae41de5",
			levelDigest:     "02b1a1b3bc0c2b54bc09258f2e83a042dad0e407af5e665e5d9fc03519a724f7",
			referenceDigest: "45d58409742d8b2b763de731621867f69766be839aba54976ff65ca8424dbc9f",
		},
		{
			function: "BURST_GAUGE_QUICK_UP", rows: 3, roleIDs: 3, variants: 3, outerSkills: 3,
			actionRefs: 0, passiveRefs: 0, callRefs: 8, levels: 8,
			rowDigest:       "b656949c03c5ad6c7e73eaec3e2ca5decd39cbfc74c24e658bba82865cf10591",
			roleDigest:      "58c673e4c7c3489b9d775f89def48c4bf42465cf7825b5e3892be5bd6498f881",
			outerDigest:     "04d3e81a2445b2181547f98a25e5f50900da289e20c79eb58d1c63d57c7560f1",
			levelDigest:     "c2575df4867bf46a6544944e56b76c6ab4c495d76b77ddd7667e0dee59824f31",
			referenceDigest: "8ebf6a028efbef87b41f5b248aa1435998e4ec301656f56fc1577ea489f8e4a3",
		},
	}
	for _, family := range families {
		if err := validateEnemyZeroGrowthFamily(catalog, family); err != nil {
			return err
		}
	}
	return nil
}

func validateEnemyZeroGrowthFamily(catalog *CombatCatalog, family enemyZeroGrowthFamily) error {
	if calibratedEnemySkillLevel(family.function) != 1 {
		return fmt.Errorf("official enemy zero-growth %s managed level is not calibrated to one", family.function)
	}
	rows := make([]CombatSkillRole, 0, family.rows)
	roleIDs := make(map[int]bool)
	for _, roleSet := range catalog.EnemySkillRoles {
		for _, role := range roleSet {
			if role.Function != family.function {
				continue
			}
			if enemyZeroGrowthCoefficient(role) != 0 {
				return fmt.Errorf("official enemy zero-growth %s row gained a level coefficient: %+v", family.function, role)
			}
			rows = append(rows, role)
			roleIDs[role.SkillID] = true
		}
	}
	if len(rows) != family.rows {
		return fmt.Errorf("official enemy zero-growth %s row count is %d, want %d", family.function, len(rows), family.rows)
	}
	outerSkillIDs := make(map[int]bool)
	variantCount := 0
	for skillID, variants := range catalog.EnemySkills {
		for _, variant := range variants {
			if !roleIDs[variant.FunctionID] {
				continue
			}
			variantCount++
			outerSkillIDs[skillID] = true
		}
	}
	actionRecords := make([]string, 0)
	passiveRecords := make([]string, 0)
	callRecords := make([]string, 0)
	referenceRecords := make([]string, 0)
	referencingLevels := make(map[int]bool)
	for levelID, level := range catalog.EnemyLevels {
		if outerSkillIDs[level.PassiveSkillID] {
			referencingLevels[levelID] = true
			record := fmt.Sprintf("%d|PASSIVE|%d", levelID, level.PassiveSkillID)
			passiveRecords = append(passiveRecords, record)
			referenceRecords = append(referenceRecords, record)
		}
		for index, skillID := range level.CallSkillIDs {
			if !outerSkillIDs[skillID] {
				continue
			}
			referencingLevels[levelID] = true
			record := fmt.Sprintf("%d|CALL|%d|%d", levelID, index, skillID)
			callRecords = append(callRecords, record)
			referenceRecords = append(referenceRecords, record)
		}
		for _, action := range level.Actions {
			if !outerSkillIDs[action.SkillID] {
				continue
			}
			referencingLevels[levelID] = true
			record := fmt.Sprintf("%d|ACTION|%d|%s|%d|%d|%d|%s|%s|%d|%d|%d|%t",
				levelID, action.Slot, action.Category, action.SkillID, action.AIConditionID, action.Priority,
				action.Target, strings.Join(action.TargetParams[:], ","), action.ActionCost, action.MaxUses, action.Rate, action.CountOnMiss)
			actionRecords = append(actionRecords, record)
			referenceRecords = append(referenceRecords, record)
		}
	}
	if len(roleIDs) != family.roleIDs || variantCount != family.variants || len(outerSkillIDs) != family.outerSkills ||
		len(actionRecords) != family.actionRefs || len(passiveRecords) != family.passiveRefs || len(callRecords) != family.callRefs ||
		len(referencingLevels) != family.levels {
		return fmt.Errorf("official enemy zero-growth %s reachability changed: roles=%d variants=%d outers=%d action=%d passive=%d call=%d levels=%d",
			family.function, len(roleIDs), variantCount, len(outerSkillIDs), len(actionRecords), len(passiveRecords), len(callRecords), len(referencingLevels))
	}
	if digest := combatRoleMatrixDigest(rows); digest != family.rowDigest {
		return fmt.Errorf("official enemy zero-growth %s row digest is %s", family.function, digest)
	}
	if digest := combatIntSetDigest(roleIDs); digest != family.roleDigest {
		return fmt.Errorf("official enemy zero-growth %s role digest is %s", family.function, digest)
	}
	if digest := combatIntSetDigest(outerSkillIDs); digest != family.outerDigest {
		return fmt.Errorf("official enemy zero-growth %s outer-skill digest is %s", family.function, digest)
	}
	if digest := combatIntSetDigest(referencingLevels); digest != family.levelDigest {
		return fmt.Errorf("official enemy zero-growth %s level digest is %s", family.function, digest)
	}
	if digest := combatStringRecordsDigest(referenceRecords); digest != family.referenceDigest {
		return fmt.Errorf("official enemy zero-growth %s reference digest is %s", family.function, digest)
	}
	return nil
}

func enemyZeroGrowthCoefficient(role CombatSkillRole) int {
	switch role.Function {
	case "PARAM_LIMIT_BREAK_FIXED":
		return combatParameterInt(role.Parameters[4]) + combatParameterInt(role.Parameters[5])
	case "HEAL_BY_SELF_PARAM":
		return combatParameterInt(role.Parameters[2]) + combatParameterInt(role.Parameters[3])
	case "DOT_VALUE_UP":
		return combatParameterInt(role.Parameters[2]) + combatParameterInt(role.Parameters[4])
	case "BURST_GAUGE_QUICK_UP":
		return combatParameterInt(role.Parameters[1])
	default:
		return -1
	}
}
