package admin

import (
	"encoding/json"
	"errors"
	"net/http"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/multiplayer"
)

const runtimeSettingsKey = "runtime-settings"

func (operations *Operations) loadRuntimeSettings() error {
	doc, err := operations.storage.ReadDocument(runtimeSettingsKey)
	if err != nil {
		return err
	}
	settings := game.RuntimeSettings{TeamBattleSpeed: multiplayer.DefaultGameSpeed}
	if doc.Revision != 0 {
		if err := json.Unmarshal(doc.Payload, &settings); err != nil {
			return err
		}
	}
	if err := operations.validateItemShopSettings(settings.ItemShop); err != nil {
		return err
	}
	if !multiplayer.ValidGameSpeed(settings.TeamBattleSpeed) {
		return errors.New("组队倍速须为1、1.5或2倍")
	}
	operations.configMu.Lock()
	defer operations.configMu.Unlock()
	operations.runtimeSettings, operations.runtimeSettingsRevision = settings, doc.Revision
	return nil
}

func (operations *Operations) TeamBattleSpeed() int {
	operations.configMu.RLock()
	defer operations.configMu.RUnlock()
	return operations.runtimeSettings.TeamBattleSpeed
}

func (operations *Operations) PrepareBusiness(handler http.Handler) {
	operations.preparePlayerPolicy(handler)
	operations.prepareGachas(handler)
	if target, ok := handler.(game.ContentConfigurator); ok {
		target.ApplyContentConfiguration(operations.ContentConfiguration())
	}
	if target, ok := handler.(game.RuntimeConfigurator); ok {
		operations.configMu.RLock()
		settings := operations.runtimeSettings
		operations.configMu.RUnlock()
		target.ApplyRuntimeSettings(settings)
	}
}

func (admin *API) runtimeSettings(w http.ResponseWriter, _ *http.Request) {
	admin.operations.configMu.RLock()
	settings, revision := admin.operations.runtimeSettings, admin.operations.runtimeSettingsRevision
	admin.operations.configMu.RUnlock()
	admin.writeRuntimeSettings(w, settings, revision)
}

func (operations *Operations) validateItemShopSettings(settings []game.ItemShopSetting) error {
	allowed := make(map[int]bool, len(operations.itemShopBases))
	for _, lineup := range operations.itemShopBases {
		allowed[lineup.LineupID] = true
	}
	for _, setting := range settings {
		if !allowed[setting.LineupID] || setting.Price < 1 || setting.Price > 10000000 {
			return errors.New("商品不存在、重复或价格不在1–10000000之间")
		}
		delete(allowed, setting.LineupID)
	}
	return nil
}

func (admin *API) writeRuntimeSettings(w http.ResponseWriter, settings game.RuntimeSettings, revision int) {
	overrides := make(map[int]game.ItemShopSetting, len(settings.ItemShop))
	for _, setting := range settings.ItemShop {
		overrides[setting.LineupID] = setting
	}
	settings.ItemShop = make([]game.ItemShopSetting, 0, len(admin.operations.itemShopBases))
	for _, lineup := range admin.operations.itemShopBases {
		setting, exists := overrides[lineup.LineupID]
		if !exists {
			setting = game.ItemShopSetting{LineupID: lineup.LineupID, Enabled: !lineup.Disabled, Price: lineup.Price}
		}
		settings.ItemShop = append(settings.ItemShop, setting)
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "settings": settings, "revision": revision, "item_shop_catalog": admin.operations.itemShopBases})
}

func (admin *API) setRuntimeSettings(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		Enabled         *bool                  `json:"crystal_purchase_enabled"`
		Expected        *int                   `json:"expected_revision"`
		ItemShop        []game.ItemShopSetting `json:"item_shop"`
		TeamBattleSpeed *int                   `json:"team_battle_speed"`
	}
	if err := decodeAdminJSON(r, &body); err != nil || body.Enabled == nil || body.Expected == nil {
		WriteAdminError(w, 400, "缺少开关或配置版本，请重新加载")
		return
	}
	if err := admin.operations.validateItemShopSettings(body.ItemShop); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	if body.TeamBattleSpeed != nil && !multiplayer.ValidGameSpeed(*body.TeamBattleSpeed) {
		WriteAdminError(w, 400, "组队倍速须为1、1.5或2倍")
		return
	}
	admin.operations.configMu.Lock()
	settings := admin.operations.runtimeSettings
	settings.CrystalPurchaseEnabled = *body.Enabled
	if body.TeamBattleSpeed != nil {
		settings.TeamBattleSpeed = *body.TeamBattleSpeed
	}
	settings.ItemShop = body.ItemShop
	if body.ItemShop == nil {
		settings.ItemShop = admin.operations.runtimeSettings.ItemShop
	}
	doc, err := admin.operations.writeDocument(runtimeSettingsKey, *body.Expected, settings)
	if err == nil {
		admin.operations.runtimeSettings, admin.operations.runtimeSettingsRevision = settings, doc.Revision
	}
	admin.operations.configMu.Unlock()
	if errors.Is(err, accountstore.ErrDocumentConflict) {
		WriteAdminError(w, 409, "设置已被更新，请刷新后重试")
		return
	}
	if err != nil {
		WriteAdminError(w, 500, err.Error())
		return
	}
	admin.writeRuntimeSettings(w, settings, doc.Revision)
}
