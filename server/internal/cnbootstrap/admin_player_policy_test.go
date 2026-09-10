package cnbootstrap

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/httpapi"
	"kairisei.local/server/internal/release"
)

func TestPlayerPolicySaveRestartAndNewAccounts(t *testing.T) {
	accounts := newFriendCapacityTestAccounts(t)
	o, err := newCNOperationStore(accounts.storage, nil)
	if err != nil {
		t.Fatal(err)
	}
	days := []release.LoginBonusDay{{Day: 1, Comment: "签到", Reward: release.Reward{Type: 10, Num: 5, CardSkillLevels: []int16{}}}}
	o.playerLoginBase = release.LoginBonusPolicy{ConfigVersion: 1, Cycle: days, Beginner: days, TotalMilestones: days}
	o.playerDefaults = &cnPlayerPolicy{Notice: cnNoticePolicy{Title: "公告", Body: "欢迎", Enabled: true}, Login: cnLoginRewards{days, days, days}, StoryCrystals: 50, Navigators: []httpapi.NaviSetting{{NaviID: 1, Enabled: true, Price: 500}}}
	o.playerNaviNames = map[int8]string{1: "妮妙"}
	if err := o.loadPlayerPolicy(); err != nil {
		t.Fatal(err)
	}
	a := &cnAdmin{operations: o, catalogByKey: map[string]cnAdminCatalogEntry{
		cnAdminCatalogKey(10, 0):       {RewardType: 10},
		cnAdminCatalogKey(6, 10000010): {RewardType: 6, RewardTypeID: 10000010, LevelMax: 60, FameMax: 90, LoveMax: 100},
	}}
	r := chi.NewRouter()
	r.Put("/policy", a.savePlayerPolicy)
	var config cnPlayerPolicy
	data, _ := json.Marshal(o.playerDefaults)
	_ = json.Unmarshal(data, &config)
	config.TutorialMail = httpapi.TutorialCompletionMail{Enabled: true, Title: "毕业礼物", Message: "全部训练完成奖励", Rewards: []release.Reward{
		{Type: 10, Num: 456, CardSkillLevels: []int16{}},
		{Type: 6, RewardTypeID: 10000010, Num: 2, CardLevel: 60, CardFame: 90, CardLove: 100, CardSkillLevels: []int16{1}},
	}}
	config.Notice.Body = "<script>alert(1)</script>\n活动公告"
	config.Login.Cycle[0].Reward.Num = 31
	config.StoryCrystals = 75
	config.Navigators[0].Price = 222
	callContentAdmin(t, r, "PUT", "/policy", map[string]any{"expected_revision": 0, "config": config}, 200)
	callContentAdmin(t, r, "PUT", "/policy", map[string]any{"expected_revision": 0, "config": config}, 409)
	bad := config
	bad.StoryCrystals = -1
	callContentAdmin(t, r, "PUT", "/policy", map[string]any{"expected_revision": 1, "config": bad}, 400)
	bad = config
	bad.TutorialMail.Rewards = []release.Reward{{Type: 6, RewardTypeID: 99999999, Num: 1}}
	callContentAdmin(t, r, "PUT", "/policy", map[string]any{"expected_revision": 1, "config": bad}, 400)
	bad.TutorialMail.Rewards = append(config.TutorialMail.Rewards, config.TutorialMail.Rewards[0])
	callContentAdmin(t, r, "PUT", "/policy", map[string]any{"expected_revision": 1, "config": bad}, 400)
	w := httptest.NewRecorder()
	o.localNotice(w, httptest.NewRequest("GET", "/", nil))
	if strings.Contains(w.Body.String(), "<script>") || !strings.Contains(w.Body.String(), "活动公告") {
		t.Fatal("notice text not safely rendered")
	}
	restarted, err := newCNOperationStore(accounts.storage, nil)
	if err != nil {
		t.Fatal(err)
	}
	restarted.playerDefaults = o.playerDefaults
	restarted.playerNaviNames = o.playerNaviNames
	restarted.playerLoginBase = o.playerLoginBase
	if err := restarted.loadPlayerPolicy(); err != nil {
		t.Fatal(err)
	}
	p := restarted.playerPolicy.Load()
	if p.Revision != 1 || !reflect.DeepEqual(p.Runtime.TutorialMail, config.TutorialMail) || p.Runtime.LoginBonus.Cycle[0].Reward.Num != 31 || p.Runtime.Navigators[0].Price != 222 {
		t.Fatal("public policy was not restored")
	}
	for i, uuid := range []string{"00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002"} {
		identity, err := accounts.resolveLogin(uuid)
		if err != nil {
			t.Fatal(err)
		}
		if identity.UserID != cnPrimaryUserID+i {
			t.Fatalf("unexpected identity %d", identity.UserID)
		}
		state, err := accounts.loadState(identity.UserID)
		if err != nil {
			t.Fatal(err)
		}
		if state.User.Gold != 0 || state.User.CoinFree != 0 || state.User.FriendPoint != 0 || state.Onboarding.Step != 0 || len(state.Engagement.Presents) != 0 {
			t.Fatalf("new user balance/flow: %+v", state.User)
		}
		state.User.Gold = 9
		if err := accounts.persistState(identity.UserID, state); err != nil {
			t.Fatal(err)
		}
		if _, err := accounts.resolveLogin(uuid); err != nil {
			t.Fatal(err)
		}
		state, err = accounts.loadState(identity.UserID)
		if err != nil || state.User.Gold != 9 {
			t.Fatal("existing login was reset")
		}
	}
	config.Notice.Enabled = false
	callContentAdmin(t, r, "PUT", "/policy", map[string]any{"expected_revision": 1, "config": config}, 200)
	w = httptest.NewRecorder()
	o.localNotice(w, httptest.NewRequest("GET", "/", nil))
	if strings.Contains(w.Body.String(), "活动公告") || !strings.Contains(w.Body.String(), "暂无公告") {
		t.Fatal("disabled notice remains visible")
	}
}

func auditCompletePlayerPolicy(t *testing.T, h http.Handler) {
	t.Helper()
	a := h.(interface{ AdminHandler() http.Handler }).AdminHandler()
	var data struct {
		Config   cnPlayerPolicy `json:"config"`
		Revision int            `json:"revision"`
	}
	if err := json.Unmarshal(callContentAdmin(t, a, "GET", "/api/player-policy", nil, 200), &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Config.Navigators) == 0 || len(data.Config.Login.Cycle) == 0 || len(data.Config.Login.Beginner) == 0 || len(data.Config.Login.Total) == 0 {
		t.Fatal("missing public policies")
	}
	callContentAdmin(t, a, "PUT", "/api/player-policy", map[string]any{"expected_revision": data.Revision, "config": data.Config}, 200)
	t.Logf("player policy: %d cycle / %d beginner / %d total rewards, %d purchasable navigators", len(data.Config.Login.Cycle), len(data.Config.Login.Beginner), len(data.Config.Login.Total), len(data.Config.Navigators))
}
