package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/gamestate"
)

// Additional draw methods keep a complete immutable rule snapshot. They share
// the original publication group, but never replace a player's existing IDs.
func ordinaryGachaVariantAllowed(p gamestate.GachaProfile) bool {
	return p.GroupID > 0 && p.GachaType == 0 && p.CategoryNum != 10000 &&
		p.CardNum > 0 && p.CardNum <= 11 && p.CardNum == p.CardNumMax &&
		p.UserSelectMax == 0 && !p.DailyFirstFree && !p.UnownedOnly &&
		len(p.RewardPool) == 0 && len(p.Steps) == 0 && len(p.BoxRounds) == 0
}

func boxGachaVariantAllowed(p gamestate.GachaProfile) bool {
	return p.GroupID > 0 && p.GachaType == 0 && p.CategoryNum != 10000 &&
		gamestate.GachaBoxDrawCountAllowed(p.CardNum) && p.CardNum == p.CardNumMax &&
		p.UserSelectMax == 0 && !p.DailyFirstFree && !p.UnownedOnly &&
		p.PlayCountMax == 0 && p.GuaranteedCount == 0 &&
		len(p.BoxRounds) == gamestate.GachaBoxTemplates && len(p.CardIDs) == 0 &&
		len(p.CardWeights) == 0 && len(p.CardFames) == 0 && len(p.Gifts) == 0 &&
		len(p.RewardPool) == 0 && len(p.Steps) == 0
}

func additionalGachaVariantAllowed(p gamestate.GachaProfile) bool {
	return ordinaryGachaVariantAllowed(p) || boxGachaVariantAllowed(p)
}

// The stock picker can distinguish professions and draw sizes, but cannot
// offer two new payment alternatives for the same profession/draw size.
// Keep pre-existing built-in ticket/crystal alternatives working as before.
func (o *Operations) validateAddedGachaVariantChoices(configs []AdminGachaConfig) error {
	for _, c := range configs {
		if o.customGachas[c.GachaID].RuleVersion != 3 {
			continue
		}
		p := adminConfiguredGacha(o.gachaBases[c.GachaID], c).Profile
		for _, other := range configs {
			if c.GachaID == other.GachaID {
				continue
			}
			candidate := adminConfiguredGacha(o.gachaBases[other.GachaID], other).Profile
			if (len(p.BoxRounds) > 0 || p.ArthurType == candidate.ArthurType) && p.CardNum == candidate.CardNum && p.CardNumMax == candidate.CardNumMax {
				return fmt.Errorf("抽法 %d 与 %d 的职业及抽数相同，同职业的新增入口须保持不同抽数", c.GachaID, other.GachaID)
			}
		}
	}
	return nil
}

// savedGachaVariantConfig prefers a saved draft, falling back to the live
// document or the built-in configuration. The caller has configMu.
func savedGachaVariantConfig(base gamestate.GachaProfile, draft, live accountstore.Document) (AdminGachaConfig, error) {
	c := AdminGachaConfigFromProfile(base)
	doc := live
	if gachaDraftExists(draft) {
		doc = draft
	}
	if doc.Revision > 0 && gachaDraftExists(doc) {
		if err := json.Unmarshal(doc.Payload, &c); err != nil {
			return c, err
		}
	}
	if c.GachaID != base.GachaID {
		return c, errors.New("gacha operation identity mismatch")
	}
	return c, nil
}

