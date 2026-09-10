package multiplayer

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	resultTurn              = 1
	resultEnd               = 2
	resultHP                = 3
	resultBaseParam         = 5
	resultBattleParam       = 6
	resultWaitAndSee        = 7
	resultContinue          = 9
	resultContinueWait      = 10
	resultCost              = 12
	resultEnableTarget      = 13
	resultCostBlock         = 14
	resultCardDeal          = 20
	resultCardDeck          = 21
	resultCardPlay          = 22
	resultCardIdentity      = 23
	resultCardWeak          = 24
	resultCardUpdate        = 25
	resultCardCost          = 27
	resultCardPass          = 28
	resultCardState         = 29
	resultStun              = 31
	resultRewrite           = 32
	resultRevive            = 11
	resultBuff              = 62
	resultPassiveBuff       = 69
	resultHPCut             = 64
	resultDebuffFailed      = 65
	resultBuffRelease       = 66
	resultBuffReleaseFailed = 67
	resultSkillRevive       = 68
	resultBuffPartition     = 70
	resultBuffLostOne       = 71
	resultChargeStart       = 74
	resultPartsBreak        = 80
	resultEnemyBreak        = 81
	resultGameOver          = 82
	resultEnemyFirstAttack  = 85
	resultAppendTurnAdd     = 206
	resultHoldSet           = 310
	resultHoldRemainder     = 311
	resultHoldMax           = 312
	resultHoldStore         = 313
	resultHoldLost          = 316
	resultBurstCondition    = 320
	resultBuddy             = 321
	resultBuddySlot         = 322
	resultBurstDiscard      = 323
	resultAddCardBuff       = 324
	resultCardBuff          = 325
	resultAttackPartition   = 502
	resultBurstStateChange  = 504
	resultBurstState        = 505
	resultBurstGaugeState   = 506
	resultBurstSkill        = 700
	resultEnemyAttackSign   = 86
	resultPartsBreakDrop    = 83
	resultEnemyBreakDrop    = 84
	resultPlayLogState      = 602
	resultHoldSkill         = 58
	resultHoldSkillEnd      = 59
	resultResumeTurn        = 100
	resultResumeCardHand    = 102
	resultResumeCardDeck    = 103
	resultResumeBuff        = 104
	resultResumeEnemy       = 106
	resultResumeGameOver    = 107
	resultResumeEnemyDrop   = 108
	resultResumeHaveDrop    = 109
	resultResumeHold        = 110
)

