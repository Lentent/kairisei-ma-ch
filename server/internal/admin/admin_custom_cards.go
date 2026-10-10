package admin

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/protocol"
)

const customCardDraftKey = "custom-card-drafts"
const customCardFirstID = 98000001
const customCardLastID = 98999999
const customSkillFirstID = 1900000000

// CN SkillRoleCsvData.roles and SkillCsvData.extends are fixed arrays of five
// entries (Const.SKILL_ROLE_MAX / SKILL_EXTEND_MAX), including in the 6.0.8 APK.
const customClientSkillGroupLimit = 5

// Skills and roles retain their template identities in the draft. Export gives
// every referenced function its own ID, including branch functions shared by
// several variants. Only names, descriptions and VALUE parameters are editable.
type customCard struct {
	ID              int                      `json:"card_id"`
	TemplateID      int                      `json:"template_card_id"`
	Name            string                   `json:"name"`
	Prefix          string                   `json:"prefix"`
	Cost            int                      `json:"cost"`
	ArthurType      int8                     `json:"arthur_type"`
	Attribute       string                   `json:"attribute"`
	Initial         gamestate.CardParameter  `json:"initial"`
	Maximum         gamestate.CardParameter  `json:"maximum"`
	LoveBonus       gamestate.CardParameter  `json:"love_bonus"`
	Skills          [][]string               `json:"skills"`
	Roles           [][]string               `json:"roles"`
	RoleSources     []customRoleSource       `json:"role_sources"`
	Artwork         []byte                   `json:"artwork,omitempty"`
	IconArtwork     []byte                   `json:"icon_artwork,omitempty"`
	CutinTemplateID int                      `json:"cutin_template_card_id,omitempty"`
	ActionSources   []customCardActionSource `json:"action_sources,omitempty"`
}
type customCardActionSource struct {
	FunctionID       int `json:"function_id"`
	CardID           int `json:"card_id"`
	SourceFunctionID int `json:"source_function_id"`
}

func (c customCard) hasCustomArtwork() bool {
	return len(c.Artwork) > 0 || len(c.IconArtwork) > 0
}

type customRoleSource struct {
	CardID int `json:"card_id"`
	Index  int `json:"index"`
}
type customCardDraft struct {
	Cards []customCard `json:"cards"`
}
type customCardReceipt struct {
	ID         int  `json:"card_id"`
	TemplateID int  `json:"template_card_id"`
	Artwork    bool `json:"artwork,omitempty"`
}
type customCardSources struct {
	Master        masterdata.CardRuntimeMaster
	Cards         map[int][]string
	Skills, Roles [][]string
	Rules         map[string][]string
	Applied       map[int]int
}

const customCardMasterPath = "_local/control/server/cn602-card-runtime-master.json"
const customCardCSVPath = "_local/control/server/cn602-card-master/card.csv"
const customSkillCSVPath = "_local/control/server/cn602-battle-master/skill_player.csv"
const customRoleCSVPath = "_local/control/server/cn602-battle-master/skill_role_player.csv"

