package admin

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // cover uploads accept JPEG
	_ "image/png"  // and PNG, the formats the client texture loader reads
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/gamestate"
)

// Operator-created pools (LOCAL_POLICY, user request 2026-09-27). A new pool copies every draw variant of
// an existing card-pool group (ticket, single, ten-draw …) into a new publication group; each variant keeps
// its template's payment and draw rules. Deletion is soft and covers the whole group, so play counts, audit
// history and identities survive and the pool can be restored.
const (
	customGachaKey     = "gacha-custom"
	customGachaFirstID = gamestate.OperatorGachaFirstID
	gachaCoverFolder   = "gacha-covers"
	gachaCoverMaxBytes = 2 * 1024 * 1024
)

var gachaCoverPathPattern = regexp.MustCompile(`^gacha-covers/[0-9a-f]{64}\.(png|jpg)$`)

// A 4096x4096 PNG can expand into a large pixel buffer. Validate one stream at a
// time; compressed request size alone does not bound concurrent decode memory.
var gachaCoverDecodeSlot = make(chan struct{}, 1)

type customGachaPool struct {
	GachaID       int                     `json:"gacha_id"`
	GroupID       int                     `json:"group_id"`
	TemplateID    int                     `json:"template_id"`
	Name          string                  `json:"name"`
	Deleted       bool                    `json:"deleted,omitempty"`
	LegacyProfile *gamestate.GachaProfile `json:"legacy_profile,omitempty"`
	RuleVersion   int                     `json:"rule_version,omitempty"` // 0: legacy copy; 1: built-in rule template; 2: full custom snapshot.
	ArthurType    int8                    `json:"arthur_type,omitempty"`  // Native profession picker: 1–4 professions, 5 mixed.
}

type customGachaDocument struct {
	Pools  []customGachaPool `json:"pools"`
	NextID int               `json:"next_id,omitempty"`
}

// customGachaBase derives fixed rules from the recorded template. Legacy copies
// omit promotions; version 1 deliberately keeps the complete selected rules.
func customGachaBase(template gamestate.GachaProfile, pool customGachaPool) gamestate.GachaProfile {
	base := gamestate.CloneGachas([]gamestate.GachaProfile{template})[0]
	base.GachaID, base.GroupID = pool.GachaID, pool.GroupID
	if pool.ArthurType != 0 {
		base.ArthurType = pool.ArthurType
	}
	if pool.Name != "" {
		base.Name = pool.Name
	}
	base.PublicationKey = "operator"
	if pool.RuleVersion == 0 {
		base.DailyFirstFree, base.DailyFirstFreeSourceState = false, ""
		base.Gifts = nil
	}
	base.PlayCount = 0
	base.PoolSourceState, base.WeightSourceState = "LOCAL_POLICY", "LOCAL_POLICY"
	return base
}

func customGachaTemplateAllowed(template gamestate.GachaProfile) bool {
	return len(template.RewardPool) == 0 && len(template.Steps) == 0 && template.GroupID > 0
}

// loadCustomGachas registers stored operator pools before configurations are built. Called once at start.
func (operations *Operations) loadCustomGachas() error {
	doc, err := operations.storage.ReadDocument(customGachaKey)
	if err != nil {
		return err
	}
	operations.customGachaRevision = doc.Revision
	operations.customGachas = map[int]customGachaPool{}
	if doc.Revision == 0 {
		return nil
	}
	var stored customGachaDocument
	if err := json.Unmarshal(doc.Payload, &stored); err != nil {
		return fmt.Errorf("decode operator gacha pools: %w", err)
	}
	if stored.NextID < 0 {
		return errors.New("invalid operator gacha ID cursor")
	}
	operations.customGachaNextID = stored.NextID
	for _, pool := range stored.Pools {
		if err := operations.registerCustomGacha(pool); err != nil {
			return err
		}
	}
	return nil
}

