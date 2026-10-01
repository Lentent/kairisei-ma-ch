package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
)

const collectionDraftKey = "honor-box-drafts"

type editableHonor struct {
	ID           int    `json:"honor_id"`
	Name         string `json:"name"`
	SlotMask     int    `json:"slot_mask"`
	SameCardID   int    `json:"same_card_id"`
	GetType      int    `json:"get_type"`
	DefaultOwned bool   `json:"default_owned"`
}
type editableBox struct {
	Item       gamestate.ItemDefinition   `json:"item"`
	Profile    gamestate.ItemGachaProfile `json:"profile"`
	IconSource int                        `json:"icon_source_id"`
}
type collectionDraft struct {
	Honors []editableHonor `json:"honors"`
	Boxes  []editableBox   `json:"boxes"`
}

func readResourceJSON(root, path string, target any) error {
	b, e := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if e != nil {
		return e
	}
	return json.Unmarshal(b, target)
}
func (a *API) collectionRoot() (string, error) {
	if a.collectionResourceRoot == "" {
		return "", errors.New("当前资源集未提供称号与礼盒编辑入口")
	}
	return a.collectionResourceRoot, nil
}
func (a *API) collectionBaseline() (collectionDraft, error) {
	root, e := a.collectionRoot()
	if e != nil {
		return collectionDraft{}, e
	}
	items, e := masterdata.LoadItemRuntimeMaster(filepath.Join(root, "_local/control/server/cn602-item-runtime-master.json"))
	if e != nil {
		return collectionDraft{}, e
	}
	var h struct {
		Honors []editableHonor `json:"honors"`
	}
	if e = readResourceJSON(root, "_local/control/server/cn602-honor-runtime-master.json", &h); e != nil {
		return collectionDraft{}, e
	}
	d := collectionDraft{Honors: []editableHonor{}, Boxes: []editableBox{}}
	for _, honor := range h.Honors {
		if honor.ID >= 26000000 {
			d.Honors = append(d.Honors, honor)
		}
	}
	for _, profile := range items.ItemGachaProfiles {
		if profile.ItemID != 9999 && profile.ItemID < 90000000 {
			continue
		}
		for _, item := range items.Items {
			if item.ItemID == profile.ItemID {
				d.Boxes = append(d.Boxes, editableBox{item, profile, 8887})
			}
		}
	}
	return d, nil
}
func (a *API) collectionDraft() (collectionDraft, int, error) {
	doc, e := a.operations.storage.ReadDocument(collectionDraftKey)
	if e != nil {
		return collectionDraft{}, 0, e
	}
	d, e := a.collectionBaseline()
	if e != nil {
		return d, 0, e
	}
	if doc.Revision > 0 {
		e = json.Unmarshal(doc.Payload, &d)
	}
	return d, doc.Revision, e
}
func (a *API) collections(w http.ResponseWriter, r *http.Request) {
	a.operations.configMu.RLock()
	defer a.operations.configMu.RUnlock()
	d, revision, e := a.collectionDraft()
	if e != nil {
		WriteAdminError(w, 503, e.Error())
		return
	}
	root, _ := a.collectionRoot()
	var honors struct {
		Honors []editableHonor `json:"honors"`
	}
	_ = readResourceJSON(root, "_local/control/server/cn602-honor-runtime-master.json", &honors)
	itemMaster, e := masterdata.LoadItemRuntimeMaster(filepath.Join(root, "_local/control/server/cn602-item-runtime-master.json"))
	if e != nil {
		WriteAdminError(w, 500, e.Error())
		return
	}
	nextHonor, nextBox, nextFunction := 26093003, 90000001, 70000000
	icons := []map[string]any{}
	for _, item := range itemMaster.Items {
		nextBox = max(nextBox, item.ItemID+1)
		nextFunction = max(nextFunction, item.FunctionValue+1)
		if item.ItemType == "GACHA" && item.PictID > 0 {
			if _, e := os.Stat(filepath.Join(root, fmt.Sprintf("_local/control/server/cn602-admin-assets/item/%d.webp", item.ItemID))); e == nil {
				icons = append(icons, map[string]any{"id": item.ItemID, "name": item.Name, "pict_id": item.PictID})
			}
		}
	}
	for _, h := range honors.Honors {
		nextHonor = max(nextHonor, h.ID+1)
	}
	for _, h := range d.Honors {
		nextHonor = max(nextHonor, h.ID+1)
	}
	for _, b := range d.Boxes {
		nextBox = max(nextBox, b.Item.ItemID+1)
		nextFunction = max(nextFunction, b.Item.FunctionValue+1)
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "revision": revision, "config": d, "next_honor_id": nextHonor, "next_item_id": nextBox, "next_function_value": nextFunction, "icons": icons})
}
func collectionText(s string, max int) bool {
	return strings.TrimSpace(s) != "" && utf8.ValidString(s) && utf8.RuneCountInString(s) <= max && !strings.ContainsAny(s, "\r\n\x00")
}
func (a *API) validateCollections(d *collectionDraft) error {
	if d.Honors == nil || d.Boxes == nil || len(d.Honors) > 200 || len(d.Boxes) > 200 {
		return errors.New("称号与礼盒须为数组，各最多200项")
	}
	root, e := a.collectionRoot()
	if e != nil {
		return e
	}
	items, e := masterdata.LoadItemRuntimeMaster(filepath.Join(root, "_local/control/server/cn602-item-runtime-master.json"))
	if e != nil {
		return e
	}
	baseItems := map[int]gamestate.ItemDefinition{}
	functions := map[int]int{}
	for _, i := range items.Items {
		baseItems[i.ItemID] = i
		if i.Function == "GACHA_EXEC" {
			functions[i.FunctionValue] = i.ItemID
		}
	}
	var hm struct {
		Honors []editableHonor `json:"honors"`
	}
	if e = readResourceJSON(root, "_local/control/server/cn602-honor-runtime-master.json", &hm); e != nil {
		return e
	}
	seen := map[int]bool{}
	for i := range d.Honors {
		h := &d.Honors[i]
		h.Name = strings.TrimSpace(h.Name)
		if h.ID < 26000000 || h.ID > 2147483647 || seen[h.ID] || !collectionText(h.Name, 60) || h.SlotMask < 1 || h.SlotMask > 15 || h.DefaultOwned || h.SameCardID != 0 || h.GetType != 0 {
			return errors.New("自定义称号须使用唯一ID、1–60字名称和有效槽位，不能自动授予")
		}
		seen[h.ID] = true
	}
	boxes := map[int]bool{}
	for i := range d.Boxes {
		b := &d.Boxes[i]
		b.Item.Name = strings.TrimSpace(b.Item.Name)
		item := b.Item
		if item.ItemID < 1 || item.ItemID > 2147483647 || boxes[item.ItemID] || (item.ItemID != 9999 && item.ItemID < 90000000) {
			return errors.New("礼盒ID重复或不属于自定义范围")
		}
		boxes[item.ItemID] = true
		if base, ok := baseItems[item.ItemID]; ok && base.Function != "GACHA_EXEC" {
			return errors.New("不能覆盖已有普通道具")
		}
		if !collectionText(item.Name, 60) || !collectionText(item.Description, 500) || item.ItemType != "GACHA" || item.Function != "GACHA_EXEC" || item.FunctionValue < 1 || item.FunctionValue > 2147483647 || item.MaxOwned < 1 || item.MaxOwned > 99999 || item.DailyLimited != 0 || item.LoveUpPrice != 0 {
			return errors.New("礼盒名称、说明、功能值或持有上限不正确")
		}
		source, ok := baseItems[b.IconSource]
		if !ok || source.ItemType != "GACHA" || source.PictID != item.PictID {
			return errors.New("请复用已有礼盒的图标")
		}
		if _, e = os.Stat(filepath.Join(root, fmt.Sprintf("_local/control/server/cn602-admin-assets/item/%d.webp", b.IconSource))); e != nil {
			return errors.New("所选后台图标缺失")
		}
		if id, ok := functions[item.FunctionValue]; ok && id != item.ItemID {
			return errors.New("礼盒功能值与其他道具冲突")
		}
		functions[item.FunctionValue] = item.ItemID
		if b.Profile.ItemID != item.ItemID || b.Profile.FunctionValue != item.FunctionValue || len(b.Profile.Rewards) != 0 || len(b.Profile.RewardPool) < 1 || len(b.Profile.RewardPool) > 100 {
			return errors.New("礼盒奖池须为1–100项随机奖励")
		}
		for j := range b.Profile.RewardPool {
			r := &b.Profile.RewardPool[j].Reward
			if r.CardSkillLevels == nil {
				r.CardSkillLevels = []int16{}
			}
			if r.Type == 18 && seen[r.RewardTypeID] {
				if r.Num != 1 || r.CardLevel != 0 || r.CardFame != 0 || r.CardLove != 0 || len(r.CardSkillLevels) != 0 {
					return errors.New("称号奖励每次只能发放1个")
				}
			} else {
				canonical, _, e := a.mailReward(AdminMailRequest{RewardType: r.Type, RewardTypeID: r.RewardTypeID, Quantity: r.Num, CardLevel: int(r.CardLevel), CardFame: int(r.CardFame), CardLove: r.CardLove})
				if e != nil {
					return fmt.Errorf("礼盒%d奖励：%w", item.ItemID, e)
				}
				*r = canonical
			}
			if e = gamestate.ValidateItemGachaReward(*r); e != nil {
				return e
			}
		}
		if e = gamestate.ValidateItemGachaRewardPool(b.Profile.RewardPool); e != nil {
			return e
		}
		b.Profile.Evidence = "LOCAL_POLICY_ADMIN_CUSTOM_BOX"
	}
	return nil
}
func (a *API) saveCollections(w http.ResponseWriter, r *http.Request) {
	if e := requireAdminMutation(r); e != nil {
		WriteAdminError(w, 403, e.Error())
		return
	}
	var body struct {
		Expected *int             `json:"expected_revision"`
		Config   *collectionDraft `json:"config"`
	}
	if e := DecodeAdminJSONLimit(r, &body, 2<<20); e != nil || body.Expected == nil || body.Config == nil {
		WriteAdminError(w, 400, "请提交配置与版本")
		return
	}
	a.operations.configMu.Lock()
	e := a.validateCollections(body.Config)
	if e != nil {
		a.operations.configMu.Unlock()
		WriteAdminError(w, 400, e.Error())
		return
	}
	_, e = a.operations.writeDocument(collectionDraftKey, *body.Expected, *body.Config)
	a.operations.configMu.Unlock()
	if e != nil {
		writeContentError(w, e)
		return
	}
	a.collections(w, r)
}
func (a *API) exportCollections(w http.ResponseWriter, r *http.Request) {
	if e := requireAdminMutation(r); e != nil {
		WriteAdminError(w, 403, e.Error())
		return
	}
	var body struct {
		Expected *int `json:"expected_revision"`
	}
	if e := decodeAdminJSON(r, &body); e != nil || body.Expected == nil {
		WriteAdminError(w, 400, "请提交已保存配置的版本")
		return
	}
	a.operations.configMu.Lock()
	d, revision, e := a.collectionDraft()
	if e == nil && revision != *body.Expected {
		e = errors.New("配置已变化，请重新载入")
	}
	if e == nil {
		e = a.validateCollections(&d)
	}
	var patch []byte
	if e == nil {
		patch, e = a.buildCollectionPatch(d)
	}
	a.operations.configMu.Unlock()
	if e != nil {
		WriteAdminError(w, 400, e.Error())
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=honor-box-resources-v%d.zip", revision))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(patch)
}
