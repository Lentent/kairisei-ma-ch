package multiplayer

import "fmt"

func validateBattlePartitionContracts() error {
	for _, result := range []BattleResult{
		{Command: resultHoldSkillEnd},
		{Command: resultAttackPartition},
	} {
		row, err := result.CSV()
		if err != nil {
			return fmt.Errorf("zero-argument structural ResultCmd %d failed its boundary: %w", result.Command, err)
		}
		if row != fmt.Sprintf("%d", result.Command) {
			return fmt.Errorf("zero-argument structural ResultCmd %d encoded as %q", result.Command, row)
		}
	}

	nonTerminal := appendAttackPartition([]BattleResult{{Command: resultCardSkill}})
	if len(nonTerminal) != 2 || nonTerminal[0].Command != resultCardSkill || nonTerminal[1].Command != resultAttackPartition {
		return fmt.Errorf("ordinary user-attack partition ordering is %+v", nonTerminal)
	}
	return nil
}