func customCSVRows(raw []byte) ([][]string, error) {
	s := bufio.NewScanner(strings.NewReader(strings.TrimPrefix(string(raw), "\ufeff")))
	s.Buffer(make([]byte, 4096), 4<<20)
	rows := [][]string{}
	for s.Scan() {
		line := s.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rows = append(rows, protocol.SplitCSVLine(line))
	}
	return rows, s.Err()
}
func customRowInt(row []string, i int) int {
	if i >= len(row) {
		return 0
	}
	n, _ := strconv.Atoi(row[i])
	return n
}
func cloneCustomRows(rows [][]string) [][]string {
	next := make([][]string, len(rows))
	for i, row := range rows {
		next[i] = append([]string(nil), row...)
	}
	return next
}
func (a *API) customCardSources() (customCardSources, error) {
	s := customCardSources{Cards: map[int][]string{}, Rules: map[string][]string{}, Applied: map[int]int{}}
	root := a.collectionResourceRoot
	if root == "" {
		return s, errors.New("当前资源集未提供自制卡牌编辑入口")
	}
	var err error
	s.Master, err = masterdata.LoadCardRuntimeMaster(filepath.Join(root, filepath.FromSlash(customCardMasterPath)))
	if err != nil {
		return s, err
	}
	read := func(path string) ([][]string, error) {
		b, e := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if e != nil {
			return nil, e
		}
		return customCSVRows(b)
	}
	rows, err := read(customCardCSVPath)
	if err != nil {
		return s, err
	}
	for _, row := range rows {
		id := customRowInt(row, 0)
		if id <= 0 {
			continue
		}
		if _, ok := s.Cards[id]; ok {
			return s, errors.New("卡牌表ID重复")
		}
		s.Cards[id] = row
	}
	if s.Skills, err = read(customSkillCSVPath); err != nil {
		return s, err
	}
	if s.Roles, err = read(customRoleCSVPath); err != nil {
		return s, err
	}
	rules, err := read("_local/control/server/cn602-battle-master/skill_role_param_rule.csv")
	if err != nil {
		return s, err
	}
	for _, row := range rules {
		if len(row) >= 13 {
			s.Rules[row[0]] = row[3:13]
		}
	}
	var source struct {
		CustomCards []customCardReceipt `json:"admin_custom_cards"`
	}
	if err = json.Unmarshal(s.Master.Source, &source); err != nil {
		return s, err
	}
	for _, receipt := range source.CustomCards {
		if receipt.ID < customCardFirstID || receipt.ID > customCardLastID || s.Applied[receipt.ID] != 0 {
			return s, errors.New("自制卡牌资源记录无效")
		}
		s.Applied[receipt.ID] = receipt.TemplateID
	}
	return s, nil
}
func (s customCardSources) template(id int) (customCard, error) {
	row, ok := s.Cards[id]
	if !ok || len(row) < 62 || id >= customCardFirstID && id <= customCardLastID {
		return customCard{}, errors.New("请选择一张资源完整的现有普通卡牌作为模板")
	}
	var base *gamestate.Card
	for i := range s.Master.CardTemplates {
		if s.Master.CardTemplates[i].CardID == id {
			base = &s.Master.CardTemplates[i]
			break
		}
	}
	if base == nil {
		return customCard{}, errors.New("模板不在普通卡牌目录中")
	}
	card := customCard{TemplateID: id, Name: base.Name, Prefix: row[4], Cost: customRowInt(row, 9), ArthurType: s.Master.DeckRankPolicy.Cards[id].ArthurType, Initial: base.ParameterInitial, Maximum: base.ParameterMaximum, LoveBonus: base.ParameterLoveMaximumBonus, Skills: [][]string{}, Roles: [][]string{}}
	roots := map[int]bool{customRowInt(row, 26): true, customRowInt(row, 27): true}
	delete(roots, 0)
	functions := map[int]bool{}
	for _, skill := range s.Skills {
		if roots[customRowInt(skill, 0)] {
			if len(skill) < 50 {
				return card, errors.New("模板技能行不完整")
			}
			card.Skills = append(card.Skills, append([]string(nil), skill...))
			fn := customRowInt(skill, 49)
			if fn == 0 {
				fn = customRowInt(skill, 0)
			}
			functions[fn] = true
			if card.Attribute == "" {
				card.Attribute = skill[11]
				card.Cost = customRowInt(skill, 14)
			}
		}
	}
	for root := range roots {
		found := false
		for _, skill := range card.Skills {
			found = found || customRowInt(skill, 0) == root
		}
		if !found {
			return card, errors.New("模板技能定义缺失")
		}
	}
	for _, role := range s.Roles {
		if functions[customRowInt(role, 0)] {
			if len(role) < 32 || len(s.Rules[role[8]]) != 10 {
				return card, errors.New("模板技能参数定义缺失")
			}
			card.Roles = append(card.Roles, append([]string(nil), role...))
		}
	}
	for fn := range functions {
		found := false
		for _, role := range card.Roles {
			found = found || customRowInt(role, 0) == fn
		}
		if !found {
			return card, errors.New("模板技能效果缺失")
		}
	}
	card.RoleSources = make([]customRoleSource, len(card.Roles))
	for i := range card.Roles {
		card.RoleSources[i] = customRoleSource{id, i}
	}
	if len(card.Skills) > 40 || len(card.Roles) > 120 {
		return card, errors.New("模板技能过于复杂，请选择其他卡牌")
	}
	if e := validateCustomClientSkillCapacity(card); e != nil {
		return card, e
	}
	return card, nil
}

