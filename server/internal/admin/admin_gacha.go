package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

type AdminGachaConfig struct {
	CardNum      int                        `json:"card_num,omitempty"`
	BannerKey    string                     `json:"banner_key,omitempty"`
	Gifts        []gamestate.GachaGiftRule  `json:"gift_rules,omitempty"`
	PayType      int                        `json:"pay_type,omitempty"` // Zero inherits the original payment for old documents.
	PayTypeID    int                        `json:"pay_typeid,omitempty"`
	GachaID      int                        `json:"gacha_id"`
	Name         string                     `json:"name"`
	Price        int                        `json:"price"`
	PlayCountMax int                        `json:"play_count_max,omitempty"`
	StartUnix    int64                      `json:"start_unix"`
	EndUnix      int64                      `json:"end_unix"`
	CardIDs      []int                      `json:"card_ids"`
	Weights      []int                      `json:"weights"`
	RewardPool   []gamestate.WeightedReward `json:"reward_pool,omitempty"`
	Steps        []gamestate.GachaStep      `json:"steps,omitempty"`
	BoxRounds    []gamestate.GachaBoxRound  `json:"box_rounds,omitempty"`
	CardFames    map[int]int                `json:"card_fames,omitempty"` // card ID → fame when drawn; missing means 1
	CoverPath    string                     `json:"cover_path,omitempty"` // uploaded cover suffix; empty keeps the banner
	// Closed hides this draw method while the pool stays open through its other methods (「扭蛋发布」 opens the
	// pool itself). A new key: the retired "disabled" field of older documents must not close anything.
	Closed bool `json:"closed,omitempty"`
}

func AdminGachaConfigFromProfile(profile gamestate.GachaProfile) AdminGachaConfig {
	profile = gamestate.CloneGachas([]gamestate.GachaProfile{profile})[0]
	// Cloning an empty reward slice produces nil; the editor needs an empty
	// list for newly created rounds and older, unfinished box configurations.
	for i := range profile.BoxRounds {
		if profile.BoxRounds[i].Rewards == nil {
			profile.BoxRounds[i].Rewards = []gamestate.GachaBoxReward{}
		}
	}
	return AdminGachaConfig{PayType: profile.PayType, PayTypeID: profile.PayTypeID, CardNum: profile.CardNum, BannerKey: profile.BannerKey, Gifts: profile.Gifts, GachaID: profile.GachaID, Name: profile.Name, Price: profile.Price, PlayCountMax: profile.PlayCountMax, CardIDs: append([]int{}, profile.CardIDs...), Weights: append([]int{}, profile.CardWeights...), RewardPool: profile.RewardPool, Steps: profile.Steps, BoxRounds: profile.BoxRounds, CardFames: profile.CardFames, CoverPath: profile.CoverPath}
}

func adminConfiguredGacha(base gamestate.GachaProfile, config AdminGachaConfig) game.GachaConfiguration {
	base = gamestate.CloneGachas([]gamestate.GachaProfile{base})[0]
	if config.PayType != 0 {
		if base.PayType == 2 && config.PayType != 2 {
			base.CardNumMax = base.CardNum
			base.DailyFirstFree = false
		}
		base.PayType, base.PayTypeID = config.PayType, config.PayTypeID
	}
	if config.CardNum > 0 && base.PublicationKey == "custom" && !base.FixedDrawCount {
		base.CardNum, base.CardNumMax = config.CardNum, config.CardNum
	}
	if base.PublicationKey == "custom" {
		base.BannerKey, base.Gifts = config.BannerKey, config.Gifts
	}
	base.Name, base.Price = config.Name, config.Price
	base.PlayCountMax = config.PlayCountMax
	payment := map[int]string{2: "友情点", 3: "水晶", 4: fmt.Sprintf("物品 %d", base.PayTypeID), 6: "付费水晶"}[base.PayType]
	base.BuyMessage = fmt.Sprintf("消耗 %d %s 抽取 %d 张卡牌吗？", config.Price, payment, base.CardNum)
	if base.PayType == 2 && base.CardNumMax > base.CardNum {
		base.BuyMessage = fmt.Sprintf("每张消耗 %d 友情点，最多连续抽取 %d 张卡牌吗？", config.Price, base.CardNumMax)
	}
	base.SubMessage = "本地运营卡池；卡牌与概率见详情。"
	if base.UnownedOnly {
		base.SubMessage = "必得未入手六星卡；按图鉴同系排除已获得卡，剩余卡牌按配置权重抽取。"
	}
	base.CardIDs, base.CardWeights = append([]int(nil), config.CardIDs...), append([]int(nil), config.Weights...)
	base.CoverPath = config.CoverPath
	if len(base.BoxRounds) > 0 {
		base.BoxRounds = config.BoxRounds
		base = gamestate.CloneGachas([]gamestate.GachaProfile{base})[0]
	}
	base.CardFames = nil
	if len(config.CardFames) > 0 {
		base.CardFames = make(map[int]int, len(config.CardFames))
		for id, fame := range config.CardFames {
			base.CardFames[id] = fame
		}
	}
	if len(base.RewardPool) > 0 {
		base.RewardPool, base.Steps = config.RewardPool, config.Steps
		base.BuyMessage = fmt.Sprintf("消耗 %d %s 抽取 %d 次吗？", config.Price, payment, base.CardNum)
		base.SubMessage = "本地复刻；阶段费用、奖励概率及赠礼见详情。"
		base = gamestate.CloneGachas([]gamestate.GachaProfile{base})[0]
	}
	base.EndTime = 2147483647
	if config.EndUnix != 0 {
		base.EndTime = int(config.EndUnix)
	}
	return game.GachaConfiguration{Profile: base, StartUnix: config.StartUnix, EndUnix: config.EndUnix, Disabled: config.Closed}
}