func (operations *Operations) registerCustomGacha(pool customGachaPool) error {
	if pool.RuleVersion == 2 {
		if pool.LegacyProfile == nil || pool.GachaID < 70000000 || pool.GachaID >= 80000000 || pool.GroupID < 70000000 || pool.GroupID >= 80000000 || pool.LegacyProfile.GachaID != pool.GachaID || pool.LegacyProfile.GroupID != pool.GroupID || pool.LegacyProfile.PublicationKey != "custom" {
			return errors.New("invalid legacy custom gacha identity")
		}
		if _, taken := operations.gachaBases[pool.GachaID]; taken {
			return errors.New("duplicate legacy custom gacha identity")
		}
		operations.gachaBases[pool.GachaID] = gamestate.CloneGachas([]gamestate.GachaProfile{*pool.LegacyProfile})[0]
		operations.customGachas[pool.GachaID] = pool
		operations.managedGachaGroups[pool.GroupID] = struct{}{}
		operations.managedGachaGroupByID[pool.GachaID] = pool.GroupID
		if operations.legacyCustomGachas == nil {
			operations.legacyCustomGachas = map[int]bool{}
		}
		operations.legacyCustomGachas[pool.GachaID] = true
		return nil
	}
	template, exists := operations.gachaBases[pool.TemplateID]
	allowed := customGachaTemplateAllowed(template)
	if pool.RuleVersion == 1 {
		allowed = ruleTemplateAllowed(template)
	}
	if !exists || !allowed || pool.RuleVersion < 0 || pool.RuleVersion > 1 || pool.ArthurType < 0 || pool.ArthurType > 5 {
		return fmt.Errorf("operator gacha %d references unavailable template %d", pool.GachaID, pool.TemplateID)
	}
	if _, taken := operations.gachaBases[pool.GachaID]; taken {
		return fmt.Errorf("operator gacha %d collides with an existing pool", pool.GachaID)
	}
	operations.gachaBases[pool.GachaID] = operations.filterGachaProfession(customGachaBase(template, pool))
	operations.customGachas[pool.GachaID] = pool
	operations.managedGachaGroups[pool.GroupID] = struct{}{}
	operations.managedGachaGroupByID[pool.GachaID] = pool.GroupID
	return nil
}

func (operations *Operations) customGachaDocument() customGachaDocument {
	doc := customGachaDocument{Pools: make([]customGachaPool, 0, len(operations.customGachas)), NextID: operations.nextCustomGachaID()}
	for _, pool := range operations.customGachas {
		doc.Pools = append(doc.Pools, pool)
	}
	slices.SortFunc(doc.Pools, func(a, b customGachaPool) int { return a.GachaID - b.GachaID })
	return doc
}

// nextCustomGachaID allocates one ID used for both the pool and its publication group.
func (operations *Operations) nextCustomGachaID() int {
	next := max(customGachaFirstID, operations.customGachaNextID)
	for id := range operations.gachaBases {
		if id >= next {
			next = id + 1
		}
	}
	for group := range operations.managedGachaGroups {
		if group >= next {
			next = group + 1
		}
	}
	return next
}

// effectiveGachaConfig is what players currently get: the published document, else the base rules.
func (operations *Operations) effectiveGachaConfig(id int) (AdminGachaConfig, error) {
	config := AdminGachaConfigFromProfile(operations.gachaBases[id])
	live, err := operations.storage.ReadDocument("gacha-live:" + strconv.Itoa(id))
	if err != nil || live.Revision == 0 {
		return config, err
	}
	err = json.Unmarshal(live.Payload, &config)
	return config, err
}

