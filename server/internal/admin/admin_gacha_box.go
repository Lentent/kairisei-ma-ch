package admin

import (
	"fmt"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

func (a *API) validateCustomBoxGacha(base gamestate.GachaProfile, config AdminGachaConfig) (map[string]any, error) {
	profile := adminConfiguredGacha(base, config).Profile
	if len(config.Weights) > 0 || len(config.CardFames) > 0 || len(config.Steps) > 0 || len(config.RewardPool) > 0 {
		return nil, fmt.Errorf("箱池只使用各轮奖励和份数")
	}
	if err := gamestate.ValidateGachaRules(profile); err != nil {
		return nil, err
	}
	names := map[string]string{}
	stages := []game.GachaOddsStage{}
	for i, round := range profile.BoxRounds {
		total := gamestate.GachaBoxStock(round.Rewards)
		stage := game.GachaOddsStage{Name: fmt.Sprintf("第 %d 轮 · %d 份", i+1, total), DrawCount: profile.CardNum, Rewards: []gamestate.Reward{}}
		if i == 10 {
			stage.Name = fmt.Sprintf("第 11 轮起 · 无限重复 · %d 份", total)
		}
		for _, entry := range round.Rewards {
			if err := a.validateCustomGachaReward(entry.Reward); err != nil {
				return nil, fmt.Errorf("第 %d 套：%w", i+1, err)
			}
			key := adminCatalogKey(entry.Reward.Type, entry.Reward.RewardTypeID)
			names[key] = a.catalogByKey[key].Name
			stage.Rewards = append(stage.Rewards, entry.Reward)
			stage.Stocks = append(stage.Stocks, entry.Stock)
		}
		var err error
		stage.Odds, err = game.PreviewGachaWeights(stage.Stocks)
		if err != nil {
			return nil, err
		}
		stages = append(stages, stage)
	}
	return map[string]any{"config": config, "base": profile, "odds_scaled": []int{}, "odds_scale": 100000, "rarities": map[int]int{}, "blocked_card_ids": []int{}, "publishable": true, "warnings": []string{fmt.Sprintf("本抽法连续抽 %d 份，只扣一次所配置费用；单抽与连抽共用库存及轮次，抽空自动接下一轮。前 10 轮独立，第 11 轮起无限重复。", profile.CardNum), "预览占比表示新一轮第一次抽取的概率；抽中扣一份，后续概率随剩余库存变化。", "重新发布时保留玩家正在抽的箱子及剩余份数，新奖励模板从下一箱生效；费用和排期即时使用新配置。"}, "stages": stages, "reward_names": names}, nil
}