func validateAdminGachaPayment(catalog map[string]AdminCatalogEntry, config AdminGachaConfig) error {
	switch config.PayType {
	case 0, 2, 3, 6:
		if config.PayTypeID != 0 {
			return errors.New("非道具消耗不能填写道具ID")
		}
	case 4:
		if config.PayTypeID <= 0 {
			return errors.New("请选择扭蛋消耗的道具")
		}
		entry, ok := catalog[adminCatalogKey(8, config.PayTypeID)]
		if !ok || entry.ResourceState == "unavailable" {
			return errors.New("扭蛋消耗道具不在当前可用道具目录中")
		}
	default:
		return errors.New("不支持的扭蛋消耗方式")
	}
	return nil
}

func (operations *Operations) prepareGachas(handler http.Handler) {
	configurator, ok := handler.(game.GachaConfigurator)
	if !ok {
		return
	}
	operations.configMu.RLock()
	defer operations.configMu.RUnlock()
	configurator.ApplyGachaConfiguration(operations.gachaRevision, operations.gachaConfigurations)
}

func (operations *Operations) reloadGachaConfigurations() error {
	configs, revision, err := operations.readGachaConfigurations()
	if err != nil {
		return err
	}
	operations.gachaConfigurations, operations.gachaRevision = configs, revision
	operations.syncCustomGachaCatalog()
	return nil
}

// Build the next snapshot without changing the currently published one.
func (operations *Operations) readGachaConfigurations() ([]game.GachaConfiguration, uint64, error) {
	configs := make([]game.GachaConfiguration, 0)
	var revision uint64
	ids := make([]int, 0, len(operations.gachaBases))
	for id := range operations.gachaBases {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		doc, err := operations.storage.ReadDocument("gacha-live:" + strconv.Itoa(id))
		if err != nil {
			return nil, 0, err
		}
		_, custom := operations.customGachas[id]
		if doc.Revision == 0 && (!custom || operations.legacyCustomGachas[id]) {
			continue
		}
		// Operator pools always carry a configuration so accounts can append (or hide) them.
		config := AdminGachaConfigFromProfile(operations.gachaBases[id])
		if doc.Revision > 0 {
			if err := json.Unmarshal(doc.Payload, &config); err != nil {
				return nil, 0, err
			}
		}
		if config.GachaID != id {
			return nil, 0, errors.New("gacha operation identity mismatch")
		}
		configured := operations.configuredGacha(id, config)
		if custom && doc.Revision == 0 {
			configured.Disabled = true
		}
		configs = append(configs, configured)
		revision += uint64(doc.Revision)
	}
	revision += uint64(operations.customGachaRevision)
	return configs, revision, nil
}

// configuredGacha adds the operator-pool flags (append to accounts; hidden while deleted).
func (operations *Operations) configuredGacha(id int, config AdminGachaConfig) game.GachaConfiguration {
	configured := adminConfiguredGacha(operations.gachaBases[id], config)
	if pool, custom := operations.customGachas[id]; custom {
		configured.Operator, configured.Disabled = true, configured.Disabled || pool.Deleted
	}
	return configured
}

