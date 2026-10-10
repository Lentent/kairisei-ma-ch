package multiplayer

import (
	"errors"
	"kairisei.local/server/internal/gamestate"
)

func (engine *BattleEngine) applyEnemyOverrides() error {
	seen := map[int]bool{}
	for _, row := range engine.enemyOverrides {
		if row.BattleIndex != engine.statWaveIndex {
			continue
		}
		if row.EnemyIndex < 0 || row.EnemyIndex >= engine.enemyCount || seen[row.EnemyIndex] {
			return errors.New("custom boss enemy slot is invalid or duplicated")
		}
		seen[row.EnemyIndex] = true
		enemy := &engine.enemies[row.EnemyIndex]
		s := row.Stats
		if enemy.EnemyID != s.EnemyID {
			return errors.New("custom boss enemy identity does not match the template")
		}
		if err := gamestate.ValidateTeamBattleEnemyStats(s); err != nil {
			return err
		}
		if err := gamestate.ValidateTeamBattleEnemyActions(row.Actions); err != nil {
			return err
		}
		for _, a := range row.Actions {
			if _, _, err := engine.catalog.CustomEnemySkill(a); err != nil {
				return err
			}
		}
		enemy.CustomActions = row.Actions
		enemy.IncludeOriginalActions = row.IncludeOriginalActions
		enemy.CustomAnimationModel = row.AnimationModel
		enemy.HP, enemy.MaxHP, enemy.BaseMaxHP = s.HP, s.HP, s.HP
		enemy.Attack, enemy.BaseAttack = s.Attack, s.Attack
		enemy.Magic, enemy.BaseMagic = s.Magic, s.Magic
		enemy.Recovery, enemy.BaseRecovery = s.Recovery, s.Recovery
		enemy.Defense, enemy.BaseDefense = s.Defense, s.Defense
		enemy.MDefense, enemy.BaseMDefense = s.MagicDefense, s.MagicDefense
		enemy.DamageReduction, enemy.AttributeFixed = s.DamageReduction, s.AttributeFixed
		enemy.Attribute, enemy.BaseAttribute = s.Attribute, s.Attribute
		if s.AttributeRates != nil {
			enemy.Level.AttributeRates = *s.AttributeRates
		}
		if s.StatusResistances != nil {
			enemy.Level.StatusResistances = *s.StatusResistances
		}
		if s.DOTReductions != nil {
			enemy.Level.DOTReductions = *s.DOTReductions
		}
	}
	return nil
}
