package cnbootstrap

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/httpapi"
	"kairisei.local/server/internal/release"
)

type cnAdminGachaConfig struct {
	Disabled   bool                     `json:"disabled,omitempty"`
	GachaID    int                      `json:"gacha_id"`
	Name       string                   `json:"name"`
	Price      int                      `json:"price"`
	StartUnix  int64                    `json:"start_unix"`
	EndUnix    int64                    `json:"end_unix"`
	CardIDs    []int                    `json:"card_ids"`
	Weights    []int                    `json:"weights"`
	RewardPool []release.WeightedReward `json:"reward_pool,omitempty"`
	Steps      []release.GachaStep      `json:"steps,omitempty"`
}

type cnAdminDocument struct {
	Revision   int             `json:"revision"`
	UpdatedUTC string          `json:"updated_utc,omitempty"`
	SHA256     string          `json:"sha256"`
	Payload    json.RawMessage `json:"payload"`
}

var errCNAdminConflict = errors.New("配置已被其他页面修改，请重新载入后预览")

func (operations *cnOperationStore) readDocument(key string) (cnAdminDocument, error) {
	db, err := operations.storage.open()
	if err != nil {
		return cnAdminDocument{}, err
	}
	defer db.Close()
	var doc cnAdminDocument
	err = db.QueryRow(`SELECT revision, updated_utc, payload_json, payload_sha256 FROM cn_global_operation WHERE operation_key = ?`, key).Scan(&doc.Revision, &doc.UpdatedUTC, &doc.Payload, &doc.SHA256)
	if errors.Is(err, sql.ErrNoRows) {
		return doc, nil
	}
	if err != nil {
		return doc, err
	}
	digest := sha256.Sum256(doc.Payload)
	if hex.EncodeToString(digest[:]) != doc.SHA256 {
		return doc, errors.New("operations document digest mismatch")
	}
	return doc, nil
}

// The revision guard and audit entry commit together. A stale browser cannot
// overwrite an operator's newer draft or publication.
func (operations *cnOperationStore) writeDocument(key string, expected int, value any) (cnAdminDocument, error) {
	if expected < 0 {
		return cnAdminDocument{}, errCNAdminConflict
	}
	body, err := json.Marshal(value)
	if err != nil {
		return cnAdminDocument{}, err
	}
	digest := sha256.Sum256(body)
	doc := cnAdminDocument{Revision: expected + 1, UpdatedUTC: time.Now().UTC().Format(time.RFC3339Nano), SHA256: hex.EncodeToString(digest[:]), Payload: body}
	db, err := operations.storage.open()
	if err != nil {
		return doc, err
	}
	defer db.Close()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return doc, err
	}
	defer tx.Rollback()
	var result sql.Result
	if expected == 0 {
		result, err = tx.Exec(`INSERT INTO cn_global_operation (operation_key, revision, updated_utc, payload_json, payload_sha256) VALUES (?, 1, ?, ?, ?) ON CONFLICT(operation_key) DO NOTHING`, key, doc.UpdatedUTC, body, doc.SHA256)
	} else {
		result, err = tx.Exec(`UPDATE cn_global_operation SET revision=revision+1, updated_utc=?, payload_json=?, payload_sha256=? WHERE operation_key=? AND revision=?`, doc.UpdatedUTC, body, doc.SHA256, key, expected)
	}
	if err != nil {
		return doc, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return doc, err
	}
	if count != 1 {
		return doc, errCNAdminConflict
	}
	operation := strings.Split(key, ":")[0]
	if key == cnTeamBattlePublicationKey || key == cnPastBattlePublicationKey {
		operation = "boss-policy"
	} else if key == cnGachaPublicationKey {
		operation = "gacha-policy"
	}
	if _, err = tx.Exec(`INSERT INTO cn_admin_audit (created_utc, operation, target, payload_json, payload_sha256) VALUES (?, ?, ?, ?, ?)`, doc.UpdatedUTC, operation, key, body, doc.SHA256); err != nil {
		return doc, err
	}
	return doc, tx.Commit()
}

func cnAdminGachaConfigFromProfile(profile release.GachaProfile) cnAdminGachaConfig {
	profile = release.CloneGachas([]release.GachaProfile{profile})[0]
	return cnAdminGachaConfig{GachaID: profile.GachaID, Name: profile.Name, Price: profile.Price, CardIDs: append([]int{}, profile.CardIDs...), Weights: append([]int{}, profile.CardWeights...), RewardPool: profile.RewardPool, Steps: profile.Steps}
}