var battleBuffCodes = map[string]int{
	"ATK_UP_FIXED": 0, "ATK_UP_BY_SELF_PARAM": 1, "CRITICAL_UP": 10,
	"ATK_UP_BY_NOW_TURN_DAMAGE": 8, "ATK_UP_BY_TARGET_PARAM": 1,
	"DAMAGE_BOOST": 22, "ATK_UP_BOOST": 23, "DEF_UP_BOOST": 24, "ATK_BREAK_BOOST": 25,
	"GUARD_BREAK_BOOST": 26, "HEAL_BOOST": 27, "CRITICAL_BOOST": 28, "DAMAGE_CUT2": 29,
	"DEF_UP_FIXED": 4, "DEF_UP_BY_SELF_PARAM": 5,
	"DAMAGE_UP": 2, "DAMAGE_CUT": 3,
	"ATTR_DEF_UP": 19, "ENCHANT": 20, "PARAM_LIMIT_BREAK_FIXED": 32,
	"ATK_BREAK_FIXED": 100, "ATK_BREAK_BY_SELF_PARAM": 101,
	"ATK_BREAK_BY_NOW_TURN_DAMAGE": 109, "ATK_BREAK_BY_TARGET_PARAM": 115,
	"GUARD_BREAK_FIXED": 102, "GUARD_BREAK_BY_SELF_PARAM": 103, "GUARD_BREAK_BY_TARGET_PARAM": 104,
	"GUARD_BREAK_BY_NOW_TURN_DAMAGE": 110, "ATTR_SEE": 107, "CARD_TRAP_DAMAGE": 112,
	"ATTR_DEF_DOWN": 113, "CRITICAL_DOWN": 114, "CARD_SEAL_REGIST": 203,
	"ATTACK_BARRIER_APPOINT_ATTR": 205, "DARKNESS_REGIST": 207, "REFLECTION": 208,
	"GUTS": 209, "COVERING": 206, "STAN": 301, "POISON": 304,
	"BURN": 305, "FREEZE": 306, "BLEED": 307, "WEAKNESS": 308,
	"ELECTRIC": 309, "COST_BLOCK": 311, "DEAL_BONUS": 400,
	"DEAL_PENALTY": 408, "ENDURE": 409, "ENEMY_CURSE": 410,
	"BLESS": 411, "CARD_SEAL": 106, "DARKNESS_APPOINT": 111,
	"DARKNESS_RANDOM": 111, "ATTR_HIDE": 204, "ATTACK_BARRIER": 205,
	"HEAL_REVERSE": 108, "DEBUFF_REGIST": 14, "DAMAGE_DOWN": 105, "REGENERATE_FIXED": 200,
	"REGENERATE_BY_SELF_PARAM": 200, "REWRITE": 202, "DEAL_PENALTY_TURN_APPOINT": 408,
	"CRITICAL_DAMAGE_BOOST": 216,
	"BEGINNING_DRAW":        412,
}

// resultCommandMinimumArgs is derived from the CN 6.0.2 ResultCmd consumers in
// BattleResultCmdFunctionBase and BattleDirectionBase. Keeping the boundary
// here makes a malformed engine row fail before it can become a managed-client
// array overrun.
var resultCommandMinimumArgs = map[int]int{
	1: 2, 2: 1, 3: 4, 5: 10, 6: 11, 7: 0, 9: 1, 10: 0, 11: 1, 12: 2, 13: 2, 14: 2,
	20: 3, 21: 3, 22: 5, 23: 4, 24: 6, 25: 10, 27: 4, 28: 1, 29: 4, 31: 1,
	32: 2, 50: 7, 51: 12, 53: 2, 56: 11, 58: 11, 59: 0, 60: 10, 61: 4, 62: 11,
	64: 4, 65: 3, 66: 8, 67: 2, 68: 4, 69: 11, 70: 0, 71: 3, 72: 5, 74: 3, 80: 5, 81: 5, 82: 1, 83: 5, 84: 5, 86: 2, 94: 3,
	85: 0, 100: 2, 102: 11, 103: 2, 104: 11, 106: 7, 107: 1, 108: 5, 109: 3, 110: 8,
	200: 4, 201: 7, 202: 2, 206: 5, 207: 4, 300: 3, 301: 4, 302: 3, 303: 7,
	304: 3, 305: 3, 306: 3, 307: 9, 308: 4, 309: 5, 314: 5, 315: 3, 318: 2, 319: 2,
	310: 9, 311: 5, 312: 2, 313: 1, 316: 4, 320: 5, 321: 3, 322: 9, 323: 6,
	324: 4, 325: 3, 502: 0, 504: 4, 505: 5, 506: 2, 602: 3, 700: 11,
}

// BattleResult is the exact integer-row transport consumed by ResultCmd.
// It intentionally has no JSON or region-specific representation.
type BattleResult struct {
	Command int
	Args    []int64
}

// battleEndResult mirrors FUN_00061d60's reachable TeamBattle END rows.
// 74d97 -> 4a944 formats signed 64-bit arguments. The zero following the
// end type in x86 decompilation is its high word, not another CSV field.
func battleEndResult(endType int) BattleResult {
	return BattleResult{Command: resultEnd, Args: []int64{int64(endType)}}
}

