package multiplayer

import (
	"errors"
	"fmt"
	"strings"
)

// executeEnemyPassiveSkill mirrors the one-shot passive traversal in
// battle5_api_start and battle5_api_pvp_start. The native member owns a full
// SkillData copy and executes it after every member's initial parameter rows,
// before the first turn phase. Passive durable effects use buff-list type 1;
// they are not ordinary enemy actions and consume no action budget.
func (engine *BattleEngine) executeEnemyPassiveSkill(actor *battleEnemy) ([]BattleResult, error) {
	if actor == nil || actor.Level.PassiveSkillID == 0 {
		return nil, nil
	}
	skillID := actor.Level.PassiveSkillID
	skill, roles, matched := engine.selectEnemySkillBranch(actor, skillID, actor.MemberType)
	if !matched || len(roles) == 0 {
		return nil, fmt.Errorf("enemy passive skill %d is incomplete", skillID)
	}
	skillResult, err := enemySkillResult(actor.MemberType, skillID, actor.MemberType, skill, true)
	if err != nil {
		return nil, err
	}
	results := []BattleResult{skillResult}
	for _, role := range roles {
		role.SourceSkillID = skill.ID
		var (
			roleResults []BattleResult
			err         error
		)
		switch role.Function {
		case "ATTR_HIDE", "ENDURE":
			roleResults, err = engine.executeEnemyPersistentEffectWithListType(actor, actor.MemberType, role, 1)
		case "REWRITE":
			roleResults = engine.executeEnemyRewriteWithListType(actor, actor.MemberType, role, 1)
		case "ENEMY_AI_TRIGGER_FLAG_SET":
			roleResults, err = engine.executeEnemyRole(actor, actor.MemberType, role, roles)
		default:
			return nil, fmt.Errorf("enemy passive skill %d function %q is unsupported", skillID, role.Function)
		}
		if err != nil {
			return nil, err
		}
		for _, row := range roleResults {
			// 8d7e0 list1 emits a base-parameter query while registering the
			// passive; 7adb0 drains its 69/6 afterwards (D-330 original API).
			if row.Command == resultPassiveBuff && len(row.Args) >= 3 && row.Args[2] == 1 {
				member := int(row.Args[0])
				if member >= 1 && member <= 4 {
					results = append(results, playerBaseParameterResult(&engine.players[member-1]))
				} else if member >= 5 && member < 5+engine.enemyCount {
					enemy := engine.enemies[member-5]
					ensureEnemyBaseParameters(&enemy)
					results = append(results, BattleResult{Command: resultBaseParam, Args: baseParameterArgs(member,
						enemy.BaseMaxHP, enemy.BaseAttack, enemy.BaseMagic, enemy.BaseRecovery, enemy.BaseDefense, enemy.BaseMDefense)})
				}
			}
			results = append(results, row)
		}
	}
	engine.nativeSkillSerial++
	results = engine.projectSkillStatusResults(results)
	return append(results, engine.finishTranceReactions(skill.Cost)...), nil
}

func validateEnemyPassiveContracts(catalog *CombatCatalog) error {
	if catalog == nil {
		return errors.New("enemy passive catalog is unavailable")
	}
	references := 0
	for levelID, level := range catalog.EnemyLevels {
		if level.PassiveSkillID == 0 {
			continue
		}
		references++
		variants := catalog.EnemySkills[level.PassiveSkillID]
		if len(variants) == 0 {
			return fmt.Errorf("official enemy level %d references missing passive skill %d", levelID, level.PassiveSkillID)
		}
		fallback := false
		for _, variant := range variants {
			if _, ok := combatSkillTargetCode(variant.Target); !ok {
				return fmt.Errorf("official enemy passive skill %d has unsupported SKILL_TARGET %q", level.PassiveSkillID, variant.Target)
			}
			if strings.TrimSpace(variant.BranchCondition) == "" && strings.TrimSpace(variant.BranchCondition2) == "" {
				fallback = true
			}
			if !enemyBranchConditionSupported(variant.BranchCondition) || !enemyBranchConditionSupported(variant.BranchCondition2) {
				return fmt.Errorf("official enemy passive skill %d has unsupported branch condition %q/%q", level.PassiveSkillID, variant.BranchCondition, variant.BranchCondition2)
			}
			roles := catalog.EnemySkillRoles[variant.FunctionID]
			if len(roles) == 0 {
				return fmt.Errorf("official enemy passive skill %d function %d has no roles", level.PassiveSkillID, variant.FunctionID)
			}
			for _, role := range roles {
				switch role.Function {
				case "ATTR_HIDE", "ENDURE", "REWRITE", "ENEMY_AI_TRIGGER_FLAG_SET":
				default:
					return fmt.Errorf("official enemy passive skill %d function %d uses unsupported role %q", level.PassiveSkillID, variant.FunctionID, role.Function)
				}
			}
		}
		if !fallback {
			return fmt.Errorf("official enemy passive skill %d has no unconditional fallback", level.PassiveSkillID)
		}
	}
	if references == 0 {
		return errors.New("official enemy levels contain no passive-skill references")
	}
	return nil
}