func cnAdminConfiguredGacha(base release.GachaProfile, config cnAdminGachaConfig) httpapi.GachaConfiguration {
	base = release.CloneGachas([]release.GachaProfile{base})[0]
	base.Name, base.Price = config.Name, config.Price
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
	if len(base.RewardPool) > 0 {
		base.RewardPool, base.Steps = config.RewardPool, config.Steps
		base.BuyMessage = fmt.Sprintf("消耗 %d %s 抽取 %d 次吗？", config.Price, payment, base.CardNum)
		base.SubMessage = "本地复刻；阶段费用、奖励概率及赠礼见详情。"
		base = release.CloneGachas([]release.GachaProfile{base})[0]
	}
	base.EndTime = 2147483647
	if config.EndUnix != 0 {
		base.EndTime = int(config.EndUnix)
	}
	base.PoolSourceState = "INFERRED_LOCAL_ADMIN_CONFIGURATION"
	base.WeightSourceState = "INFERRED_LOCAL_ADMIN_CONFIGURATION"
	return httpapi.GachaConfiguration{Profile: base, StartUnix: config.StartUnix, EndUnix: config.EndUnix, Disabled: config.Disabled}
}

func (operations *cnOperationStore) prepareGachas(handler http.Handler) {
	configurator, ok := handler.(httpapi.GachaConfigurator)
	if !ok {
		return
	}
	operations.configMu.RLock()
	defer operations.configMu.RUnlock()
	configurator.ApplyGachaConfiguration(operations.gachaRevision, operations.gachaConfigurations)
}

func (operations *cnOperationStore) reloadGachaConfigurations() error {
	configs := make([]httpapi.GachaConfiguration, 0)
	var revision uint64
	ids := make([]int, 0, len(operations.gachaBases))
	for id := range operations.gachaBases {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		doc, err := operations.readDocument("gacha-live:" + strconv.Itoa(id))
		if err != nil {
			return err
		}
		if doc.Revision == 0 {
			continue
		}
		var config cnAdminGachaConfig
		if err := json.Unmarshal(doc.Payload, &config); err != nil {
			return err
		}
		if config.GachaID != id {
			return errors.New("gacha operation identity mismatch")
		}
		configs = append(configs, cnAdminConfiguredGacha(operations.gachaBases[id], config))
		revision += uint64(doc.Revision)
	}
	operations.gachaConfigurations, operations.gachaRevision = configs, revision
	return nil
}

func (admin *cnAdmin) validateGachaConfig(config cnAdminGachaConfig) (map[string]any, error) {
	base, exists := admin.operations.gachaBases[config.GachaID]
	if !exists {
		return nil, errors.New("不能编辑新手保留卡池或未知卡池")
	}
	if strings.TrimSpace(config.Name) == "" || len([]rune(config.Name)) > 60 || config.Price < 1 || config.Price > 10000000 {
		return nil, errors.New("名称须为 1–60 字；价格须为 1–10000000 的整数")
	}
	if config.StartUnix < 0 || config.EndUnix < 0 || config.StartUnix > 2147483647 || config.EndUnix > 2147483647 || (config.EndUnix != 0 && config.EndUnix <= config.StartUnix) {
		return nil, errors.New("排期无效：结束时间须晚于开始时间且早于 2038-01-19")
	}
	if len(base.RewardPool) > 0 {
		return admin.validateMixedGachaConfig(base, config)
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
		entry, exists := admin.catalogByKey[cnAdminCatalogKey(6, id)]
		if !exists || seen[id] || config.Weights[index] < 1 || config.Weights[index] > 1000000 {
			return nil, fmt.Errorf("卡牌 %d 不存在、重复或权重超出 1–1000000", id)
		}
		if !cnAdminGachaCardEligible(base, entry, id) {
			return nil, fmt.Errorf("卡牌 %d 不是扭蛋来源的初始形态，不能加入此卡池", id)
		}
		seen[id] = true
		if entry.ResourceState == "unavailable" {
			blocked = append(blocked, id)
		}
		rarities[entry.Rarity]++
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
	odds, err := httpapi.PreviewGachaWeights(config.Weights)
	if err != nil {
		return nil, err
	}
	rarityByID := make(map[int]int, len(config.CardIDs))
	for _, id := range config.CardIDs {
		rarityByID[id] = admin.catalogByKey[cnAdminCatalogKey(6, id)].Rarity
	}
	stages, err := httpapi.PreviewGachaStages(cnAdminConfiguredGacha(base, config).Profile, rarityByID)
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
	return map[string]any{"config": config, "base": base, "odds_scaled": odds, "odds_scale": 100000, "rarities": rarities, "blocked_card_ids": blocked, "publishable": len(blocked) == 0, "warnings": warnings, "stages": stages}, nil
}

func (admin *cnAdmin) gachaEditorList(writer http.ResponseWriter, request *http.Request) {
	admin.operations.configMu.RLock()
	defer admin.operations.configMu.RUnlock()
	rows := make([]map[string]any, 0, len(admin.operations.gachaBases))
	for id, base := range admin.operations.gachaBases {
		live, err := admin.operations.readDocument("gacha-live:" + strconv.Itoa(id))
		if err != nil {
			writeCNAdminError(writer, 500, err.Error())
			return
		}
		draft, err := admin.operations.readDocument("gacha-draft:" + strconv.Itoa(id))
		if err != nil {
			writeCNAdminError(writer, 500, err.Error())
			return
		}
		config := cnAdminGachaConfigFromProfile(base)
		if live.Revision > 0 {
			if err := json.Unmarshal(live.Payload, &config); err != nil {
				writeCNAdminError(writer, 500, err.Error())
				return
			}
		}
		rows = append(rows, map[string]any{"gacha_id": id, "config": config, "base": base, "live": live, "draft": draft})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i]["gacha_id"].(int) < rows[j]["gacha_id"].(int) })
	writeCNAdminJSON(writer, 200, map[string]any{"state": "PASS", "pools": rows})
}