func (admin *API) createCustomGacha(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		SourceID       int    `json:"source_id"`
		RuleTemplateID int    `json:"rule_template_id"`
		Name           string `json:"name"`
		Expected       *int   `json:"expected_revision"`
		Professions    []int8 `json:"professions"`
	}
	if err := decodeAdminJSON(r, &body); err != nil || body.Expected == nil {
		WriteAdminError(w, 400, "请选择规则模板或复制来源、填写卡池名称并携带配置版本")
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" || len([]rune(name)) > 60 {
		WriteAdminError(w, 400, "卡池名称须为 1–60 字")
		return
	}
	o := admin.operations
	o.configMu.Lock()
	defer o.configMu.Unlock()
	if *body.Expected != o.customGachaRevision {
		WriteAdminError(w, 409, accountstore.ErrDocumentConflict.Error())
		return
	}
	members := make([]int, 0, 4)
	ruleVersion := 0
	if body.RuleTemplateID != 0 {
		if body.SourceID != 0 {
			WriteAdminError(w, 400, "规则模板和复制来源只能选择一种")
			return
		}
		members = o.ruleTemplateMembers(body.RuleTemplateID)
		if len(members) == 0 {
			WriteAdminError(w, 400, "规则模板不可用，请重新载入后选择")
			return
		}
		ruleVersion = 1
	} else {
		source, exists := o.gachaBases[body.SourceID]
		if copied, isCustom := o.customGachas[body.SourceID]; !exists || (isCustom && copied.Deleted) {
			WriteAdminError(w, 400, "复制来源不存在或已删除")
			return
		}
		for id, base := range o.gachaBases {
			if base.GroupID != source.GroupID {
				continue
			}
			if !customGachaTemplateAllowed(base) || base.UnownedOnly {
				WriteAdminError(w, 400, "只能复制普通卡牌卡池（不含混合奖励、阶段或必得未入手池）")
				return
			}
			members = append(members, id)
		}
	}
	slices.Sort(members)
	if len(body.Professions) > 0 {
		if !o.professionChoicesAllowed(members) {
			WriteAdminError(w, 400, "此模板不能使用职业选择：需要普通固定抽数卡池，且每种抽数只能有一种消耗方式")
			return
		}
		slices.Sort(body.Professions)
		for i, profession := range body.Professions {
			if profession < 1 || profession > 5 || (i > 0 && profession == body.Professions[i-1]) {
				WriteAdminError(w, 400, "职业选项须为不重复的佣兵、富豪、盗贼、歌姬或混合")
				return
			}
		}
	}
	type variant struct {
		source     int
		profession int8
	}
	variants := []variant{}
	for _, id := range members {
		if len(body.Professions) == 0 {
			variants = append(variants, variant{source: id})
		}
		for _, profession := range body.Professions {
			variants = append(variants, variant{source: id, profession: profession})
		}
	}
	first := o.nextCustomGachaID()
	pools := make([]customGachaPool, len(variants))
	contents := make([]AdminGachaConfig, len(variants))
	undo := func() {
		for _, pool := range pools {
			if pool.GachaID != 0 {
				delete(o.gachaBases, pool.GachaID)
				delete(o.customGachas, pool.GachaID)
				delete(o.legacyCustomGachas, pool.GachaID)
				delete(o.managedGachaGroupByID, pool.GachaID)
			}
		}
		delete(o.managedGachaGroups, first)
	}
	for i, variant := range variants {
		sourceID := variant.source
		templateID := sourceID
		if copied, isCustom := o.customGachas[sourceID]; isCustom {
			templateID = copied.TemplateID
		}
		content := AdminGachaConfigFromProfile(o.gachaBases[sourceID])
		if ruleVersion == 0 {
			var err error
			content, err = o.effectiveGachaConfig(sourceID)
			if err != nil {
				undo()
				WriteAdminError(w, 500, err.Error())
				return
			}
		}
		pool := customGachaPool{GachaID: first + i, GroupID: first, TemplateID: templateID, Name: name, RuleVersion: ruleVersion, ArthurType: variant.profession}
		if source, ok := o.customGachas[sourceID]; ok && pool.ArthurType == 0 {
			pool.ArthurType = source.ArthurType
		}
		if old, ok := o.customGachas[sourceID]; ok && old.LegacyProfile != nil {
			profile := adminConfiguredGacha(o.gachaBases[sourceID], content).Profile
			profile.GachaID, profile.GroupID, profile.Name, profile.PlayCount = pool.GachaID, pool.GroupID, name, 0
			if pool.ArthurType != 0 {
				profile.ArthurType = pool.ArthurType
			}
			pool.RuleVersion = 2
			pool.LegacyProfile = &profile
		}
		if err := o.registerCustomGacha(pool); err != nil {
			undo()
			WriteAdminError(w, 400, err.Error())
			return
		}
		pools[i] = pool
		content.GachaID, content.Name = pool.GachaID, name
		filtered := o.filterGachaProfession(adminConfiguredGacha(o.gachaBases[pool.GachaID], content).Profile)
		content.CardIDs, content.Weights, content.CardFames = filtered.CardIDs, filtered.CardWeights, filtered.CardFames
		if i > 0 {
			lead := contents[0]
			content.CoverPath, content.StartUnix, content.EndUnix = lead.CoverPath, lead.StartUnix, lead.EndUnix
		}
		if _, err := admin.validateGachaConfig(content); err != nil {
			undo()
			WriteAdminError(w, 400, err.Error())
			return
		}
		contents[i] = content
	}
	nextConfigs, _, err := o.readGachaConfigurations()
	if err != nil {
		undo()
		WriteAdminError(w, 500, err.Error())
		return
	}
	writes := []accountstore.DocumentWrite{{Key: customGachaKey, Expected: o.customGachaRevision, Value: o.customGachaDocument(), Operation: customGachaKey}}
	for i, pool := range pools {
		writes = append(writes, accountstore.DocumentWrite{Key: "gacha-draft:" + strconv.Itoa(pool.GachaID), Value: contents[i], Operation: "gacha-draft"})
	}
	documents, err := o.storage.WriteDocuments(writes)
	if err != nil {
		undo()
		writeContentError(w, err)
		return
	}
	o.customGachaRevision = documents[0].Revision
	o.gachaConfigurations = nextConfigs
	o.syncCustomGachaCatalog()
	o.gachaRevision++
	// New content starts as drafts: publish the whole group, then open it in
	// 扭蛋发布. Until then players cannot see it.
	ids := make([]int, len(pools))
	for i, pool := range pools {
		ids[i] = pool.GachaID
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "gacha_ids": ids, "group_id": first, "revision": documents[0].Revision})
}

