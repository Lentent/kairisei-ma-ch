package admin

import (
	"errors"
	"fmt"
	"reflect"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

// Historical reward identities and gifts are fixed by the audited preset;
// operators can tune each step's price and weights, plus its publication window.
func ValidateMixedGachaConfig(catalog map[string]AdminCatalogEntry, base gamestate.GachaProfile, config AdminGachaConfig) (map[string]any, error) {
	if err := validateAdminGachaPayment(catalog, config); err != nil {
		return nil, err
	}
	if len(config.CardIDs) != 0 || len(config.Weights) != 0 || len(config.Steps) != len(base.Steps) {
		return nil, errors.New("混合池须保留预设奖励与阶段数量")
	}
	configured := adminConfiguredGacha(base, config).Profile
	if err := gamestate.ValidateGachaRules(configured); err != nil {
		return nil, err
	}
	canonical := [][]gamestate.WeightedReward{base.RewardPool}
	edited := [][]gamestate.WeightedReward{config.RewardPool}
	for i, step := range config.Steps {
		if step.Price < 1 || step.Price > 10000000 {
			return nil, errors.New("阶段价格须为1–10000000")
		}
		canonical = append(canonical, base.Steps[i].RewardPool)
		edited = append(edited, step.RewardPool)
	}
	if len(config.Steps) > 0 && (config.Price != config.Steps[0].Price || !reflect.DeepEqual(config.RewardPool, config.Steps[0].RewardPool)) {
		return nil, errors.New("首阶段价格和奖励须与首页一致")
	}
	names := map[string]string{}
	blocked := []int{}
	for i, pool := range edited {
		if len(pool) != len(canonical[i]) {
			return nil, errors.New("混合池须保留预设奖励身份，调整每项权重即可")
		}
		for j, entry := range pool {
			if !reflect.DeepEqual(entry.Reward, canonical[i][j].Reward) || entry.Weight < 1 || entry.Weight > 1000000 {
				return nil, errors.New("只能调整预设奖励权重（1–1000000），不能替换身份或数量")
			}
			key := adminCatalogKey(entry.Reward.Type, entry.Reward.RewardTypeID)
			item, ok := catalog[key]
			if !ok {
				return nil, fmt.Errorf("混合奖励不存在：%s", key)
			}
			names[key] = item.Name
			if item.ResourceState == "unavailable" {
				blocked = append(blocked, entry.Reward.RewardTypeID)
			}
		}
	}
	stages := []game.GachaOddsStage{}
	if len(config.Steps) == 0 {
		var err error
		stages, err = game.PreviewGachaStages(configured, nil)
		if err != nil {
			return nil, err
		}
	} else {
		for i := range config.Steps {
			configured.PlayCount = i
			rows, err := game.PreviewGachaStages(configured, nil)
			if err != nil {
				return nil, err
			}
			rows[0].Name = fmt.Sprintf("第%d阶段 · 费用%d", i+1, config.Steps[i].Price)
			stages = append(stages, rows...)
		}
	}
	return map[string]any{"config": config, "base": adminConfiguredGacha(base, config).Profile, "odds_scaled": []int{}, "odds_scale": 100000, "rarities": map[int]int{}, "blocked_card_ids": blocked, "publishable": len(blocked) == 0, "warnings": []string{"混合池奖励与赠礼按预设保留；各阶段价格和权重为本地运营配置。最后阶段重复，赠礼仍按累计抽取次数限制。"}, "stages": stages, "reward_names": names}, nil
}