func (admin *cnAdmin) gachaEditorAction(writer http.ResponseWriter, request *http.Request) {
	if err := requireCNAdminMutation(request); err != nil {
		writeCNAdminError(writer, 403, err.Error())
		return
	}
	var body struct {
		Config               cnAdminGachaConfig `json:"config"`
		ExpectedRevision     int                `json:"expected_revision"`
		ExpectedLiveRevision int                `json:"expected_live_revision"`
		SHA256               string             `json:"sha256"`
	}
	if err := decodeCNAdminJSONLimit(request, &body, 2*1024*1024); err != nil {
		writeCNAdminError(writer, 400, err.Error())
		return
	}
	admin.operations.configMu.Lock()
	defer admin.operations.configMu.Unlock()
	action := chi.URLParam(request, "action")
	if action != "preview" && action != "draft" && action != "publish" {
		writeCNAdminError(writer, 404, "unknown editor action")
		return
	}
	key := strconv.Itoa(body.Config.GachaID)
	if action == "publish" {
		draft, err := admin.operations.readDocument("gacha-draft:" + key)
		if err != nil {
			writeCNAdminError(writer, 500, err.Error())
			return
		}
		if draft.Revision == 0 || draft.Revision != body.ExpectedRevision || draft.SHA256 != body.SHA256 {
			writeCNAdminError(writer, 409, errCNAdminConflict.Error())
			return
		}
		if err := json.Unmarshal(draft.Payload, &body.Config); err != nil {
			writeCNAdminError(writer, 500, err.Error())
			return
		}
	}
	preview, err := admin.validateGachaConfig(body.Config)
	if err != nil {
		writeCNAdminError(writer, 400, err.Error())
		return
	}
	if action == "preview" {
		writeCNAdminJSON(writer, 200, map[string]any{"state": "PASS", "preview": preview})
		return
	}
	operationKey, expected := "gacha-draft:"+key, body.ExpectedRevision
	if action == "publish" {
		if !preview["publishable"].(bool) {
			writeCNAdminError(writer, 400, "发布被资源闭包阻止，详见预览中的卡牌 ID")
			return
		}
		operationKey, expected = "gacha-live:"+key, body.ExpectedLiveRevision
	}
	doc, err := admin.operations.writeDocument(operationKey, expected, body.Config)
	if err != nil {
		status := 500
		if errors.Is(err, errCNAdminConflict) {
			status = 409
		}
		writeCNAdminError(writer, status, err.Error())
		return
	}
	if action == "publish" {
		// Update the immutable in-memory configuration while holding the same
		// lock readers use. No account cache or player save is bulk rewritten.
		configured := cnAdminConfiguredGacha(admin.operations.gachaBases[body.Config.GachaID], body.Config)
		replaced := false
		for i := range admin.operations.gachaConfigurations {
			if admin.operations.gachaConfigurations[i].Profile.GachaID == body.Config.GachaID {
				admin.operations.gachaConfigurations[i] = configured
				replaced = true
				break
			}
		}
		if !replaced {
			admin.operations.gachaConfigurations = append(admin.operations.gachaConfigurations, configured)
		}
		admin.operations.gachaRevision++
	}
	writeCNAdminJSON(writer, 200, map[string]any{"state": "PASS", "document": doc, "preview": preview})
}
