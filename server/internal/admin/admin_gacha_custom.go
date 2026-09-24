package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"kairisei.local/server/internal/gamestate"
)

const customGachaCatalogKey = "custom-gacha-catalog"

// Registry entries establish immutable identities and the rule family. Live
// documents carry editable rewards; new identities stay closed until published.
func (o *Operations) loadCustomGachas() error {
	o.customGachas = map[int]bool{}
	doc, err := o.storage.ReadDocument(customGachaCatalogKey)
	if err != nil {
		return err
	}
	if doc.Revision == 0 {
		return nil
	}
	var profiles []gamestate.GachaProfile
	if err := json.Unmarshal(doc.Payload, &profiles); err != nil {
		return err
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].GachaID < profiles[j].GachaID })
	for _, p := range profiles {
		if p.GachaID < 70000000 || p.GachaID >= 80000000 || p.GroupID < 70000000 || p.GroupID >= 80000000 || p.PublicationKey != "custom" {
			return errors.New("invalid custom gacha identity")
		}
		if _, exists := o.gachaBases[p.GachaID]; exists {
			return errors.New("duplicate custom gacha identity")
		}
		if _, exists := o.managedGachaGroups[p.GroupID]; exists && !o.customGachas[p.GroupID] {
			return errors.New("duplicate custom gacha group")
		}
		o.registerCustomGacha(p)
	}
	for _, p := range profiles {
		if root, ok := o.gachaBases[p.GroupID]; !ok || !o.customGachas[p.GroupID] || root.GroupID != p.GroupID {
			return errors.New("invalid custom gacha group root")
		}
	}
	o.lockCustomGroupDrawCounts()
 return nil
}

func (o *Operations) registerCustomGacha(p gamestate.GachaProfile) {
	o.customGachas[p.GachaID] = true
	o.gachaBases[p.GachaID] = p
	o.managedGachaGroups[p.GroupID] = struct{}{}
	o.managedGachaGroupByID[p.GachaID] = p.GroupID
}

func (o *Operations) syncCustomGachaCatalog() {
	profiles := []gamestate.GachaProfile{}
	for _, c := range o.gachaConfigurations {
		if o.customGachas[c.Profile.GachaID] {
			profiles = append(profiles, c.Profile)
		}
	}
	o.storage.SetOperationGachas(profiles)
}

