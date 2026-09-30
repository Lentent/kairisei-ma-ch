package game

import (
	"kairisei.local/server/internal/gamestate"
	"time"
)

// RuntimeSettings is shared operator policy, never part of a player snapshot.
// The zero value deliberately leaves local crystal purchases disabled.
type RuntimeSettings struct {
	CrystalPurchaseEnabled bool              `json:"crystal_purchase_enabled"`
	TeamBattleSpeed        int               `json:"team_battle_speed"`
	ItemShop               []ItemShopSetting `json:"item_shop,omitempty"`
	// GachaCoverSource selects where operator pool covers are read from: "" or "server" serves the
	// local gacha-covers folder, "storage" prefixes GachaCoverBaseURL (for example an S3/CDN bucket).
	GachaCoverSource  string `json:"gacha_cover_source,omitempty"`
	GachaCoverBaseURL string `json:"gacha_cover_base_url,omitempty"`
}

type ItemShopSetting struct {
	LineupID    int    `json:"lineup_id"`
	Enabled     bool   `json:"enabled"`
	Price       int    `json:"price"`
	ItemID      int    `json:"item_id,omitempty"`
	BuyType     int    `json:"buy_type,omitempty"`
	Name        string `json:"name,omitempty"`
	Quantity    int    `json:"quantity,omitempty"`
	PayType     int    `json:"pay_type,omitempty"`
	TabType     int    `json:"tab_type,omitempty"`
	BuyNumMax   int    `json:"buy_num_max,omitempty"`
	TotalLimit  int    `json:"total_limit,omitempty"`
	Period      string `json:"period,omitempty"`
	PeriodLimit int    `json:"period_limit,omitempty"`
}

type RuntimeConfigurator interface {
	ApplyRuntimeSettings(RuntimeSettings)
}

func (s *Account) ApplyRuntimeSettings(settings RuntimeSettings) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.localCrystalPurchaseEnabled = settings.CrystalPurchaseEnabled
	s.gachaCoverBaseURL = ""
	if settings.GachaCoverSource == "storage" {
		s.gachaCoverBaseURL = settings.GachaCoverBaseURL
	}
	s.itemShopSettings = make(map[int]ItemShopSetting, len(settings.ItemShop))
	for _, setting := range settings.ItemShop {
		s.itemShopSettings[setting.LineupID] = setting
	}
}

// GachaCoverBaseURL is the storage-container prefix for pool covers, or "" to serve them locally.
func (s *Account) GachaCoverBaseURL() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.gachaCoverBaseURL
}

func (s *Account) configuredItemShopLineup(lineup gamestate.ItemShopLineup) gamestate.ItemShopLineup {
	return s.itemShopLineupAt(lineup, time.Now())
}
