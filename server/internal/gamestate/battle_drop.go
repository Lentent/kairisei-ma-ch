package gamestate

// Awake waves are alternative forms of the preceding mandatory encounter.
// They inherit its body reward, not a second copy of its broken-part rewards.
func TeamBattleBodyRewardWave(enemyTypes []int8, index int) int {
	for index > 0 && index < len(enemyTypes) && enemyTypes[index] == 4 {
		index--
	}
	return index
}

func TeamBattleWaveDrops(plan []TeamBattleEnemyDrop, enemyTypes []int8, index int) []TeamBattleEnemyDrop {
	source := TeamBattleBodyRewardWave(enemyTypes, index)
	var result []TeamBattleEnemyDrop
	for _, drop := range plan {
		if drop.BattleIndex == index || (source != index && drop.BattleIndex == source && drop.EnemyIndex == 0) {
			drop.BattleIndex = index
			result = append(result, drop)
		}
	}
	return result
}
