package admin

import (
	"errors"
	"strings"

	"kairisei.local/server/internal/game"
)

func validateMissionPolicy(missions []game.MissionDefinition) error {
	// nil is the legacy document format; loading upgrades it to defaults.
	if missions == nil {
		return nil
	}
	defaults := game.DefaultMissions()
	if len(missions) != len(defaults) {
		return errors.New("请保留全部9项任务，使用启用开关控制是否开放")
	}
	byID := make(map[int]game.MissionDefinition, len(defaults))
	for _, def := range defaults {
		byID[def.ID] = def
	}
	for _, def := range missions {
		base, ok := byID[def.ID]
		if !ok || def.Kind != base.Kind || def.Daily != base.Daily {
			return errors.New("任务ID重复或任务类型发生变化，请重新加载配置")
		}
		delete(byID, def.ID)
		if strings.TrimSpace(def.Title) == "" || len([]rune(def.Title)) > 60 {
			return errors.New("任务标题须为1至60字")
		}
		if def.Target < 1 || def.Target > 10000000 || !validPolicyAmount(def.Crystals, false) {
			return errors.New("任务目标和水晶奖励须为1至10000000的整数")
		}
		if def.Daily && def.Kind == "login" && def.Target != 1 {
			return errors.New("每日登录的目标固定为1次")
		}
	}
	return nil
}