func (admin *API) changeCustomGachaDeletion(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		Expected *int `json:"expected_revision"`
	}
	id, err := strconv.Atoi(chi.URLParam(r, "gachaID"))
	if err != nil || decodeAdminJSON(r, &body) != nil || body.Expected == nil {
		WriteAdminError(w, 400, "请携带卡池与配置版本")
		return
	}
	deleted := chi.URLParam(r, "action") == "delete"
	o := admin.operations
	o.configMu.Lock()
	defer o.configMu.Unlock()
	pool, exists := o.customGachas[id]
	if !exists {
		WriteAdminError(w, 400, "只能删除或恢复后台新建的卡池；内置卡池请在「扭蛋发布」关闭")
		return
	}
	if *body.Expected != o.customGachaRevision || pool.Deleted == deleted {
		WriteAdminError(w, 409, accountstore.ErrDocumentConflict.Error())
		return
	}
	setGroup := func(value bool) {
		for memberID, member := range o.customGachas {
			if member.GroupID == pool.GroupID {
				member.Deleted = value
				o.customGachas[memberID] = member
			}
		}
	}
	setGroup(deleted)
	nextConfigs, _, err := o.readGachaConfigurations()
	if err != nil {
		setGroup(!deleted)
		WriteAdminError(w, 500, err.Error())
		return
	}
	doc, err := o.writeDocument(customGachaKey, o.customGachaRevision, o.customGachaDocument())
	if err != nil {
		setGroup(!deleted)
		writeContentError(w, err)
		return
	}
	o.customGachaRevision = doc.Revision
	o.gachaConfigurations = nextConfigs
	o.syncCustomGachaCatalog()
	o.gachaRevision++
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "revision": doc.Revision})
}

