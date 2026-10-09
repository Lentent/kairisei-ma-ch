package admin

import (
	"fmt"
	"strings"
)

// These labels follow the CN role consumers in battle_engine_math.go,
// battle_engine_player_actions.go and battle_engine_player_effects.go. The
// parameter-rule CSV describes types only; VALUE does not imply a percentage.
type customEffectParameterHelp struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Unit        string `json:"unit,omitempty"`
}

type customEffectHelp struct {
	Name             string                        `json:"name"`
	Description      string                        `json:"description"`
	Formula          string                        `json:"formula,omitempty"`
	Parameters       []customEffectParameterHelp   `json:"parameters"`
	DescriptionSlots map[int]customDescriptionSlot `json:"description_slots,omitempty"`
}

func customCardEffectHelp(rules map[string][]string) map[string]customEffectHelp {
	help := map[string]customEffectHelp{}
	p := func(name, description, unit string) customEffectParameterHelp {
		return customEffectParameterHelp{name, description, unit}
	}
	duration := p("持续回合", "效果持续的回合数，具体生效与结束时点沿用模板。", "turn")
	stat := p("影响的能力", "ATK物理攻击、INT魔法攻击、MND回复量、DEF物防、MDEF魔防、MAX_HP最大HP。", "")
	source := p("参照的自身能力", "计算时读取施放者的这项能力。HP为当前HP，MAX_HP为最大HP。", "")
	chance := p("成功概率", "100表示100%；目标抗性和状态可能使效果失败。", "percent")
	chanceGrowth := p("成功概率等级成长", "最终概率＝基础概率＋此值×卡牌等级。", "percent")
	unused := p("模板保留字段", "当前服务端的此效果未使用该字段，建议保留模板值。", "")
	add := func(code, name, description, formula string, params ...customEffectParameterHelp) {
		help[code] = customEffectHelp{Name: name, Description: description, Formula: formula, Parameters: params}
	}
	add("ATTACK_AA", "直接攻击", "按属性及物理／魔法类型攻击目标，可设置攻击次数。实际伤害还受防御、属性克制、暴击和Chain影响。",
		"基础威力＝基础伤害＋伤害成长×等级÷1000＋参照能力×（能力倍率＋倍率成长×等级）÷1000。",
		p("基础伤害", "不依赖自身能力的固定伤害部分。", "point"), p("基础伤害等级成长", "此值×等级÷1000后加到基础伤害；1000表示每级加1。", "scaled_growth"),
		p("能力倍率", "1000表示参照能力的100%，1300表示130%。", "permille"), p("能力倍率等级成长", "每级增加的能力倍率，单位千分比。", "permille"), p("攻击次数", "每次攻击分别结算。", "count"), source,
		p("基础暴击率", "150表示15%，1000表示100%，并非暴击伤害倍率。", "permille"), p("攻击属性", "火、冰、风、光、暗；使用与来源分支相同的属性时，会随自制卡属性转换。", ""), p("伤害类型", "PHYSICS物理、MAGIC魔法。", ""))
	fixed := []customEffectParameterHelp{duration, stat, p("固定值倍率", "对下一项基础值与成长的总和乘以此值÷1000；1表示0.1%，1000表示100%。", "permille"), p("基础能力值", "先与等级成长相加，再乘固定值倍率。", "point"), p("能力值等级成长", "每级增加到基础能力值的数值，然后一起乘倍率。", "point"), p("额外每级加值", "此值×等级单独加到结果，不乘固定值倍率。", "point")}
	for _, entry := range []struct{ code, name, desc string }{
		{"ATK_UP_FIXED", "攻击能力提升", "提高目标的指定攻击或回复能力。"}, {"DEF_UP_FIXED", "防御能力提升", "提高目标的指定防御能力。"},
		{"ATK_BREAK_FIXED", "攻击能力降低", "降低目标的指定攻击或回复能力。"}, {"GUARD_BREAK_FIXED", "防御能力降低", "降低目标的物防或魔防。"},
		{"PARAM_LIMIT_BREAK_FIXED", "能力上限提升", "提高指定能力的战斗上限；本身不等于增加当前能力值。"},
	} {
		add(entry.code, entry.name, entry.desc, "变化量＝（基础能力值＋能力值成长×等级）×固定值倍率÷1000＋额外每级加值×等级。", fixed...)
	}
	scaled := []customEffectParameterHelp{duration, stat, source, p("自身能力倍率", "按参照能力的千分比计算；1000表示100%。", "permille"), p("倍率等级成长", "每级增加的千分比倍率。", "permille"), p("额外每级加值", "此值×等级加到最终变化量。", "point"), p("参照能力上限", "大于0时限制参与计算的自身能力；0或空表示不限。", "point")}
	for _, entry := range []struct{ code, name, desc string }{
		{"ATK_UP_BY_SELF_PARAM", "按自身能力提升攻击", "读取施放者的能力，提升目标指定能力。"}, {"DEF_UP_BY_SELF_PARAM", "按自身能力提升防御", "读取施放者的能力，提升目标防御。"},
		{"ATK_BREAK_BY_SELF_PARAM", "按自身能力降低攻击", "读取施放者的能力，降低目标指定能力。"},
	} {
		add(entry.code, entry.name, entry.desc, "变化量＝参照能力×（倍率＋倍率成长×等级）÷1000＋额外每级加值×等级；正数参照上限先限制参照能力。", scaled...)
	}
	heal := []customEffectParameterHelp{p("基础回复量", "固定回复HP部分。", "point"), p("回复量等级成长", "此值×等级÷1000加入基础回复量；1000表示每级加1。", "scaled_growth"), p("能力回复倍率", "1000表示参照能力的100%。", "permille"), p("回复倍率等级成长", "每级增加的千分比倍率。", "permille"), source}
	add("HEAL_FIXED", "立即回复HP", "为目标回复HP。实际回复受回复效果、Chain和目标剩余HP等影响。", "回复量＝基础回复量＋回复成长×等级÷1000＋参照能力×（回复倍率＋倍率成长×等级）÷1000。", heal...)
	add("HEAL_BY_SELF_PARAM", "按自身能力回复HP", "读取施放者指定能力计算回复量。", "回复量＝参照能力×（倍率＋倍率成长×等级）÷1000＋额外每级回复×等级。", source, p("能力回复倍率", "1000表示100%。", "permille"), p("回复倍率等级成长", "每级增加的千分比倍率。", "permille"), p("额外每级回复", "每级单独增加的回复量。", "point"), p("参照能力上限", "正数限制参照能力，0表示不限。", "point"))
	regen := append([]customEffectParameterHelp{duration}, heal...)
	add("REGENERATE_FIXED", "持续回复HP", "在后续回合按持续回复效果恢复HP，持续回合数由第一项决定。", "每次回复量的计算与立即回复相同。", regen...)
	for _, entry := range []struct{ code, name, desc string }{
		{"ATTR_DEF_UP", "属性防御提升", "减少来自指定属性的伤害；含比例与固定值两部分。"}, {"ATTR_DEF_DOWN", "属性防御降低", "使目标受到指定属性的伤害增加；含比例与固定值两部分。"},
	} {
		add(entry.code, entry.name, entry.desc, "比例部分＝基础比例＋比例成长×等级；固定部分＝固定值＋固定成长×等级÷1000。", duration, p("属性伤害调整比例", "千分比，100表示10%。", "permille"), p("比例等级成长", "每级增加的千分比数值。", "permille"), p("固定伤害调整值", "在伤害结算中增加或减少的固定数值。", "point"), p("固定值等级成长", "此值×等级÷1000加入固定值。", "scaled_growth"), p("作用属性", "只影响这个属性的伤害。", ""))
	}
	add("ENCHANT", "属性追加伤害", "攻击命中后追加指定属性的伤害，与普通攻击伤害分别结算。", "每次追加威力＝基础倍率×（基础值＋基础值成长×等级）÷1000＋额外每级加值×等级。", duration, p("基础倍率", "1000表示100%。", "permille"), p("追加伤害基础值", "与下一项成长共同参与计算。", "point"), p("基础值等级成长", "每级增加的基础值。", "point"), p("额外每级加值", "每级独立增加的追加伤害。", "point"), p("追加伤害属性", "追加伤害使用的属性。", ""))
	for _, entry := range []struct{ code, name, desc, unit, parameter string }{
		{"CRITICAL_UP", "暴击率提升", "提高暴击发生概率；并非提高暴击伤害。", "permille", "暴击率增加值"}, {"WEAKNESS", "弱点标记", "使目标受到的普通攻击伤害增加。", "permille", "受到伤害增加比例"},
		{"CARD_SEAL_REGIST", "封印抗性", "提高卡牌抵抗封印的概率。", "percent", "封印抵抗概率"},
	} {
		add(entry.code, entry.name, entry.desc, "最终数值＝基础数值＋等级成长×等级。", duration, p(entry.parameter, "千分比1000表示100%；百分比100表示100%，以此项单位为准。", entry.unit), p("数值等级成长", "每级增加的数值，单位与基础数值一致。", entry.unit))
	}
	add("DARKNESS_REGIST", "黑暗抗性", "提高抵抗手牌黑暗状态的概率。", "", duration, p("抵抗概率", "100表示100%。", "percent"), unused)
	add("CRITICAL_DAMAGE_BOOST", "暴击伤害提升", "增加暴击时的伤害倍率，基础暴击倍率为150%。", "例如填50，暴击倍率从150%变为200%。", duration, p("暴击倍率增加值", "以百分点增加，50表示额外50个百分点。", "percent"))
	add("COVERING", "替队友承受伤害", "代替队友承受符合条件的攻击，并按减伤率减少伤害。", "减伤率＝基础减伤率＋减伤成长×等级。", duration, p("代受伤害减伤率", "千分比，200表示20%减伤。", "permille"), p("减伤率等级成长", "每级增加的千分比减伤率。", "permille"), p("属性过滤", "NULL或不限表示不限制攻击属性。", ""), p("伤害类型过滤", "ALL所有、PHYSICS物理、MAGIC魔法。", ""))
	add("REFLECTION", "伤害反弹", "受击后向攻击者反弹伤害，本身不抵消此次受到的伤害。", "反弹倍率＝基础倍率＋倍率成长×等级。", duration, p("基础反弹倍率", "单位万分比，10000表示100%，770表示7.7%。", "permyriad"), p("反弹倍率等级成长", "每级增加的万分比倍率。", "permyriad"), p("伤害类型过滤", "ALL所有、PHYSICS物理、MAGIC魔法。", ""))
	for _, code := range []string{"ATTACK_BARRIER", "ATTACK_BARRIER_APPOINT_ATTR"} {
		params := []customEffectParameterHelp{duration, p("单次吸收伤害上限", "可吸收伤害的门槛，与可使用次数分别设置。", "point"), p("吸收上限等级成长", "每级增加的吸收上限。", "point"), p("可使用次数", "每次成功抵挡消耗一次。", "count"), p("可抵挡伤害类型", "ALL所有、PHYSICS物理、MAGIC魔法。", "")}
		if code == "ATTACK_BARRIER_APPOINT_ATTR" {
			params = append(params, p("可抵挡属性", "只抵挡指定属性的攻击。", ""))
		}
		add(code, "攻击护盾", "在持续时间及次数内抵挡符合条件的攻击。", "吸收上限＝基础上限＋上限成长×等级。", params...)
	}
	add("GUTS", "致死后恢复HP", "受到致死伤害时消耗一次机会并恢复HP，次数与持续回合分别设置。", "", duration, p("可触发次数", "每次致死恢复消耗一次。", "count"), p("恢复最大HP比例", "百分比，50表示恢复到最大HP的50%。", "percent"))
	add("STAN", "眩晕", "使目标无法正常行动；命中还受目标抗性和近期眩晕抗性影响。", "成功概率＝基础概率＋概率成长×等级。", duration, chance, chanceGrowth, unused, unused)
	add("COST_BLOCK", "封锁COST", "减少目标后续可用COST。", "", duration, p("封锁COST数量", "例如2表示封锁2点COST。", "point"))
	add("HP_CUT", "削减当前HP", "按比例减少目标当前HP，使用此效果不会将HP减到0。", "", p("当前HP削减比例", "百分比，50表示削减当前HP的50%。", "percent"), unused)
	for _, entry := range []struct{ code, name, desc string }{
		{"POISON", "中毒", "后续回合受到暗属性持续伤害。"}, {"BURN", "燃烧", "后续回合受到火属性持续伤害。"},
		{"FREEZE", "冰冻", "后续回合受到冰属性持续伤害；这里是持续伤害，不是眩晕。"}, {"BLEED", "裂伤", "后续回合受到风属性持续伤害。"}, {"ELECTRIC", "感电", "后续回合受到光属性持续伤害。"},
	} {
		add(entry.code, entry.name, entry.desc, "持续伤害基础值＝固定伤害＋固定成长×等级÷1000＋自身参照能力×（能力倍率＋倍率成长×等级）÷1000；还受目标属性及持续伤害抗性影响。", duration, chance, chanceGrowth, p("固定持续伤害", "每次持续伤害的固定部分。", "point"), p("固定伤害等级成长", "此值×等级÷1000加入固定伤害。", "scaled_growth"), p("能力伤害倍率", "参照能力的千分比，1000表示100%。", "permille"), p("能力倍率等级成长", "每级增加的千分比倍率。", "permille"), source)
	}
	add("DOT_VALUE_UP", "强化已有持续伤害", "强化目标身上指定种类的已有持续伤害，同时可延长持续回合；目标没有该状态时不会凭空施加。", "伤害新值＝原伤害×（100＋增幅）÷100＋固定增加量。", p("额外持续回合", "加到已有持续伤害的剩余回合数。", "turn"), p("伤害增幅", "百分比，20表示原伤害增加20%。", "percent"), p("增幅等级成长", "每级增加的百分比增幅。", "percent"), p("固定伤害增加量", "比例提高后再增加的固定伤害。", "point"), p("固定增加量等级成长", "此值×等级÷1000加入固定增加量。", "scaled_growth"), p("持续伤害种类", "POISON中毒、BURN燃烧、FREEZE冰冻、BLEED裂伤、ELECTRIC感电。", ""))
	for _, entry := range []struct{ code, name, desc string }{
		{"ATK_OP_DRAIN", "攻击吸血", "本技能的攻击造成伤害后，按比例为自身恢复HP。"}, {"ATK_OP_DRAIN_ALL", "攻击后全体回复", "本技能攻击造成伤害后，按比例为己方全体恢复HP。"},
	} {
		add(entry.code, entry.name, entry.desc, "吸血比例＝基础比例＋比例成长×等级。", p("伤害转回复比例", "百分比，50表示伤害的50%。", "percent"), p("比例等级成长", "每级增加的百分比比例。", "percent"), p("回复上限", "正数限制回复数值，0或空表示不限。", "point"))
	}
	add("ATK_OP_PIERCING", "攻击无视部分防御", "本技能攻击时忽略目标一定比例的正数防御；不是持续防御降低。", "无视比例＝基础比例＋等级成长×等级。", p("无视防御比例", "百分比，100表示忽略100%的正数防御。", "percent"), p("比例等级成长", "每级增加的百分比。", "percent"))
	add("ATK_OP_REVENGE", "累计受伤转攻击威力", "按施放者累计受到的伤害，为本技能攻击增加威力。", "额外威力受参照能力与上限比例约束。", p("受伤转威力比例", "百分比，40表示累计受伤的40%。", "percent"), p("比例等级成长", "每级增加的百分比。", "percent"), source, p("参照能力上限比例", "限制额外威力，百分比，例如参照MAX_HP且填50表示最多最大HP的50%。", "percent"))
	add("ATK_OP_DAMAGE_INCREASE", "按自身能力增加攻击威力", "为本技能直接攻击增加固定部分及按自身能力计算的额外威力。", "额外威力＝固定值＋固定成长×等级÷1000＋参照能力×（倍率＋倍率成长×等级）÷1000。", p("固定额外威力", "不依赖参照能力的部分。", "point"), p("固定威力等级成长", "此值×等级÷1000加入固定部分。", "scaled_growth"), p("能力倍率", "1000表示参照能力的100%。", "permille"), p("倍率等级成长", "每级增加的千分比。", "permille"), source, p("参照能力上限", "正数限制参照能力，0表示不限。", "point"))
	add("ATK_OP_ATTR_RATE_DOWN_INVALID", "忽略不利属性倍率", "本技能攻击时不承受不利属性造成的伤害倍率降低。", "")
	add("ATK_OP_REFLECTION_INVALID", "攻击不受反弹", "本技能指定伤害类型的直接攻击不会触发目标的伤害反弹。", "", p("忽略反弹的伤害类型", "ALL所有、PHYSICS物理、MAGIC魔法。", ""))
	for _, entry := range []struct{ code, name, desc string }{
		{"DEBUFF_RELEASE_ONE", "解除指定减益", "尝试解除目标身上指定类别的减益状态。"}, {"BUFF_RELEASE_ONE", "解除指定增益", "尝试解除目标身上指定类别的增益状态。"},
	} {
		add(entry.code, entry.name, entry.desc, "解除概率＝基础概率＋概率成长×等级。", chance, chanceGrowth, p("解除类别一", "指定要解除的状态类型，NULL表示空类别。", ""), p("解除类别二", "第二个要解除的状态类型。", ""))
	}
	add("BUFF_RELEASE", "解除增益", "尝试解除目标身上的增益效果。", "解除概率＝基础概率＋概率成长×等级。", chance, chanceGrowth)
	add("CURSE_RELEASE", "解除诅咒", "尝试解除追加诅咒卡，独立于普通减益状态。", "解除概率＝基础概率＋概率成长×等级。", chance, chanceGrowth, p("诅咒属性过滤", "NULL表示不限制属性。", ""))
	add("DEAL_BONUS", "增加抽卡数量", "增加后续抽牌阶段的抽卡数量。", "", p("额外抽卡张数", "例如1表示额外抽1张。", "count"))
	add("DEAL_PENALTY", "减少抽卡数量", "减少后续抽牌阶段的抽卡数量。", "", p("减少抽卡张数", "例如1表示少抽1张。", "count"))
	add("BURST_GAUGE_QUICK_UP", "立即增加爆发槽", "立刻为目标增加爆发槽数值，受爆发槽状态与上限影响。", "增加量＝基础值＋等级成长×等级÷1000，再计入Chain。", p("增加的爆发槽数值", "这是槽数值，不是一个通用百分比。", "point"), p("爆发槽等级成长", "此值×等级÷1000加入基础值。", "scaled_growth"))
	add("TRANCE_GAUGE_VALUE_DOWN", "减少敌方超越槽", "降低指定状态敌人的超越槽；敌人处于超越状态时，会反向增加过热槽。", "变化比例＝基础比例＋比例成长×等级。", p("适用超越状态", "TRANCE表示敌人处于超越状态时生效。", ""), p("槽变化比例", "按该状态槽上限的百分比计算。", "percent"), p("比例等级成长", "每级增加的百分比。", "percent"))
	add("BLESS", "设置祝福追加卡", "放置模板的祝福追加技能，满足条件后执行；新增此效果不会自动创建新的追加技能。", "", p("祝福保留回合", "0时沿用模板追加技能的持续回合。", "turn"), unused, p("是否重复触发", "0为一次，非0允许在保留时间内重复触发。", "flag"), p("展示值1基础值", "只用于客户端祝福介绍的展示值1，不改变呼出技能的实际效果。", "point"), p("展示值1等级成长", "展示值1基础值＋此值×等级。", "point"), p("展示值2基础值", "只用于客户端祝福介绍的展示值2。", "point"), p("展示值2等级成长", "展示值2基础值＋此值×等级。", "point"), p("展示值3基础值", "只用于客户端祝福介绍的展示值3。", "point"), p("展示值3等级成长", "展示值3基础值＋此值×等级。", "point"))
	add("BLESS_TURN_UP", "延长祝福", "增加目标已有祝福追加卡的剩余回合。", "", p("增加的回合数", "加到已有祝福的剩余回合，不创建新的祝福。", "turn"), p("属性过滤", "沿用模板的祝福属性过滤。", ""))
	add("ATTR_SEE", "查看隐藏属性", "让目标隐藏的属性可以被看见。", "", duration)
	add("CARD_SEAL", "封印手牌", "随机封印符合技能类别、属性和COST筛选的手牌，封印期间不能正常使用。", "持续回合在上下限内随机；封印张数也在上下限内随机。", p("封印回合下限", "随机持续时间的最小回合数。", "turn"), p("封印回合上限", "随机持续时间的最大回合数。", "turn"), p("封印张数下限", "符合筛选的手牌中随机选择的最少张数。", "count"), p("封印张数上限", "最多封印的张数。", "count"), p("筛选技能类别", "例如SORCERY魔法攻击，ALL不限。", ""), p("筛选属性", "ALL不限。", ""), p("筛选COST下限", "与上限共同限定卡牌技能COST。", "point"), p("筛选COST上限", "与下限共同限定卡牌技能COST。", "point"), p("包含／排除筛选", "非0表示只包含匹配筛选的卡；0表示排除匹配筛选的卡。", "flag"))
	add("CARD_TRAP_DAMAGE", "手牌陷阱伤害", "随机为手牌设置陷阱，触发时造成独立伤害。", "伤害＝固定值＋固定成长×等级÷1000＋参照能力×（倍率＋倍率成长×等级）÷1000。", duration, p("陷阱张数下限", "随机设置陷阱的最少张数。", "count"), p("陷阱张数上限", "最多设置陷阱的张数。", "count"), p("固定陷阱伤害", "陷阱伤害的固定部分。", "point"), p("固定伤害等级成长", "此值×等级÷1000加入固定伤害。", "scaled_growth"), p("能力伤害倍率", "1000表示参照能力的100%。", "permille"), p("能力倍率等级成长", "每级增加的千分比。", "permille"), source)

	// Preserve rule order exactly; the editor's parameter index is also the CSV
	// column offset. Unknown functions get explicit uncertainty, never guessed units.
	for code, types := range rules {
		entry, known := help[code]
		if !known {
			entry = customEffectHelp{Name: code, Description: "此效果尚未配置中文说明，请结合来源卡技能说明并保留不确定的模板参数。"}
		}
		params := make([]customEffectParameterHelp, len(types))
		for i, kind := range types {
			if i < len(entry.Parameters) {
				params[i] = entry.Parameters[i]
			} else if known {
				params[i] = unused
			} else {
				params[i] = p(fmt.Sprintf("参数%d（待确认）", i+1), "尚未确认用途与单位，建议保留模板值。", "")
			}
			if kind == "" {
				params[i] = p("未使用", "模板未定义此参数。", "")
			}
		}
		entry.Parameters = params
		entry.DescriptionSlots = customDescriptionSlots(code)
		help[code] = entry
	}
	// Return only functions supported by this resource set's parameter rules.
	for code := range help {
		if _, ok := rules[code]; !ok {
			delete(help, code)
		}
	}
	return help
}