func (admin *API) validateStoredGachas() error {
	groups := make(map[int][]AdminGachaConfig)
	boxGroups := make(map[int]bool)
	for _, config := range admin.operations.gachaConfigurations {
		if pool, custom := admin.operations.customGachas[config.Profile.GachaID]; custom && pool.Deleted {
			// A retained tombstone must not prevent starting the server.
			continue
		}
		edit := AdminGachaConfigFromProfile(config.Profile)
		edit.StartUnix, edit.EndUnix = config.StartUnix, config.EndUnix
		edit.PayType, edit.PayTypeID = config.Profile.PayType, config.Profile.PayTypeID
		published := true
		if _, custom := admin.operations.customGachas[config.Profile.GachaID]; custom {
			live, err := admin.operations.storage.ReadDocument("gacha-live:" + strconv.Itoa(config.Profile.GachaID))
			if err != nil {
				return err
			}
			published = live.Revision > 0
		}
		// An unpublished variant may inherit a newer saved draft while the other
		// variants still run their old templates. Only live members must agree.
		if published {
			groups[config.Profile.GroupID] = append(groups[config.Profile.GroupID], edit)
			boxGroups[config.Profile.GroupID] = boxGroups[config.Profile.GroupID] || len(config.Profile.BoxRounds) > 0
		}
		preview, err := admin.validateGachaConfig(edit)
		if err != nil {
			return err
		}
		if !preview["publishable"].(bool) {
			return errors.New("published gacha card resources are unavailable")
		}
	}
	for groupID, configs := range groups {
		if boxGroups[groupID] {
			if err := admin.operations.gachaGroupShared(configs); err != nil {
				return fmt.Errorf("箱池组 %d：%w", groupID, err)
			}
		}
	}
	return nil
}