func (a *API) gachaCreate(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		SourceID int    `json:"source_id"`
		Name     string `json:"name"`
		Mode     string `json:"mode"`
	}
	if err := DecodeAdminJSONLimit(r, &body, 4096); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" || len([]rune(body.Name)) > 60 {
		WriteAdminError(w, 400, "名称须为1–60字")
		return
	}
	o := a.operations
	o.configMu.Lock()
	defer o.configMu.Unlock()
	if body.SourceID == 0 && body.Mode == "standard_group" {
		body.SourceID = 60200001
	}
	p := gamestate.GachaProfile{Name: body.Name, CategoryNum: 1, CategoryPictID: 1, PayType: 3, Price: 50, CardNum: 11, CardNumMax: 11, EndTime: 2147483647, BannerKey: "local_standard"}
	if body.SourceID != 0 {
		base, ok := o.gachaBases[body.SourceID]
		if !ok {
			WriteAdminError(w, 400, "请选择可复制的常规卡池")
			return
		}
		p = gamestate.CloneGachas([]gamestate.GachaProfile{base})[0]
		for _, c := range o.gachaConfigurations {
			if c.Profile.GachaID == body.SourceID {
				p = gamestate.CloneGachas([]gamestate.GachaProfile{c.Profile})[0]
			}
		}
	} else if body.Mode == "mixed" {
		// Establish the mixed family; the editor replaces this placeholder before
		// validation/publication. No placeholder can enter a player's catalog.
		p.RewardPool = []gamestate.WeightedReward{{Weight: 1}}
	} else if body.Mode != "" && body.Mode != "ordinary" {
		WriteAdminError(w, 400, "未知卡池类型")
		return
	}
	doc, err := o.storage.ReadDocument(customGachaCatalogKey)
	if err != nil {
		WriteAdminError(w, 500, err.Error())
		return
	}
	profiles := []gamestate.GachaProfile{}
	if doc.Revision > 0 {
		if err := json.Unmarshal(doc.Payload, &profiles); err != nil {
			WriteAdminError(w, 500, err.Error())
			return
		}
	}
	id := 70000001
	for _, existing := range profiles {
		if existing.GachaID >= id {
			id = existing.GachaID + 1
		}
	}
	for id < 80000000 {
		_, pool := o.gachaBases[id]
		_, group := o.managedGachaGroups[id]
		if !pool && !group {
			break
		}
		id++
	}
	if id >= 80000000 || len(profiles) >= 1000 {
		WriteAdminError(w, 400, "自定义卡池数量已达上限")
		return
	}
	sources := []gamestate.GachaProfile{p}
	if body.SourceID != 0 {
		sources = nil
		for _, candidate := range o.gachaBases {
			if candidate.GroupID != p.GroupID {
				continue
			}
			for _, live := range o.gachaConfigurations {
				if live.Profile.GachaID == candidate.GachaID {
					candidate = live.Profile
					break
				}
			}
			sources = append(sources, candidate)
		}
		sort.Slice(sources, func(i, j int) bool {
			if sources[i].OrderNum != sources[j].OrderNum {
				return sources[i].OrderNum < sources[j].OrderNum
			}
			return sources[i].GachaID < sources[j].GachaID
		})
	}
	if len(sources) == 0 || len(profiles)+len(sources) > 1000 || id+len(sources) > 80000000 {
		WriteAdminError(w, 400, "自定义卡池数量已达上限")
		return
	}
	for offset := range sources {
		if _, exists := o.gachaBases[id+offset]; exists {
			WriteAdminError(w, 409, "新卡池ID冲突，请刷新后重试")
			return
		}
		if _, exists := o.managedGachaGroups[id+offset]; exists {
			WriteAdminError(w, 409, "新分组ID冲突，请刷新后重试")
			return
		}
	}
	created := gamestate.CloneGachas(sources)
	ids := []int{}
	for i := range created {
		p := &created[i]
		p.GachaID, p.GroupID, p.PlayCount, p.OrderNum = id+i, id, 0, len(profiles)+100+i
		p.Name, p.PublicationKey, p.EndTime = body.Name, "custom", 2147483647
		p.DailyFirstFree, p.DailyFirstAvailable = false, false
		p.CardNumMax = p.CardNum
		p.BuyMessage = "请在后台配置并发布"
		ids = append(ids, p.GachaID)
	}
	profiles = append(profiles, created...)
	if _, err := o.writeDocument(customGachaCatalogKey, doc.Revision, profiles); err != nil {
		WriteAdminError(w, 409, err.Error())
		return
	}
	for _, p := range created {
		o.registerCustomGacha(p)
	}
	o.lockCustomGroupDrawCounts()
 WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "gacha_id": id, "group_id": id, "gacha_ids": ids})
}

func (a *API) customGachaPresets() []AdminGachaPreset {
	result := []AdminGachaPreset{}
	grouped := map[int]*AdminGachaPreset{}
	ids := []int{}
	for id := range a.operations.customGachas {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		p := a.operations.gachaBases[id]
		for _, c := range a.operations.gachaConfigurations {
			if c.Profile.GachaID == id {
				p = c.Profile
			}
		}
		payment := map[int]string{2: "友情点", 3: "水晶", 6: "付费水晶"}[p.PayType]
		if p.PayType == 4 {
			payment = a.catalogByKey[adminCatalogKey(8, p.PayTypeID)].Name
		}
		if group := grouped[p.GroupID]; group != nil {
			group.GachaIDs = append(group.GachaIDs, id)
		} else {
			grouped[p.GroupID] = &AdminGachaPreset{PublicationKey: "custom", GroupID: p.GroupID, Name: p.Name, BannerKey: p.BannerKey, ImageURL: "/gacha-assets/" + p.BannerKey + ".png", PaymentItemID: p.PayTypeID, PaymentItem: payment, Price: p.Price, DrawCount: p.CardNum, CardCount: len(p.CardIDs) + len(p.RewardPool), GachaIDs: []int{id}}
		}
	}
	for _, group := range grouped {
		result = append(result, *group)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].GroupID < result[j].GroupID })
	return result
}