func validateEnemyPassiveSimulation(catalog *CombatCatalog) error {
	engine := &BattleEngine{catalog: catalog, phase: battlePhaseCreated, enemyCount: 1, rng: newXorShift128(1)}
	for index := range engine.players {
		engine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 100, MaxHP: 100}
	}
	engine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL",
		MemberType: 5, HP: 1000, MaxHP: 1000,
		Level: CombatEnemyLevel{PassiveSkillID: 36001401},
	}
	results, err := engine.Start()
	if err != nil {
		return err
	}
	if len(results) < 4 {
		return errors.New("enemy passive start emitted no result rows")
	}
	skillResult := results[len(results)-4]
	baseResult := results[len(results)-3]
	buffResult := results[len(results)-2]
	paramResult := results[len(results)-1]
	if skillResult.Command != resultSkill || len(skillResult.Args) != 7 || skillResult.Args[0] != 5 || skillResult.Args[1] != 36001401 ||
		skillResult.Args[2] != 5 || skillResult.Args[3] != 1 || skillResult.Args[4] != 0 || skillResult.Args[5] != 36001401 || skillResult.Args[6] != 1 {
		return fmt.Errorf("enemy passive skill projection is %+v", skillResult)
	}
	if baseResult.Command != 5 || !equalBattleArgs(baseResult.Args, []int64{5, 1000, 0, 0, 0, 0, 0, 99999, 99999, 99999}) {
		return fmt.Errorf("enemy passive base parameters are %+v", baseResult)
	}
	if buffResult.Command != resultPassiveBuff || len(buffResult.Args) != 11 || buffResult.Args[0] != 5 || buffResult.Args[2] != 1 || buffResult.Args[3] != int64(battleBuffCodes["ENDURE"]) {
		return fmt.Errorf("enemy passive buff projection is %+v", buffResult)
	}
	if paramResult.Command != resultBattleParam || !equalBattleArgs(paramResult.Args, battleParameterArgs(5, 1000, 1000, 0, 0, 0, 0, 0, 99999, 99999, 99999)) {
		return fmt.Errorf("enemy passive parameter projection is %+v", paramResult)
	}
	if len(engine.enemies[0].Effects) != 1 || engine.enemies[0].Effects[0].Function != "ENDURE" || engine.enemies[0].Effects[0].ListType != 1 || engine.enemies[0].Effects[0].Value != 1 || engine.enemies[0].Effects[0].Remaining != 99 || engine.enemies[0].Effects[0].SourceSkillID != 36001401 {
		return fmt.Errorf("enemy passive durable state is %+v", engine.enemies[0].Effects)
	}
	flagEngine := &BattleEngine{catalog: catalog, enemyCount: 1, rng: newXorShift128(1)}
	flagEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 1000, MaxHP: 1000, Level: CombatEnemyLevel{PassiveSkillID: 35001327}}
	flagResults, err := flagEngine.executeEnemyPassiveSkill(&flagEngine.enemies[0])
	if err != nil {
		return err
	}
	if len(flagResults) != 1 || flagResults[0].Command != resultSkill || len(flagResults[0].Args) != 7 || flagResults[0].Args[4] != 3 ||
		!flagEngine.enemies[0].hasAIFlag(1) || !flagEngine.enemies[0].hasAIFlag(2) {
		return fmt.Errorf("enemy passive AI flags are results=%+v flags=%b", flagResults, flagEngine.enemies[0].AIFlags)
	}
	return nil
}