func (admin *API) validateGachaConfig(config AdminGachaConfig) (map[string]any, error) {
	base, exists := admin.operations.gachaBases[config.GachaID]
	if !exists {
		return nil, errors.New("不能编辑新手保留卡池或未知卡池")
	}
	if pool := admin.operations.customGachas[config.GachaID]; (pool.RuleVersion == 3 || len(base.BoxRounds) > 0) && config.CardNum != base.CardNum {
		return nil, fmt.Errorf("新建抽法固定为%d抽，请重新载入后保存", base.CardNum)
	}
	if len(base.BoxRounds) == 0 && len(config.BoxRounds) > 0 {
		return nil, errors.New("请新建箱池，普通池和阶段池不能添加箱池模板")
	}
	if pool, custom := admin.operations.customGachas[config.GachaID]; custom && pool.Deleted {
		return nil, errors.New("卡池已删除，恢复后才能编辑或发布")
	}
	if err := validateAdminGachaPayment(admin.catalogByKey, config); err != nil {
		return nil, err
	}
	if err := admin.validateGachaCover(config.CoverPath); err != nil {
		return nil, err
	}
	if strings.TrimSpace(config.Name) == "" || len([]rune(config.Name)) > 60 || config.Price < 1 || config.Price > 10000000 {
		return nil, errors.New("名称须为 1–60 字；价格须为 1–10000000 的整数")
	}
	if config.PlayCountMax < 0 || config.PlayCountMax > 1000000 {
		return nil, errors.New("每名玩家累计限抽次数须为0–1000000，0表示不限，1表示一次性")
	}
	if config.StartUnix < 0 || config.EndUnix < 0 || config.StartUnix > 2147483647 || config.EndUnix > 2147483647 || (config.EndUnix != 0 && config.EndUnix <= config.StartUnix) {
		return nil, errors.New("排期无效：结束时间须晚于开始时间且早于 2038-01-19")
	}
	if len(base.BoxRounds) > 0 {
		if len(config.BoxRounds) == 0 {
			return nil, errors.New("箱池不能移除轮次模板")
		}
		if err := admin.validateCustomGachaMetadata(base, config); err != nil {
			return nil, err
		}
		return admin.validateCustomBoxGacha(base, config)
	}
	if admin.operations.legacyCustomGachas[config.GachaID] {
		if err := admin.validateCustomGachaMetadata(base, config); err != nil {
			return nil, err
		}
		if len(base.RewardPool) > 0 {
			return admin.validateCustomMixedGacha(base, config)
		}
	}
	if len(config.BoxRounds) > 0 {
		return nil, errors.New("普通池或阶段池不能附加箱池模板，请新建箱池")
	}
	if len(base.RewardPool) > 0 {
		if len(config.CardFames) > 0 {
			return nil, errors.New("混合奖励卡池不能设置抽出名声")
		}
		return ValidateMixedGachaConfig(admin.catalogByKey, base, config)
	}
	if len(config.RewardPool) > 0 || len(config.Steps) > 0 {
		return nil, errors.New("普通卡池不能附加混合奖励规则")
	}
	if len(config.CardIDs) == 0 || len(config.CardIDs) > 6000 || len(config.CardIDs) != len(config.Weights) || len(config.CardIDs) < base.UserSelectMax {
		return nil, errors.New("卡牌与权重数量不匹配，或不足自选数量")
	}
	seen := make(map[int]bool)
	blocked := make([]int, 0)
	rarities := make(map[int]int)
	for index, id := range config.CardIDs {
		entry, exists := admin.catalogByKey[adminCatalogKey(6, id)]
		if !exists || seen[id] || config.Weights[index] < 1 || config.Weights[index] > 1000000 {
			return nil, fmt.Errorf("卡牌 %d 不存在、重复或权重超出 1–1000000", id)
		}
		if !adminGachaCardEligible(base, entry) {
			return nil, fmt.Errorf("卡牌 %d 不符合此分池的职业或星级规则", id)
		}
		seen[id] = true
		if entry.ResourceState == "unavailable" {
			blocked = append(blocked, id)
		}
		rarities[entry.Rarity]++
	}
	for id, fame := range config.CardFames {
		if !seen[id] {
			return nil, fmt.Errorf("名声设置中的卡牌 %d 不在此卡池", id)
		}
		if limit := admin.catalogByKey[adminCatalogKey(6, id)].FameMax; fame < 1 || fame > limit {
			return nil, fmt.Errorf("卡牌 %d 的抽出名声须为 1–%d", id, limit)
		}
	}
	if base.GuaranteedCount > 0 {
		if rarities[base.GuaranteedRarityRank] == 0 || rarities[base.RemainderRarityRank] == 0 {
			return nil, errors.New("此池保底要求两种指定稀有度都有候选卡")
		}
		for rarity := range rarities {
			if rarity != base.GuaranteedRarityRank && rarity != base.RemainderRarityRank {
				return nil, fmt.Errorf("稀有度 %d 不参与此池的保底或普通抽取", rarity)
			}
		}
	}
	odds, err := game.PreviewGachaWeights(config.Weights)
	if err != nil {
		return nil, err
	}
	rarityByID := make(map[int]int, len(config.CardIDs))
	for _, id := range config.CardIDs {
		rarityByID[id] = admin.catalogByKey[adminCatalogKey(6, id)].Rarity
	}
	profile := adminConfiguredGacha(base, config).Profile
	if err := gamestate.ValidateGachaRules(profile); err != nil {
		return nil, err
	}
	stages, err := game.PreviewGachaStages(profile, rarityByID)
	if err != nil {
		return nil, err
	}
	warnings := []string{}
	if base.UnownedOnly {
		warnings = append(warnings, "仅可配置六星扭蛋初始卡；实际概率会排除玩家图鉴已获得的同系卡后重新计算，全部获得后隐藏此池")
	}
	if len(blocked) > 0 {
		warnings = append(warnings, "部分卡牌尚未进入当前运行资源清单，可以保存草稿，发布前须完成资源闭包")
	}
	if base.UserSelectMax > 0 {
		warnings = append(warnings, "自选池的实际概率按玩家选定卡牌重新归一化")
	}
	if base.PayType == 2 && base.CardNumMax > base.CardNum {
		warnings = append(warnings, "友情点连抽按余额决定抽数，价格为每抽价格；每日首次免费规则保持不变")
	}
	return map[string]any{"config": config, "base": profile, "odds_scaled": odds, "odds_scale": 100000, "rarities": rarities, "blocked_card_ids": blocked, "publishable": len(blocked) == 0, "warnings": warnings, "stages": stages}, nil
}

func (admin *API) gachaEditorList(writer http.ResponseWriter, request *http.Request) {
	admin.operations.configMu.RLock()
	defer admin.operations.configMu.RUnlock()
	rows := make([]map[string]any, 0, len(admin.operations.gachaBases))
	for id, base := range admin.operations.gachaBases {
		live, err := admin.operations.storage.ReadDocument("gacha-live:" + strconv.Itoa(id))
		if err != nil {
			WriteAdminError(writer, 500, err.Error())
			return
		}
		draft, err := admin.operations.storage.ReadDocument("gacha-draft:" + strconv.Itoa(id))
		if err != nil {
			WriteAdminError(writer, 500, err.Error())
			return
		}
		config := AdminGachaConfigFromProfile(base)
		if live.Revision > 0 {
			if err := json.Unmarshal(live.Payload, &config); err != nil {
				WriteAdminError(writer, 500, err.Error())
				return
			}
		}
		pool, custom := admin.operations.customGachas[id]
		rows = append(rows, map[string]any{"gacha_id": id, "config": config, "base": base, "live": live, "draft": draft, "custom": custom, "legacy": admin.operations.legacyCustomGachas[id], "deleted": pool.Deleted, "template_id": pool.TemplateID, "banner_url": "/gacha-assets/" + base.BannerKey + ".png"})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i]["gacha_id"].(int) < rows[j]["gacha_id"].(int) })
	banners := []string{}
	for key := range admin.gachaBannerPaths {
		banners = append(banners, key)
	}
	sort.Strings(banners)
	WriteAdminJSON(writer, 200, map[string]any{"state": "PASS", "banners": banners, "pools": rows, "custom_revision": admin.operations.customGachaRevision, "rule_templates": admin.gachaRuleTemplates()})
}

