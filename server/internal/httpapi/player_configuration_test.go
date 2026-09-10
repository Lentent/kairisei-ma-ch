package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"kairisei.local/server/internal/release"
)

func TestPlayerPolicyKeepsClaimsAndMatchesNaviPrice(t *testing.T) {
	reward := func(n int) release.Reward { return release.Reward{Type: 10, Num: n, CardSkillLevels: []int16{}} }
	day := func(n int) []release.LoginBonusDay {
		return []release.LoginBonusDay{{Day: 1, Comment: "签到", Reward: reward(n)}}
	}
	config := PlayerConfiguration{Revision: 1, StoryCrystals: 75, LoginBonus: release.LoginBonusPolicy{ConfigVersion: 1, Cycle: day(10), Beginner: day(20), TotalMilestones: day(30)}, Navigators: []NaviSetting{{NaviID: 1, Enabled: true, Price: 77}}}
	s := &store{currentName: "亚瑟", coinFree: 100, loginBonusState: release.LoginBonusState{ConfigVersion: 1},
		naviCatalogIDs: map[int8]struct{}{0: {}, 1: {}}, selectableNaviIDs: map[int8]struct{}{0: {}}, naviPurchasePrice: 500,
		storyRewardPolicy: release.StoryRewardPolicy{MainFirstClear: reward(50), SubFirstClear: reward(50), EventFirstClear: reward(50)},
		storyMainParts:    []release.StoryMainPart{{Sections: []release.StoryMainSection{{Stories: []release.StoryMain{{StoryMainID: 1, StateFlag: 1}}}}}},
	}
	a := &API{store: s, release: &release.Release{State: release.State{User: release.User{NaviCatalogIDs: []int8{0, 1}}}}}
	h := &accountBusinessHandler{api: a}
	h.ApplyPlayerConfiguration(config)
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	if claims, err := s.claimLoginBonuses(now); err != nil || claims.Daily == nil || s.coinFree != 160 {
		t.Fatalf("configured login rewards: %v %d", err, s.coinFree)
	}
	config.Revision++
	config.LoginBonus.Cycle = day(40)
	h.ApplyPlayerConfiguration(config)
	if claims, err := s.claimLoginBonuses(now); err != nil || claims.Daily != nil || s.coinFree != 160 {
		t.Fatal("configuration reset today's claim")
	}
	if _, err := s.claimLoginBonuses(now.Add(24 * time.Hour)); err != nil || s.coinFree != 200 {
		t.Fatalf("next-day reward: %v %d", err, s.coinFree)
	}
	w := httptest.NewRecorder()
	a.getNaviShow(w, httptest.NewRequest("POST", "/", nil))
	var shown struct {
		Navis []struct {
			ID    int `json:"navi_id"`
			Price int `json:"get_value"`
			Owned int `json:"is_get"`
		} `json:"NaviList"`
	}
	if err := json.Unmarshal(bytes.Split(w.Body.Bytes(), []byte{'\n'})[1], &shown); err != nil || len(shown.Navis) != 2 || shown.Navis[1].Price != 77 {
		t.Fatalf("display price: %s", w.Body.String())
	}
	if err := s.purchaseNavi(1); err != nil || s.coinFree != 123 {
		t.Fatalf("navi charge: %v %d", err, s.coinFree)
	}
	config.Revision++
	config.Navigators = []NaviSetting{{NaviID: 1, Enabled: false, Price: 77}}
	h.ApplyPlayerConfiguration(config)
	if !s.selectNavi(1) {
		t.Fatal("disabling sale removed ownership")
	}
	delete(s.selectableNaviIDs, 1)
	if err := s.purchaseNavi(1); err == nil || s.coinFree != 123 {
		t.Fatal("disabled navi was purchased")
	}
	config.Revision++
	config.Navigators[0].Enabled = true
	config.Navigators[0].Price = 500
	h.ApplyPlayerConfiguration(config)
	if err := s.purchaseNavi(1); err != errInsufficientCrystals || s.coinFree != 123 {
		t.Fatal("insufficient balance changed")
	}
	for i := 0; i < 2; i++ {
		if !s.beginMainStory(1) {
			t.Fatal("story unavailable")
		}
		if _, err := s.endMainStory(true); err != nil {
			t.Fatal(err)
		}
	}
	if s.coinFree != 198 {
		t.Fatalf("story reward duplicated or wrong: %d", s.coinFree)
	}
}