func (a *API) validateCustomGachaMetadata(base gamestate.GachaProfile, c AdminGachaConfig) error {
 if base.FixedDrawCount && c.CardNum != base.CardNum { return fmt.Errorf("同组抽取入口固定为%d抽，不能更改；请刷新后重新保存草稿",base.CardNum) }
	if c.CardNum < 1 || c.CardNum > 11 || (base.GuaranteedCount > 0 && c.CardNum <= base.GuaranteedCount) {
		return errors.New("每次抽数须为1–11，且须大于继承的保底抽数")
	}
	if c.BannerKey == "" {
		return errors.New("请选择或上传卡池横幅")
	}
	if strings.HasPrefix(c.BannerKey, "custom_") {
		if _, err := a.operations.readCustomGachaBanner(c.BannerKey); err != nil {
			return err
		}
	} else if _, exists := a.gachaBannerPaths[c.BannerKey]; !exists {
		return errors.New("横幅不存在")
	}
	if len(c.Gifts) > 120 {
		return errors.New("赠礼规则最多120项")
	}
	p := adminConfiguredGacha(base, c).Profile
	if err := gamestate.ValidateGachaRules(p); err != nil {
		return err
	}
	for _, g := range c.Gifts {
		if len(g.Rewards) > 120 {
			return errors.New("每条赠礼最多120项")
		}
		for _, reward := range g.Rewards {
			if err := a.validateCustomGachaReward(reward); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *API) validateCustomGachaReward(r gamestate.Reward) error {
	if err := gamestate.ValidateGachaReward(r); err != nil {
		return err
	}
	if err := a.validateContentReward(r); err != nil {
		return err
	}
	entry, ok := a.catalogByKey[adminCatalogKey(r.Type, r.RewardTypeID)]
	if !ok || entry.ResourceState == "unavailable" {
		return fmt.Errorf("奖励 %d:%d 不存在或资源不可用", r.Type, r.RewardTypeID)
	}
	return nil
}

func (a *API) validateCustomMixedGacha(base gamestate.GachaProfile, c AdminGachaConfig) (map[string]any, error) {
	if len(c.CardIDs) > 0 || len(c.Weights) > 0 || len(c.Steps) > 20 {
		return nil, errors.New("混合池使用奖励列表，最多20个阶段")
	}
	pools := [][]gamestate.WeightedReward{c.RewardPool}
	for _, step := range c.Steps {
		if step.Price < 1 || step.Price > 10000000 {
			return nil, errors.New("阶段价格须为1–10000000")
		}
		pools = append(pools, step.RewardPool)
	}
	for _, pool := range pools {
		if len(pool) > 6000 {
			return nil, errors.New("每阶段奖励最多6000项")
		}
		for _, entry := range pool {
			if entry.Weight < 1 || entry.Weight > 1000000 {
				return nil, errors.New("权重须为1–1000000")
			}
			if err := a.validateCustomGachaReward(entry.Reward); err != nil {
				return nil, err
			}
		}
	}
	// Reuse the established stage preview and first-stage consistency checks,
	// substituting the operator-owned identities as this custom pool's baseline.
	p := adminConfiguredGacha(base, c).Profile
	preview, err := ValidateMixedGachaConfig(a.catalogByKey, p, c)
	if err != nil {
		return nil, err
	}
	preview["warnings"] = []string{"最后阶段重复；阶段和赠礼按此新卡池独立累计抽取次数。修改已上线池的阶段或赠礼不会重置进度。"}
	return preview, nil
}

// Multi-entry groups identify ticket/crystal alternatives by their draw size.
// Derive the lock from the registry, including groups created by older builds.
// Keep the registry's original draw sizes so malformed live overrides do not
// turn a single into a multi before account restoration or display filtering.
func (o *Operations) lockCustomGroupDrawCounts() {
 counts:=map[int]int{}
 for id:=range o.customGachas { counts[o.gachaBases[id].GroupID]++ }
 for id:=range o.customGachas { p:=o.gachaBases[id];p.FixedDrawCount=counts[p.GroupID]>1;o.gachaBases[id]=p }
}