// battleGameOverResult mirrors battle5_api_gameover's ResultCmd82 row.
func battleGameOverResult(memberType int) BattleResult {
	return BattleResult{Command: resultGameOver, Args: []int64{int64(memberType)}}
}

func (result BattleResult) CSV() (string, error) {
	if result.Command <= 0 || result.Command >= 1000 {
		return "", errors.New("battle result command is invalid")
	}
	if minimum, exists := resultCommandMinimumArgs[result.Command]; exists && len(result.Args) < minimum {
		return "", fmt.Errorf("battle result command %d requires at least %d arguments, got %d", result.Command, minimum, len(result.Args))
	}
	fields := make([]string, 1, len(result.Args)+1)
	fields[0] = strconv.Itoa(result.Command)
	for _, argument := range result.Args {
		fields = append(fields, strconv.FormatInt(argument, 10))
	}
	return strings.Join(fields, ","), nil
}

func encodeBattleResults(results []BattleResult) (string, error) {
	if len(results) == 0 {
		return "", errors.New("battle result group is empty")
	}
	rows := make([]string, 0, len(results))
	for _, result := range results {
		row, err := result.CSV()
		if err != nil {
			return "", err
		}
		rows = append(rows, row)
	}
	return strings.Join(rows, "\n"), nil
}

func encodeOptionalBattleResults(results []BattleResult) (string, error) {
	if len(results) == 0 {
		return "", nil
	}
	return encodeBattleResults(results)
}

func cardPlayPlanResult(engine *BattleEngine, memberType int, submission cardPlaySubmission) (string, error) {
	if memberType < 1 || memberType > maxRoomMembers {
		return "", errors.New("card play plan member type is invalid")
	}
	if engine == nil {
		return "", errors.New("card play plan battle engine is unavailable")
	}
	results := make([]BattleResult, 0, len(submission.CardTypes))
	for index, cardType := range submission.CardTypes {
		if cardType == 0 {
			continue
		}
		darkness := engine.playerCardDarkness(memberType, cardType)
		state := 0
		if _, sealed := playerCardSealEffect(&engine.players[memberType-1], cardType); sealed {
			state |= 1
		}
		if _, trapped := playerCardTrapEffect(&engine.players[memberType-1], cardType); trapped {
			state |= 2
		}
		results = append(results, BattleResult{Command: 303, Args: []int64{
			int64(memberType), int64(cardType), int64(submission.Targets[index]), 0, 0, int64(darkness), int64(state),
		}})
	}
	if submission.SphereSlot != 0 {
		action, err := engine.validateSphereSubmission(memberType, submission)
		if err != nil {
			return "", err
		}
		results = append(results, BattleResult{Command: resultSpherePlayPlan, Args: []int64{
			int64(memberType), int64(action.sphereSlot), int64(action.target),
		}})
	}
	if len(results) == 0 {
		// The original 303 consumer clears the teammate's previous selection
		// when cardType is zero. Silence would leave cancelled cards visible.
		results = append(results, BattleResult{Command: resultCardPlayPlan, Args: []int64{
			int64(memberType), 0, 0, 0, 0, 0, 0,
		}})
	}
	return encodeBattleResults(results)
}

func (engine *BattleEngine) playerCardDarkness(memberType int, cardType int) int {
	if memberType < 1 || memberType > len(engine.players) {
		return 0
	}
	player := &engine.players[memberType-1]
	slot := -1
	for index, deckSlot := range player.Hand {
		if deckSlot > 0 && deckSlot <= len(player.Deck) && player.Deck[deckSlot-1].CardType == cardType {
			slot = index
			break
		}
	}
	if slot < 0 {
		return 0
	}
	return playerDarknessMask(player) >> (4 - slot) & 1
}

func playerDarknessMask(player *battlePlayer) int {
	mask := 0
	for _, effect := range player.Effects {
		if strings.HasPrefix(effect.Function, "DARKNESS") && effect.Remaining > 0 {
			mask |= effect.Mask
		}
	}
	return mask
}
