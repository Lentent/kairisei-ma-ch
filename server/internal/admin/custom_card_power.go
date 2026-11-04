package admin

import (
	"fmt"
	"strconv"
)

const customPowerBoost = "CUSTOM_POWER_BOOST"

func customPowerBoostRole(functionID int) []string {
	row := make([]string, 33)
	row[0], row[8], row[9], row[20] = strconv.Itoa(functionID), customPowerBoost, "SELF", "80"
	return row
}

// Scale magnitude terms, never both factors of a product. Structural fields
// (duration, count, chance, selectors, critical and chain rates) stay intact.
func customPowerColumns(code string) []int {
	switch code {
	case "ATTACK_AA", "HEAL_FIXED":
		return []int{20, 21, 22, 23}
	case "REGENERATE_FIXED":
		return []int{21, 22, 23, 24}
	case "ATK_UP_FIXED", "DEF_UP_FIXED", "ATK_BREAK_FIXED", "GUARD_BREAK_FIXED", "PARAM_LIMIT_BREAK_FIXED":
		return []int{23, 24, 25}
	case "ATK_UP_BY_SELF_PARAM", "DEF_UP_BY_SELF_PARAM", "ATK_BREAK_BY_SELF_PARAM":
		return []int{23, 24, 25}
	case "HEAL_BY_SELF_PARAM":
		return []int{21, 22, 23}
	case "ATTR_DEF_UP", "ATTR_DEF_DOWN":
		return []int{21, 22, 23, 24}
	case "ENCHANT":
		return []int{22, 23, 24}
	case "POISON", "BURN", "FREEZE", "BLEED", "ELECTRIC":
		return []int{23, 24, 25, 26}
	case "ATTACK_BARRIER", "ATTACK_BARRIER_APPOINT_ATTR":
		return []int{21, 22}
	case "ATK_OP_DAMAGE_INCREASE":
		return []int{20, 21, 22, 23}
	case "CARD_TRAP_DAMAGE":
		return []int{23, 24, 25, 26}
	}
	return nil
}

func customBoostPercent(group [][]string, chain int) int64 {
	var percent int64
	for _, role := range group {
		if role[8] == customPowerBoost && customRowInt(role, 33) <= chain {
			percent += int64(customRowInt(role, 20))
		}
	}
	return percent
}

func applyCustomPower(row []string, percent int64) error {
	if row[8] == customPowerBoost {
		row[8] = "ATK_OP_PIERCING"
		for i := 20; i < 30; i++ {
			row[i] = "0"
		}
		return nil
	}
	if percent == 0 {
		return nil
	}
	for _, col := range customPowerColumns(row[8]) {
		if col >= len(row) || row[col] == "" {
			continue
		}
		value, err := strconv.ParseInt(row[col], 10, 64)
		if err != nil || value < -100000000 || value > 100000000 {
			return fmt.Errorf("效果%s的数值无法安全提升", row[8])
		}
		value = value * (100 + percent) / 100
		if value < -2147483648 || value > 2147483647 {
			return fmt.Errorf("效果%s提升后的数值超出客户端范围", row[8])
		}
		row[col] = strconv.FormatInt(value, 10)
	}
	return nil
}
