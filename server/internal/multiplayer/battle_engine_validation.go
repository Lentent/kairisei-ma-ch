package multiplayer

import (
	"errors"
	"fmt"
)

func (engine *BattleEngine) validateTarget(targetKind string, memberType int, target int) error {
	switch targetKind {
	case "SELF":
		if target != memberType {
			return errors.New("self skill target is invalid")
		}
	case "USER_ONE":
		if target < 1 || target > 4 {
			return errors.New("single ally skill target is invalid")
		}
	case "USER_ALL":
		if target != 0 && (target < 1 || target > 4) {
			return errors.New("all ally skill target is invalid")
		}
	case "ENEMY_ONE":
		if !engine.livingEnemyTarget(target) {
			return errors.New("single enemy skill target is invalid")
		}
	case "ENEMY_ALL":
		if target != 0 && !engine.livingEnemyTarget(target) {
			return errors.New("all enemy skill target is invalid")
		}
	default:
		return fmt.Errorf("unsupported combat target kind %q", targetKind)
	}
	return nil
}

func (engine *BattleEngine) livingEnemyTarget(target int) bool {
	if target == 0 {
		return engine.enemyCount > 0 && engine.enemies[0].HP > 0
	}
	if target < 5 || target >= 5+engine.enemyCount {
		return false
	}
	return engine.enemies[target-5].HP > 0
}

func (player *battlePlayer) cardInHand(cardType int) (BattleCard, bool) {
	for _, deckSlot := range player.Hand {
		if deckSlot == 0 {
			continue
		}
		card := player.Deck[deckSlot-1]
		if card.CardType == cardType {
			return card, true
		}
	}
	return BattleCard{}, false
}

func baseParameterArgs(memberType int, hp int, attack int, magic int, recovery int, defense int, magicDefense int) []int64 {
	return []int64{int64(memberType), int64(hp), int64(attack), int64(magic), int64(recovery), int64(defense), int64(magicDefense), 99999, 99999, 99999}
}

func battleParameterArgs(memberType int, hp int, maxHP int, attack int, magic int, recovery int, defense int, magicDefense int, limitAttack int, limitMagic int, limitRecovery int) []int64 {
	return []int64{
		int64(memberType), int64(hp), int64(maxHP), int64(attack), int64(magic), int64(recovery), int64(defense), int64(magicDefense),
		int64(activeParameterLimit(limitAttack)), int64(activeParameterLimit(limitMagic)), int64(activeParameterLimit(limitRecovery)),
	}
}