func (admin *API) gachaEditorAction(writer http.ResponseWriter, request *http.Request) {
	if err := requireAdminMutation(request); err != nil {
		WriteAdminError(writer, 403, err.Error())
		return
	}
	var body struct {
		Config               AdminGachaConfig `json:"config"`
		ExpectedRevision     int              `json:"expected_revision"`
		ExpectedLiveRevision int              `json:"expected_live_revision"`
		SHA256               string           `json:"sha256"`
	}
	if err := DecodeAdminJSONLimit(request, &body, 2*1024*1024); err != nil {
		WriteAdminError(writer, 400, err.Error())
		return
	}
	admin.operations.configMu.Lock()
	defer admin.operations.configMu.Unlock()
	action := chi.URLParam(request, "action")
	if action != "preview" && action != "draft" && action != "publish" {
		WriteAdminError(writer, 404, "unknown editor action")
		return
	}
	key := strconv.Itoa(body.Config.GachaID)
	if base, ok := admin.operations.gachaBases[body.Config.GachaID]; ok && len(base.BoxRounds) > 0 && len(admin.operations.gachaGroupMembers(base.GroupID)) > 1 && action != "preview" {
		WriteAdminError(writer, 400, "无限池包含多种抽法，奖励与轮次共用，请重新加载页面后整池保存或发布")
		return
	}
	if action == "publish" {
		draft, err := admin.operations.storage.ReadDocument("gacha-draft:" + key)
		if err != nil {
			WriteAdminError(writer, 500, err.Error())
			return
		}
		if !gachaDraftExists(draft) || draft.Revision != body.ExpectedRevision || draft.SHA256 != body.SHA256 {
			WriteAdminError(writer, 409, accountstore.ErrDocumentConflict.Error())
			return
		}
		if err := json.Unmarshal(draft.Payload, &body.Config); err != nil {
			WriteAdminError(writer, 500, err.Error())
			return
		}
		if base, ok := admin.operations.gachaBases[body.Config.GachaID]; ok && !admin.operations.legacyCustomGachas[body.Config.GachaID] && len(admin.operations.gachaGroupMembers(base.GroupID)) > 1 {
			WriteAdminError(writer, 400, "此卡池包含多种抽法，请重新加载页面后整池发布")
			return
		}
	}
	preview, err := admin.validateGachaConfig(body.Config)
	if err != nil {
		WriteAdminError(writer, 400, err.Error())
		return
	}
	if action == "preview" {
		WriteAdminJSON(writer, 200, map[string]any{"state": "PASS", "preview": preview})
		return
	}
	operationKey, expected := "gacha-draft:"+key, body.ExpectedRevision
	if action == "publish" {
		if !preview["publishable"].(bool) {
			WriteAdminError(writer, 400, "发布被资源闭包阻止，详见预览中的卡牌 ID")
			return
		}
		operationKey, expected = "gacha-live:"+key, body.ExpectedLiveRevision
	}
	writes := []accountstore.DocumentWrite{{Key: operationKey, Expected: expected, Value: body.Config, Operation: strings.Split(operationKey, ":")[0]}}
	if action == "publish" {
		writes = append(writes, accountstore.DocumentWrite{Key: "gacha-draft:" + key, Expected: body.ExpectedRevision, Value: nil, Operation: "gacha-draft-consumed"})
	}
	docs, err := admin.operations.storage.WriteDocuments(writes)
	if err != nil {
		status := 500
		if errors.Is(err, accountstore.ErrDocumentConflict) {
			status = 409
		}
		WriteAdminError(writer, status, err.Error())
		return
	}
	if action == "publish" {
		admin.operations.applyPublishedGacha(body.Config)
		admin.operations.syncCustomGachaCatalog()
	}
	result := map[string]any{"state": "PASS", "document": docs[0], "preview": preview}
	if action == "publish" {
		result["draft"] = docs[1]
	}
	WriteAdminJSON(writer, 200, result)
}
