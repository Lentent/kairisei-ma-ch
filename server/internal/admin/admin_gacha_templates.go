package admin

import (
	"fmt"
	"slices"
	"strings"

	"kairisei.local/server/internal/gamestate"
)

// Templates use built-in rules, never another operator's mutable live document.
// Version 1 keeps promotions as well as draw rules; legacy copies keep their old semantics.
type gachaRuleTemplate struct {
	ID                int      `json:"id"`
	Name              string   `json:"name"`
	SourceName        string   `json:"source_name"`
	GachaIDs          []int    `json:"gacha_ids"`
	Summary           []string `json:"summary"`
	ProfessionChoices bool     `json:"profession_choices"`
}

// The native picker groups only by card_num/card_num_max and then takes the
// first row for an Arthur type. Duplicate payment alternatives would be ambiguous.
func (o *Operations) professionChoicesAllowed(members []int) bool {
	seen := map[[2]int]bool{}
	for _, id := range members {
		p := o.gachaBases[id]
		key := [2]int{p.CardNum, p.CardNumMax}
		if p.GachaType != 0 || p.ArthurType != 0 || p.UserSelectMax != 0 || p.CategoryNum == 10000 || p.CardNum != p.CardNumMax || p.DailyFirstFree || p.UnownedOnly || len(p.RewardPool) != 0 || seen[key] {
			return false
		}
		seen[key] = true
	}
	return len(members) > 0
}

func (o *Operations) filterGachaProfession(p gamestate.GachaProfile) gamestate.GachaProfile {
	if p.ArthurType < 1 || p.ArthurType > 4 {
		return p
	}
	ids, weights := []int{}, []int{}
	fames := map[int]int{}
	for i, id := range p.CardIDs {
		job, known := o.gachaCardJobs[id]
		if known && (job == 0 || job == p.ArthurType) {
			ids, weights = append(ids, id), append(weights, p.CardWeights[i])
			if fame := p.CardFames[id]; fame > 1 {
				fames[id] = fame
			}
		}
	}
	p.CardIDs, p.CardWeights, p.CardFames = ids, weights, fames
	return p
}

func ruleTemplateAllowed(base gamestate.GachaProfile) bool {
	// Custom self-selection also needs persistent selection restoration. No current
	// non-tutorial preset uses it; do not offer an incomplete contract in this picker.
	return base.GroupID > 0 && base.PublicationKey != "operator" && base.UserSelectMax == 0 &&
		base.CardNum > 0 && base.CardNumMax <= 100 && gamestate.ValidateGachaRules(base) == nil
}

func (o *Operations) ruleTemplateMembers(groupID int) []int {
	members := o.gachaGroupMembers(groupID)
	for _, id := range members {
		if _, custom := o.customGachas[id]; custom || !ruleTemplateAllowed(o.gachaBases[id]) {
			return nil
		}
	}
	return members
}

// The caller holds configMu. Descriptions are generated from the same profiles
// used by creation so the picker cannot advertise a different set of rules.
func (admin *API) gachaRuleTemplates() []gachaRuleTemplate {
	o := admin.operations
	groups := []int{}
	for group := range o.managedGachaGroups {
		groups = append(groups, group)
	}
	slices.Sort(groups)
	result := []gachaRuleTemplate{}
	for _, group := range groups {
		members := o.ruleTemplateMembers(group)
		if len(members) == 0 {
			continue
		}
		lead := o.gachaBases[members[0]]
		kind := "普通卡牌"
		switch {
		case len(lead.Steps) > 0:
			kind = fmt.Sprintf("%d阶段扭蛋", len(lead.Steps))
		case len(lead.RewardPool) > 0:
			kind = "混合奖励"
		case lead.UnownedOnly:
			kind = "未入手限定"
		case lead.DailyFirstFree:
			kind = "友情点·每日首抽免费"
		}
		template := gachaRuleTemplate{ID: group, SourceName: lead.Name, GachaIDs: members, Summary: []string{}, ProfessionChoices: o.professionChoicesAllowed(members)}
		draws := []string{}
		for i, id := range members {
			p := o.gachaBases[id]
			draw := fmt.Sprintf("%d抽", p.CardNum)
			if p.CardNumMax > p.CardNum {
				draw = fmt.Sprintf("%d–%d抽", p.CardNum, p.CardNumMax)
			}
			if !slices.Contains(draws, draw) {
				draws = append(draws, draw)
			}
			payment := map[int]string{2: "友情点", 3: "水晶", 6: "付费水晶"}[p.PayType]
			if p.PayType == 4 {
				payment = admin.catalogByKey[adminCatalogKey(8, p.PayTypeID)].Name
				if payment == "" {
					payment = fmt.Sprintf("道具 %d", p.PayTypeID)
				}
			}
			line := fmt.Sprintf("抽法%d：%s；默认消耗 %s × %d", i+1, draw, payment, p.Price)
			if p.PayType == 2 && p.CardNumMax > p.CardNum {
				line += "／张，按余额决定抽数"
			}
			if p.DailyFirstFree {
				line += "；每日首次免费1抽（改为其他消耗方式后取消）"
			}
			if p.UnownedOnly {
				line += "；只抽未获得同系的六星初始卡，全部获得后隐藏"
			}
			if p.GuaranteedCount > 0 {
				line += fmt.Sprintf("；其中%d张为%d星，其余%d张为%d星", p.GuaranteedCount, p.GuaranteedRarityRank, p.CardNum-p.GuaranteedCount, p.RemainderRarityRank)
			}
			template.Summary = append(template.Summary, line)
			if len(p.Steps) > 0 {
				prices := []string{}
				for _, step := range p.Steps {
					prices = append(prices, fmt.Sprint(step.Price))
				}
				template.Summary = append(template.Summary, "阶段费用："+strings.Join(prices, " → ")+"；成功抽取后推进，最后阶段重复")
			}
			for _, gift := range p.Gifts {
				span := fmt.Sprintf("第%d次起", gift.FromPlay)
				if gift.ToPlay > 0 {
					span = fmt.Sprintf("第%d–%d次", gift.FromPlay, gift.ToPlay)
				}
				if gift.ToPlay == gift.FromPlay {
					span = fmt.Sprintf("第%d次", gift.FromPlay)
				}
				rewards := []string{}
				for _, reward := range gift.Rewards {
					name := admin.catalogByKey[adminCatalogKey(reward.Type, reward.RewardTypeID)].Name
					if name == "" {
						name = fmt.Sprintf("奖励 %d:%d", reward.Type, reward.RewardTypeID)
					}
					rewards = append(rewards, fmt.Sprintf("%s × %d", name, reward.Num))
				}
				template.Summary = append(template.Summary, fmt.Sprintf("抽法%d %s成功抽取附赠：%s", i+1, span, strings.Join(rewards, "、")))
			}
		}
		template.Name = kind + " · " + strings.Join(draws, "／")
		if len(lead.RewardPool) > 0 {
			template.Summary = append(template.Summary, "可编辑封面、排期、消耗、价格与奖励权重；奖励身份、数量、阶段数量及附赠条件固定")
		} else {
			template.Summary = append(template.Summary, "可编辑卡牌、概率、名声、封面、排期、消耗与价格；抽数和特殊限制按模板保留")
		}
		result = append(result, template)
	}
	return result
}
