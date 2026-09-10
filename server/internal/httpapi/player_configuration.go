package httpapi

import "kairisei.local/server/internal/release"

// Shared public policy; applying it never resets ownership or claim cursors.
type PlayerConfiguration struct {
	Revision      uint64
	LoginBonus    release.LoginBonusPolicy
	StoryCrystals int
	Navigators    []NaviSetting
	TutorialMail  TutorialCompletionMail
}

// Public policy, not account state. The terminal onboarding transition and its
// presents are persisted together, so retries need no separate delivery job.
type TutorialCompletionMail struct {
	Enabled bool             `json:"enabled"`
	Title   string           `json:"title"`
	Message string           `json:"message"`
	Rewards []release.Reward `json:"rewards"`
}

type NaviSetting struct {
	NaviID  int8 `json:"navi_id"`
	Enabled bool `json:"enabled"`
	Price   int  `json:"price"`
}

type PlayerConfigurator interface{ ApplyPlayerConfiguration(PlayerConfiguration) }

func (h *accountBusinessHandler) ApplyPlayerConfiguration(config PlayerConfiguration) {
	if config.Revision == 0 || config.Revision == h.playerRevision {
		return
	}
	s := h.api.store
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loginBonusPolicy = config.LoginBonus
	s.tutorialCompletionMail = config.TutorialMail
	s.storyRewardPolicy.MainFirstClear.Num = config.StoryCrystals
	s.storyRewardPolicy.SubFirstClear.Num = config.StoryCrystals
	s.storyRewardPolicy.EventFirstClear.Num = config.StoryCrystals
	s.naviSettings = make(map[int8]NaviSetting, len(config.Navigators))
	for _, n := range config.Navigators {
		s.naviSettings[n.NaviID] = n
	}
	h.playerRevision = config.Revision
}

func (s *store) naviPriceLocked(id int8) (int, bool) {
	if n, ok := s.naviSettings[id]; ok {
		return n.Price, n.Enabled
	}
	return s.naviPurchasePrice, s.naviPurchasePrice > 0
}

func (s *store) naviPrices() map[int8]NaviSetting {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[int8]NaviSetting, len(s.naviCatalogIDs))
	for id := range s.naviCatalogIDs {
		price, enabled := s.naviPriceLocked(id)
		result[id] = NaviSetting{NaviID: id, Price: price, Enabled: enabled}
	}
	return result
}
