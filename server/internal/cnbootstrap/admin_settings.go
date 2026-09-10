package cnbootstrap

import (
	"encoding/json"
	"errors"
	"net/http"

	"kairisei.local/server/internal/httpapi"
)

const cnRuntimeSettingsKey = "runtime-settings"

func (operations *cnOperationStore) loadRuntimeSettings() error {
	doc, err := operations.readDocument(cnRuntimeSettingsKey)
	if err != nil {
		return err
	}
	var settings httpapi.RuntimeSettings
	if doc.Revision != 0 {
		if err := json.Unmarshal(doc.Payload, &settings); err != nil {
			return err
		}
	}
	if err := operations.validateItemShopSettings(settings.ItemShop); err != nil {
		return err
	}
	operations.configMu.Lock()
	defer operations.configMu.Unlock()
	operations.runtimeSettings, operations.runtimeSettingsRevision = settings, doc.Revision
	return nil
}

func (operations *cnOperationStore) prepareBusiness(handler http.Handler) {
	operations.preparePlayerPolicy(handler)
	operations.prepareGachas(handler)
	if target, ok := handler.(httpapi.ContentConfigurator); ok {
		target.ApplyContentConfiguration(operations.contentConfiguration())
	}
	if target, ok := handler.(httpapi.RuntimeConfigurator); ok {
		operations.configMu.RLock()
		settings := operations.runtimeSettings
		operations.configMu.RUnlock()
		target.ApplyRuntimeSettings(settings)
	}
}

func (admin *cnAdmin) runtimeSettings(w http.ResponseWriter, _ *http.Request) {
	admin.operations.configMu.RLock()
	settings, revision := admin.operations.runtimeSettings, admin.operations.runtimeSettingsRevision
	admin.operations.configMu.RUnlock()
	admin.writeRuntimeSettings(w, settings, revision)
}

func (operations *cnOperationStore) validateItemShopSettings(settings []httpapi.ItemShopSetting) error {
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

func (admin *cnAdmin) writeRuntimeSettings(w http.ResponseWriter, settings httpapi.RuntimeSettings, revision int) {
	overrides := make(map[int]httpapi.ItemShopSetting, len(settings.ItemShop))
	for _, setting := range settings.ItemShop {
		overrides[setting.LineupID] = setting
	}
	settings.ItemShop = make([]httpapi.ItemShopSetting, 0, len(admin.operations.itemShopBases))
	for _, lineup := range admin.operations.itemShopBases {
		setting, exists := overrides[lineup.LineupID]
		if !exists {
			setting = httpapi.ItemShopSetting{LineupID: lineup.LineupID, Enabled: !lineup.Disabled, Price: lineup.Price}
		}
		settings.ItemShop = append(settings.ItemShop, setting)
	}
	writeCNAdminJSON(w, 200, map[string]any{"state": "PASS", "settings": settings, "revision": revision, "item_shop_catalog": admin.operations.itemShopBases})
}

func (admin *cnAdmin) setRuntimeSettings(w http.ResponseWriter, r *http.Request) {
	if err := requireCNAdminMutation(r); err != nil {
		writeCNAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		Enabled  *bool                     `json:"crystal_purchase_enabled"`
		Expected *int                      `json:"expected_revision"`
		ItemShop []httpapi.ItemShopSetting `json:"item_shop"`
	}
	if err := decodeCNAdminJSON(r, &body); err != nil || body.Enabled == nil || body.Expected == nil {
		writeCNAdminError(w, 400, "缺少开关或配置版本，请重新加载")
		return
	}
	if err := admin.operations.validateItemShopSettings(body.ItemShop); err != nil {
		writeCNAdminError(w, 400, err.Error())
		return
	}
	admin.operations.configMu.Lock()
	settings := httpapi.RuntimeSettings{CrystalPurchaseEnabled: *body.Enabled, ItemShop: body.ItemShop}
	if body.ItemShop == nil {
		settings.ItemShop = admin.operations.runtimeSettings.ItemShop
	}
	doc, err := admin.operations.writeDocument(cnRuntimeSettingsKey, *body.Expected, settings)
	if err == nil {
		admin.operations.runtimeSettings, admin.operations.runtimeSettingsRevision = settings, doc.Revision
	}
	admin.operations.configMu.Unlock()
	if errors.Is(err, errCNAdminConflict) {
		writeCNAdminError(w, 409, "设置已被更新，请刷新后重试")
		return
	}
	if err != nil {
		writeCNAdminError(w, 500, err.Error())
		return
	}
	admin.writeRuntimeSettings(w, settings, doc.Revision)
}