func (admin *API) createGachaVariant(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		SourceID int                           `json:"source_id"`
		CardNum  int                           `json:"card_num"`
		Name     string                        `json:"name"`
		PayType  int                           `json:"pay_type"`
		PayID    int                           `json:"pay_typeid"`
		Price    int                           `json:"price"`
		Revision *int                          `json:"expected_revision"`
		Expected map[int]gachaGroupExpectation `json:"expected"`
	}
	if err := DecodeAdminJSONLimit(r, &body, 128*1024); err != nil || body.Revision == nil {
		WriteAdminError(w, 400, "请携带来源分池、抽数及整池配置版本")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" || len([]rune(body.Name)) > 60 || !gamestate.GachaBoxDrawCountAllowed(body.CardNum) || body.Price < 1 || body.Price > 10000000 || body.PayType == 0 {
		WriteAdminError(w, 400, "名称须为 1–60 字，抽数须为 1–11（无限箱池另支持 50 抽），价格须为 1–10000000，并选择消耗方式")
		return
	}
	o := admin.operations
	o.configMu.Lock()
	defer o.configMu.Unlock()
	if *body.Revision != o.customGachaRevision {
		WriteAdminError(w, 409, accountstore.ErrDocumentConflict.Error())
		return
	}
	source, found := o.gachaBases[body.SourceID]
	if !found || source.GroupID == 0 {
		WriteAdminError(w, 400, "来源分池不存在或为新手保留卡池")
		return
	}
	members := o.gachaGroupMembers(source.GroupID)
	if len(members) == 0 || len(body.Expected) != len(members) {
		WriteAdminError(w, 409, "卡池抽法已变化，请重新载入后创建")
		return
	}
	configs := make(map[int]AdminGachaConfig, len(members))
	box := len(source.BoxRounds) > 0
	if !box && body.CardNum > 11 {
		WriteAdminError(w, 400, "普通卡池的新增抽法须为 1–11 抽，50 抽仅用于无限箱池")
		return
	}
	for _, id := range members {
		base := o.gachaBases[id]
		if pool, custom := o.customGachas[id]; custom && pool.Deleted {
			WriteAdminError(w, 400, "卡池在回收站中，请先恢复")
			return
		}
		if !additionalGachaVariantAllowed(base) {
			WriteAdminError(w, 400, "仅普通固定抽数卡池和无限箱池可添加抽法；阶段池、每日免费池及其他特殊规则池请使用原有模板")
			return
		}
		if (len(base.BoxRounds) > 0) != box {
			WriteAdminError(w, 400, "同一卡池不能混用普通卡牌和箱池抽法")
			return
		}
		key := strconv.Itoa(id)
		draft, err := o.storage.ReadDocument("gacha-draft:" + key)
		if err != nil {
			WriteAdminError(w, 500, err.Error())
			return
		}
		live, err := o.storage.ReadDocument("gacha-live:" + key)
		if err != nil {
			WriteAdminError(w, 500, err.Error())
			return
		}
		expected, exists := body.Expected[id]
		if !exists || expected.Draft != draft.Revision || expected.Live != live.Revision || (gachaDraftExists(draft) && expected.SHA256 != draft.SHA256) {
			WriteAdminError(w, 409, accountstore.ErrDocumentConflict.Error())
			return
		}
		c, err := savedGachaVariantConfig(base, draft, live)
		if err != nil {
			WriteAdminError(w, 500, err.Error())
			return
		}
		if _, err := admin.validateGachaConfig(c); err != nil {
			WriteAdminError(w, 400, fmt.Sprintf("来源抽法 %d 的已保存配置无效：%v", id, err))
			return
		}
		p := adminConfiguredGacha(base, c).Profile
		if !additionalGachaVariantAllowed(p) || (len(p.BoxRounds) > 0) != box {
			WriteAdminError(w, 400, "来源卡池的已保存配置不是普通固定抽数卡池或无限箱池")
			return
		}
		if box && id != members[0] && !reflect.DeepEqual(c.BoxRounds, configs[members[0]].BoxRounds) {
			WriteAdminError(w, 400, "同一无限箱池的各抽法必须共用奖励模板，请先整池保存草稿")
			return
		}
		if (box || p.ArthurType == source.ArthurType) && p.CardNum == body.CardNum {
			WriteAdminError(w, 400, "同一卡池的同职业抽法或箱池抽法已存在这种抽数，请编辑现有分池的消耗和价格")
			return
		}
		configs[id] = c
	}
	source = adminConfiguredGacha(source, configs[body.SourceID]).Profile
	if body.CardNum <= source.GuaranteedCount {
		WriteAdminError(w, 400, "新抽数须大于来源分池的保底张数，请选择合适的来源分池")
		return
	}
	id := max(70000001, o.nextCustomGachaID())
	if id >= 80000000 || len(o.customGachas) >= 1000 {
		WriteAdminError(w, 400, "自定义卡池数量或可用 ID 已达上限")
		return
	}
	profile := gamestate.CloneGachas([]gamestate.GachaProfile{source})[0]
	profile.GachaID, profile.Name, profile.CardNum, profile.CardNumMax = id, body.Name, body.CardNum, body.CardNum
	profile.PayType, profile.PayTypeID, profile.Price = body.PayType, body.PayID, body.Price
	profile.PlayCount, profile.GroupPlayCount, profile.DailyFirstAvailable = 0, 0, false
	profile.PublicationKey, profile.FixedDrawCount = "operator", true
	profile.PoolSourceState, profile.WeightSourceState = "LOCAL_POLICY", "LOCAL_POLICY"
	for _, memberID := range members {
		profile.OrderNum = max(profile.OrderNum, o.gachaBases[memberID].OrderNum)
	}
	profile.OrderNum++
	pool := customGachaPool{GachaID: id, GroupID: source.GroupID, Name: body.Name, RuleVersion: 3, SnapshotProfile: &profile}
	if err := o.registerCustomGacha(pool); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	undo := func() {
		delete(o.gachaBases, id)
		delete(o.customGachas, id)
		delete(o.managedGachaGroupByID, id)
	}
	content := configs[body.SourceID]
	content.GachaID, content.Name, content.CardNum = id, body.Name, body.CardNum
	content.PayType, content.PayTypeID, content.Price = body.PayType, body.PayID, body.Price
	content.Closed = false
	lead := configs[members[0]]
	content.CoverPath, content.StartUnix, content.EndUnix, content.PlayCountMax = lead.CoverPath, lead.StartUnix, lead.EndUnix, lead.PlayCountMax
	if _, err := admin.validateGachaConfig(content); err != nil {
		undo()
		WriteAdminError(w, 400, err.Error())
		return
	}
	nextConfigs, _, err := o.readGachaConfigurations()
	if err != nil {
		undo()
		WriteAdminError(w, 500, err.Error())
		return
	}
	docs, err := o.storage.WriteDocuments([]accountstore.DocumentWrite{
		{Key: customGachaKey, Expected: o.customGachaRevision, Value: o.customGachaDocument(), Operation: customGachaKey},
		{Key: "gacha-draft:" + strconv.Itoa(id), Expected: 0, Value: content, Operation: "gacha-variant-draft"},
	})
	if err != nil {
		undo()
		writeContentError(w, err)
		return
	}
	o.customGachaRevision = docs[0].Revision
	o.gachaConfigurations = nextConfigs
	o.syncCustomGachaCatalog()
	o.gachaRevision++
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "gacha_id": id, "group_id": source.GroupID, "gacha_ids": []int{id}, "revision": docs[0].Revision})
}