func customCardTargetLabels() map[string]string {
	return map[string]string{"SELECT": "沿用技能选择目标", "SELF": "自身", "USER_ONE": "己方单体", "USER_ALL": "己方全体", "FRIEND_ALL": "己方全体", "ENEMY_ONE": "敌方单体", "ENEMY_ALL": "敌方全体", "RANDOM": "随机目标"}
}

func customCardValueLabels() map[string]string {
	labels := map[string]string{"ATK": "物理攻击", "INT": "魔法攻击", "MND": "回复量", "DEF": "物理防御", "MDEF": "魔法防御", "HP": "当前HP", "MAX_HP": "最大HP", "PHYSICS": "物理", "MAGIC": "魔法", "ALL": "全部／不限", "NULL": "空／不限", "FIRE": "火", "ICE": "冰", "WIND": "风", "LIGHT": "光", "DARK": "暗", "TRANCE": "超越状态", "OVER_HEAT": "过热状态", "SORCERY": "魔法攻击技能", "PHYSICS_ATTACK": "物理攻击技能", "HEAL": "回复技能", "SUPPORT": "支援技能", "JAMMING": "妨害技能"}
	for _, group := range []struct {
		prefix string
		items  map[string]string
	}{
		{"", map[string]string{"POISON": "中毒", "BURN": "燃烧", "FREEZE": "冰冻", "BLEED": "裂伤", "ELECTRIC": "感电", "CARD_SEAL": "卡牌封印", "DARKNESS": "黑暗", "STAN": "眩晕"}},
	} {
		for code, name := range group.items {
			labels[group.prefix+code] = name
		}
	}
	for code, name := range map[string]string{"ATK": "物理攻击", "INT": "魔法攻击", "MND": "回复量", "DEF": "物防", "MDEF": "魔防"} {
		labels["ATK_UP_BY_"+code] = name + "提升"
		labels["DEF_UP_BY_"+code] = name + "提升"
	}
	// Attribute combinations retain each readable component.
	for _, a := range []string{"FIRE", "ICE", "WIND", "LIGHT", "DARK"} {
		for _, b := range []string{"FIRE", "ICE", "WIND", "LIGHT", "DARK"} {
			if a != b {
				labels[a+"_"+b] = strings.Join([]string{labels[a], labels[b]}, "＋")
			}
		}
	}
	return labels
}