// uploadGachaCover stores a PNG/JPEG under the covers folder named by its SHA-256, so files are immutable
// and can be mirrored to a storage container unchanged. The pool references it on its next draft.
func (admin *API) uploadGachaCover(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	if admin.gachaCoverDir == "" {
		WriteAdminError(w, 503, "服务器未配置封面目录")
		return
	}
	var body struct {
		Data string `json:"data_base64"`
	}
	if err := DecodeAdminJSONLimit(r, &body, gachaCoverMaxBytes*4/3+1024); err != nil {
		WriteAdminError(w, 400, "封面文件最大 2 MiB")
		return
	}
	data, err := base64.StdEncoding.DecodeString(body.Data)
	if err != nil || len(data) == 0 || len(data) > gachaCoverMaxBytes {
		WriteAdminError(w, 400, "封面须为 2 MiB 以内的 PNG 或 JPEG 图片")
		return
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") {
		WriteAdminError(w, 400, "封面须为 PNG 或 JPEG 图片")
		return
	}
	if config.Width < 64 || config.Height < 32 || config.Width > 4096 || config.Height > 4096 {
		WriteAdminError(w, 400, "封面尺寸须在 64×32 到 4096×4096 之间")
		return
	}
	select {
	case gachaCoverDecodeSlot <- struct{}{}:
	case <-r.Context().Done():
		return
	}
	_, _, decodeErr := image.Decode(bytes.NewReader(data))
	<-gachaCoverDecodeSlot
	if decodeErr != nil {
		WriteAdminError(w, 400, "图片数据不完整或已损坏，请重新导出后上传")
		return
	}
	extension := map[string]string{"png": ".png", "jpeg": ".jpg"}[format]
	digest := sha256.Sum256(data)
	name := hex.EncodeToString(digest[:]) + extension
	target := filepath.Join(admin.gachaCoverDir, name)
	if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(admin.gachaCoverDir, 0o755); err != nil {
			WriteAdminError(w, 500, err.Error())
			return
		}
		temp, err := os.CreateTemp(admin.gachaCoverDir, ".upload-*")
		if err != nil {
			WriteAdminError(w, 500, err.Error())
			return
		}
		_, writeErr := temp.Write(data)
		closeErr := temp.Close()
		if writeErr != nil || closeErr != nil {
			_ = os.Remove(temp.Name())
			WriteAdminError(w, 500, "封面写入失败")
			return
		}
		if err := os.Rename(temp.Name(), target); err != nil {
			_ = os.Remove(temp.Name())
			// Another upload may have committed this same content-addressed file.
			stored, readErr := os.ReadFile(target)
			if readErr != nil || !bytes.Equal(stored, data) {
				WriteAdminError(w, 500, err.Error())
				return
			}
		}
	} else if err != nil {
		WriteAdminError(w, 500, err.Error())
		return
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "cover_path": gachaCoverFolder + "/" + name, "width": config.Width, "height": config.Height, "bytes": len(data)})
}

// ServeGachaCover serves a stored cover by its content-addressed name; it is shared by the admin preview
// route and the game server's /local/gacha-covers/ route.
func ServeGachaCover(dir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := gachaCoverFolder + "/" + chi.URLParam(r, "file")
		if dir == "" || !gachaCoverPathPattern.MatchString(path) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", map[bool]string{true: "image/png", false: "image/jpeg"}[strings.HasSuffix(path, ".png")])
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.ServeFile(w, r, filepath.Join(dir, filepath.Base(path)))
	}
}

func (admin *API) validateGachaCover(path string) error {
	if path == "" {
		return nil
	}
	if !gachaCoverPathPattern.MatchString(path) {
		return errors.New("封面路径无效，请重新上传")
	}
	if _, err := os.Stat(filepath.Join(admin.gachaCoverDir, filepath.Base(path))); err != nil {
		return errors.New("封面文件不存在，请重新上传")
	}
	return nil
}

// customGachaPresets lists operator pool groups that are not deleted for the publication page, labelled
// like the built-in group they were copied from.
func (admin *API) customGachaPresets() []AdminGachaPreset {
	o := admin.operations
	result := []AdminGachaPreset{}
	for _, pool := range o.customGachaDocument().Pools {
		if pool.Deleted {
			continue
		}
		if n := len(result); n > 0 && result[n-1].GroupID == pool.GroupID {
			result[n-1].GachaIDs = append(result[n-1].GachaIDs, pool.GachaID)
			continue
		}
		preset := AdminGachaPreset{PublicationKey: "operator", GroupID: pool.GroupID, GachaIDs: []int{pool.GachaID}}
		for _, template := range admin.gachaPresets {
			if slices.Contains(template.GachaIDs, pool.TemplateID) {
				preset.BannerKey, preset.ImageURL, preset.PaymentItemID, preset.PaymentItem, preset.DrawCount = template.BannerKey, template.ImageURL, template.PaymentItemID, template.PaymentItem, template.DrawCount
			}
		}
		base := o.gachaBases[pool.GachaID]
		if pool.LegacyProfile != nil {
			preset.PublicationKey, preset.BannerKey, preset.ImageURL = "custom", base.BannerKey, "/gacha-assets/"+base.BannerKey+".png"
			preset.PaymentItemID, preset.DrawCount = base.PayTypeID, base.CardNum
			preset.PaymentItem = map[int]string{2: "友情点", 3: "水晶", 6: "付费水晶"}[base.PayType]
			if base.PayType == 4 {
				preset.PaymentItem = admin.catalogByKey[adminCatalogKey(8, base.PayTypeID)].Name
			}
		}
		preset.Name, preset.Price, preset.CardCount = base.Name, base.Price, len(base.CardIDs)
		result = append(result, preset)
	}
	return result
}