func validateCustomClientSkillCapacity(c customCard) error {
	for _, group := range []struct {
		rows [][]string
		name string
	}{{c.Skills, "技能分支"}, {c.Roles, "效果"}} {
		counts := map[int]int{}
		for _, row := range group.rows {
			id := customRowInt(row, 0)
			counts[id]++
			if counts[id] > customClientSkillGroupLimit {
				return fmt.Errorf("卡牌%d的组%d最多支持%d项%s，请减少该组数量后再导出", c.ID, id, customClientSkillGroupLimit, group.name)
			}
		}
	}
	return nil
}
func (a *API) customCardsDraft(s customCardSources) (customCardDraft, int, error) {
	d := customCardDraft{Cards: []customCard{}}
	doc, err := a.operations.storage.ReadDocument(customCardDraftKey)
	if err != nil {
		return d, 0, err
	}
	if doc.Revision > 0 {
		err = json.Unmarshal(doc.Payload, &d)
	}
	return d, doc.Revision, err
}
func customCardNextID(s customCardSources, d customCardDraft) int {
	used := map[int]bool{}
	for id := range s.Cards {
		used[id] = true
	}
	for _, c := range d.Cards {
		used[c.ID] = true
	}
	for id := customCardFirstID; id <= customCardLastID; id++ {
		if !used[id] {
			return id
		}
	}
	return 0
}
func customCardOccupiedIDs(s customCardSources) []int {
	ids := []int{}
	for id := range s.Cards {
		if id >= customCardFirstID && id <= customCardLastID {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	return ids
}
func (a *API) customCards(w http.ResponseWriter, r *http.Request) {
	a.operations.configMu.RLock()
	defer a.operations.configMu.RUnlock()
	s, e := a.customCardSources()
	if e != nil {
		WriteAdminError(w, 503, e.Error())
		return
	}
	d, revision, e := a.customCardsDraft(s)
	if e != nil {
		WriteAdminError(w, 503, e.Error())
		return
	}
	names := map[int]string{}
	for _, c := range d.Cards {
		for _, id := range append([]int{c.CutinTemplateID}, customActionSourceCardIDs(c)...) {
			if row := s.Cards[id]; len(row) > 5 {
				names[id] = row[5]
			}
		}
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "config": d, "revision": revision, "next_card_id": customCardNextID(s, d), "occupied_card_ids": customCardOccupiedIDs(s), "applied": s.Applied, "parameter_rules": s.Rules, "effect_help": customCardEffectHelp(s.Rules), "target_labels": customCardTargetLabels(), "value_labels": customCardValueLabels(), "presentation_names": names})
}
func (a *API) customCardTemplate(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.Atoi(chi.URLParam(r, "id"))
	if e != nil {
		WriteAdminError(w, 400, "卡牌ID无效")
		return
	}
	s, e := a.customCardSources()
	if e != nil {
		WriteAdminError(w, 503, e.Error())
		return
	}
	c, e := s.template(id)
	if e != nil {
		WriteAdminError(w, 400, e.Error())
		return
	}
	if entry, ok := a.catalogByKey[adminCatalogKey(6, id)]; !ok || entry.ResourceState == "unavailable" {
		WriteAdminError(w, 400, "模板客户端资源不完整")
		return
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "card": c, "parameter_rules": s.Rules, "effect_help": customCardEffectHelp(s.Rules), "target_labels": customCardTargetLabels(), "value_labels": customCardValueLabels()})
}
func customParameterValues(p gamestate.CardParameter) [4]int {
	return [4]int{p.HP, p.Attack, p.Magic, p.Mind}
}
func (a *API) validateCustomCards(d *customCardDraft, s customCardSources) error {
	if d.Cards == nil || len(d.Cards) > 200 {
		return errors.New("自制卡牌须为数组，最多200张")
	}
	seen := map[int]bool{}
	templates := map[int]customCard{}
	template := func(id int) (customCard, error) {
		if c, ok := templates[id]; ok {
			return c, nil
		}
		c, e := s.template(id)
		if e == nil {
			templates[id] = c
		}
		return c, e
	}
	usedSkills := map[int]bool{}
	for _, rows := range [][][]string{s.Skills, s.Roles} {
		for _, row := range rows {
			usedSkills[customRowInt(row, 0)] = true
		}
	}
	artworkBytes := 0
	for i := range d.Cards {
		c := &d.Cards[i]
		c.Name = strings.TrimSpace(c.Name)
		c.Prefix = strings.TrimSpace(c.Prefix)
		if c.ID < customCardFirstID || c.ID > customCardLastID || seen[c.ID] {
			return errors.New("自制卡牌ID须在98000001–98999999内且不能重复")
		}
		seen[c.ID] = true
		if _, exists := s.Cards[c.ID]; exists && s.Applied[c.ID] != c.TemplateID {
			return errors.New("不能覆盖未由本编辑器生成的卡牌")
		}
		base, e := template(c.TemplateID)
		if e != nil {
			return e
		}
		if entry, ok := a.catalogByKey[adminCatalogKey(6, c.TemplateID)]; !ok || entry.ResourceState == "unavailable" {
			return errors.New("模板客户端资源不完整")
		}
		if !collectionText(c.Name, 40) || c.Prefix != "" && !collectionText(c.Prefix, 30) || c.Cost < 1 || c.Cost > 10 || c.ArthurType < 0 || c.ArthurType > 4 {
			return errors.New("名称、冠名、职业或COST无效（COST为1–10）")
		}
		if !map[string]bool{"FIRE": true, "ICE": true, "WIND": true, "LIGHT": true, "DARK": true}[c.Attribute] {
			return errors.New("请选择火、冰、风、光或暗属性")
		}
		initial, maximum, bonus := customParameterValues(c.Initial), customParameterValues(c.Maximum), customParameterValues(c.LoveBonus)
		for j := range initial {
			if initial[j] < 0 || maximum[j] < initial[j] || maximum[j] > 10000000 || bonus[j] < 0 || bonus[j] > 10000000 {
				return errors.New("四维须为0–10000000，满级值不能小于初始值")
			}
		}
		if len(c.Skills) != len(base.Skills) || len(c.Roles) < 1 || len(c.Roles) > 120 || len(c.RoleSources) != len(c.Roles) {
			return errors.New("请保留模板技能分支，并配置1–120项效果和来源")
		}
		for j, row := range c.Skills {
			original := base.Skills[j]
			if len(row) != len(original) {
				return errors.New("技能行长度无效")
			}
			for k, v := range row {
				if k == 1 || k == 3 {
					if v != original[k] && !collectionText(v, map[int]int{1: 60, 3: 500}[k]) {
						return errors.New("请填写有效技能名称和说明")
					}
					continue
				}
				if v != original[k] {
					return errors.New("技能条件和演出须沿用模板")
				}
			}
		}
		functions := map[int]bool{}
		for _, row := range base.Roles {
			functions[customRowInt(row, 0)] = false
		}
		for j, row := range c.Roles {
			source := c.RoleSources[j]
			effectBase := base
			if source.CardID != c.TemplateID {
				var e error
				effectBase, e = template(source.CardID)
				if e != nil {
					return e
				}
				if entry, ok := a.catalogByKey[adminCatalogKey(6, source.CardID)]; !ok || entry.ResourceState == "unavailable" {
					return errors.New("效果来源卡牌资源不完整")
				}
			}
			if source.Index < 0 || source.Index >= len(effectBase.Roles) {
				return errors.New("效果来源索引无效")
			}
			original := effectBase.Roles[source.Index]
			if len(row) != len(original) {
				return errors.New("技能效果行长度无效")
			}
			fn := customRowInt(row, 0)
			if _, ok := functions[fn]; !ok {
				return errors.New("效果须分配到模板已有的技能分支")
			}
			functions[fn] = true
			for k, v := range row {
				if k == 0 {
					continue
				}
				if k >= 20 && k < 30 && s.Rules[original[8]][k-20] == "VALUE" {
					if v == original[k] {
						continue
					}
					n, e := strconv.ParseInt(v, 10, 32)
					if e != nil || n < -100000000 || n > 100000000 {
						return errors.New("技能数值须为-100000000–100000000的整数")
					}
					continue
				}
				if v != original[k] {
					return errors.New("技能类型、目标和枚举参数须沿用模板")
				}
			}
		}
		for _, hasRole := range functions {
			if !hasRole {
				return errors.New("每个技能分支至少保留一项效果")
			}
		}
		if e := validateCustomClientSkillCapacity(*c); e != nil {
			return e
		}
		if _, _, e := customCardPresentation(*c, s, func(id int) bool {
			entry, ok := a.catalogByKey[adminCatalogKey(6, id)]
			return ok && entry.ResourceState != "unavailable"
		}); e != nil {
			return e
		}
		for _, upload := range []struct {
			name string
			data []byte
		}{{"立绘", c.Artwork}, {"卡面小图", c.IconArtwork}} {
			if len(upload.data) > 0 {
				if _, e := decodeCustomArtwork(upload.data); e != nil {
					return fmt.Errorf("%s：%w", upload.name, e)
				}
				artworkBytes += len(upload.data)
				if artworkBytes > 16<<20 {
					return errors.New("全部小图和立绘总大小须小于16MB，请压缩图片或分批制作")
				}
			}
		}
		ids := customSkillIDs(*c)
		if len(ids) > 64 {
			return errors.New("模板技能引用超过上限")
		}
		for _, id := range ids {
			if usedSkills[id] && s.Applied[c.ID] == 0 {
				return errors.New("生成的技能或效果ID与当前资源冲突")
			}
		}
	}
	for id := range s.Applied {
		if !seen[id] {
			return fmt.Errorf("已发布的卡牌%d须保留在草稿中，避免影响玩家库存", id)
		}
	}
	return nil
}
func customSkillIDs(c customCard) map[int]int {
	ids := map[int]bool{}
	for _, r := range c.Skills {
		ids[customRowInt(r, 0)] = true
		fn := customRowInt(r, 49)
		if fn > 0 {
			ids[fn] = true
		}
	}
	for _, r := range c.Roles {
		ids[customRowInt(r, 0)] = true
	}
	source := make([]int, 0, len(ids))
	for id := range ids {
		source = append(source, id)
	}
	sort.Ints(source)
	result := map[int]int{}
	for i, id := range source {
		result[id] = customSkillFirstID + (c.ID-customCardFirstID)*64 + i
	}
	return result
}
func (a *API) saveCustomCards(w http.ResponseWriter, r *http.Request) {
	if e := requireAdminMutation(r); e != nil {
		WriteAdminError(w, 403, e.Error())
		return
	}
	var body struct {
		Expected *int             `json:"expected_revision"`
		Config   *customCardDraft `json:"config"`
	}
	if e := DecodeAdminJSONLimit(r, &body, 32<<20); e != nil || body.Expected == nil || body.Config == nil {
		WriteAdminError(w, 400, "请提交配置与版本（含卡面最多32MB）")
		return
	}
	a.operations.configMu.Lock()
	s, e := a.customCardSources()
	if e == nil {
		e = a.validateCustomCards(body.Config, s)
	}
	if e != nil {
		a.operations.configMu.Unlock()
		WriteAdminError(w, 400, e.Error())
		return
	}
	_, e = a.operations.writeDocument(customCardDraftKey, *body.Expected, *body.Config)
	a.operations.configMu.Unlock()
	if e != nil {
		writeContentError(w, e)
		return
	}
	a.customCards(w, r)
}
func (a *API) exportCustomCards(w http.ResponseWriter, r *http.Request) {
	if e := requireAdminMutation(r); e != nil {
		WriteAdminError(w, 403, e.Error())
		return
	}
	var body struct {
		Expected *int `json:"expected_revision"`
	}
	if e := decodeAdminJSON(r, &body); e != nil || body.Expected == nil {
		WriteAdminError(w, 400, "请提交已保存草稿的版本")
		return
	}
	a.operations.configMu.Lock()
	s, e := a.customCardSources()
	var d customCardDraft
	revision := 0
	if e == nil {
		d, revision, e = a.customCardsDraft(s)
	}
	if e == nil && revision != *body.Expected {
		a.operations.configMu.Unlock()
		WriteAdminError(w, 409, "草稿已变化，请重新载入")
		return
	}
	if e == nil {
		e = a.validateCustomCards(&d, s)
	}
	if e == nil && len(d.Cards) == 0 {
		e = errors.New("请先新增并保存自制卡牌")
	}
	var patch []byte
	if e == nil {
		patch, e = a.buildCustomCardPatch(d, s)
	}
	a.operations.configMu.Unlock()
	if e != nil {
		WriteAdminError(w, 400, e.Error())
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=custom-cards-v%d.zip", revision))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(patch)
}
