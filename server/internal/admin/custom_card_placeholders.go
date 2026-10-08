package admin

// The native battle5_api_get_skill_info_param serializes five branches, each
// with five effects and ten display slots. These are computed display values,
// not the raw VALUE fields. Verified against the CN 6.0.2 ARM producers.
type customDescriptionSlot struct {
	Name       string `json:"name"`
	Formula    string `json:"formula"`
	Parameters []int  `json:"parameters"`
}

func customDescriptionSlots(code string) map[int]customDescriptionSlot {
	slots := map[int]customDescriptionSlot{}
	add := func(slot int, name, formula string, parameters ...int) {
		slots[slot] = customDescriptionSlot{name, formula, parameters}
	}
	duration := func() { add(0, "持续回合", "取持续回合参数。", 0) }
	switch code {
	case "ATTACK_AA":
		add(1, "固定伤害部分", "基础伤害＋基础伤害等级成长×等级÷1000；不包含参照能力的伤害。", 0, 1)
		add(2, "参照能力百分比", "（能力倍率＋能力倍率等级成长×等级）÷10，显示的是百分比数值。", 2, 3)
		add(3, "攻击次数", "取攻击次数参数。", 4)
		add(4, "基础暴击率百分比", "基础暴击率÷10，显示的是百分比数值。", 6)
	case "ATK_UP_FIXED", "DEF_UP_FIXED", "ATK_BREAK_FIXED", "GUARD_BREAK_FIXED", "PARAM_LIMIT_BREAK_FIXED":
		duration()
		add(1, "固定能力变化量", "（基础固定值＋固定值等级成长×等级）×固定值倍率÷1000。", 2, 3, 4)
		add(2, "额外等级加成", "额外等级加成×等级；这是独立展示值，不自动加进前一个占位符。", 5)
	case "ATK_UP_BY_SELF_PARAM", "DEF_UP_BY_SELF_PARAM", "ATK_BREAK_BY_SELF_PARAM", "GUARD_BREAK_BY_SELF_PARAM":
		duration()
		add(1, "参照能力百分比", "（基础能力倍率＋倍率等级成长×等级）÷10，显示的是百分比数值。", 3, 4)
		add(2, "额外固定变化量", "额外等级加成×等级。", 5)
		add(6, "参照能力上限", "取参照能力上限参数。", 6)
	case "HEAL_FIXED":
		add(1, "固定回复量", "基础回复量＋回复量等级成长×等级÷1000；不包含参照能力带来的回复。", 0, 1)
		add(2, "参照能力百分比", "（能力倍率＋倍率等级成长×等级）÷10，显示的是百分比数值。", 2, 3)
	case "REGENERATE_FIXED":
		duration()
		add(1, "每回合固定回复量", "基础回复量＋回复量等级成长×等级÷1000。", 1, 2)
		add(2, "参照能力百分比", "（能力倍率＋倍率等级成长×等级）÷10，显示的是百分比数值。", 3, 4)
	case "ATTR_DEF_UP", "ATTR_DEF_DOWN":
		add(1, "属性抗性百分比部分", "（基础比例＋比例等级成长×等级）÷10，显示的是百分比数值。", 1, 2)
		add(2, "属性抗性固定值部分", "基础固定值＋固定值等级成长×等级÷1000。", 3, 4)
	case "COVERING", "CRITICAL_UP", "WEAKNESS":
		duration()
		add(1, "效果百分比", "（基础比例＋比例等级成长×等级）÷10，显示的是百分比数值。", 1, 2)
	case "REFLECTION":
		duration()
		add(1, "反弹百分比", "（基础反弹倍率＋反弹倍率等级成长×等级）÷100；原参数使用万分比。", 1, 2)
	case "GUTS":
		duration()
		add(1, "复活次数", "取复活次数参数。", 1)
		add(2, "复活HP百分比", "取复活HP比例参数，原单位为百分比。", 2)
	case "ATK_OP_DRAIN", "ATK_OP_DRAIN_ALL", "ATK_OP_PIERCING", "ATK_OP_REVENGE":
		add(1, "效果百分比", "基础比例＋比例等级成长×等级，显示的是百分比数值。", 0, 1)
	case "ATK_OP_DAMAGE_INCREASE":
		add(1, "固定额外威力", "固定额外威力＋固定威力等级成长×等级÷1000。", 0, 1)
		add(2, "参照能力百分比", "（能力倍率＋倍率等级成长×等级）÷10，显示的是百分比数值。", 2, 3)
		add(3, "参照能力上限", "取参照能力上限参数。", 5)
	case "BURST_GAUGE_QUICK_UP":
		add(1, "圣剑槽介绍展示值", "客户端介绍展示＝基础值＋等级成长×等级；此处说明介绍的展示规则，实战结算的成长换算可能不同。", 0, 1)
	case "CARD_SEAL_REGIST":
		duration()
		add(1, "封印抗性百分比", "基础抗性＋抗性等级成长×等级，显示的是百分比数值。", 1, 2)
	case "DEAL_BONUS", "DEAL_PENALTY":
		add(0, "抽牌变化张数", "取抽牌张数参数；抽牌效果的展示数在本效果的第0个位置。", 0)
	case "ENCHANT":
		duration()
		add(1, "追加伤害固定值", "（基础追加伤害＋伤害等级成长×等级）×伤害倍率÷1000＋额外每级加值×等级。", 1, 2, 3, 4)
	case "TRANCE_GAUGE_VALUE_DOWN":
		add(1, "敌方槽变化百分比", "槽变化比例＋比例等级成长×等级。", 1, 2)
	case "POISON", "BURN", "FREEZE", "BLEED", "ELECTRIC":
		duration()
		add(1, "成功概率百分比", "基础成功概率＋概率等级成长×等级。", 1, 2)
		add(2, "每回合固定伤害", "固定伤害＋固定伤害等级成长×等级÷1000；不包含参照能力带来的伤害。", 3, 4)
		add(3, "参照能力百分比", "（能力倍率＋倍率等级成长×等级）÷10，显示的是百分比数值。", 5, 6)
	case "BLESS":
		// Native 54e60 serializes explicit description fields, independently of
		// the dynamically calculated CALL power at info+0x50.
		add(7, "祝福介绍展示值1", "展示值1基础值＋展示值1等级成长×等级；沿用模板提供的呼出技能介绍数据，不会自动随呼出技能参数同步。", 3, 4)
		add(8, "祝福介绍展示值2", "展示值2基础值＋展示值2等级成长×等级；沿用模板提供的呼出技能介绍数据。", 5, 6)
		add(9, "祝福介绍展示值3", "展示值3基础值＋展示值3等级成长×等级；沿用模板提供的呼出技能介绍数据。", 7, 8)
	}
	return slots
}
