package cnbootstrap

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"slices"
	"strings"

	"kairisei.local/server/internal/httpapi"
	"kairisei.local/server/internal/release"
)

const cnPlayerPolicyKey = "player-policy"

type cnNoticePolicy struct {
	Enabled bool   `json:"enabled"`
	Title   string `json:"title"`
	Body    string `json:"body"`
}
type cnInitialResources struct {
	Gold         int `json:"gold"`
	Crystals     int `json:"crystals"`
	FriendPoints int `json:"friend_points"`
}
type cnLoginRewards struct {
	Cycle    []release.LoginBonusDay `json:"cycle"`
	Beginner []release.LoginBonusDay `json:"beginner"`
	Total    []release.LoginBonusDay `json:"total_milestones"`
}
type cnPlayerPolicy struct {
	Notice        cnNoticePolicy        `json:"notice"`
	Initial       cnInitialResources    `json:"initial_resources"`
	Login         cnLoginRewards        `json:"login_rewards"`
	StoryCrystals int                   `json:"story_first_clear_crystals"`
	Navigators    []httpapi.NaviSetting `json:"navigators"`
}
type cnPlayerPolicySnapshot struct {
	Value    cnPlayerPolicy
	Revision int
	Runtime  httpapi.PlayerConfiguration
}

func (o *cnOperationStore) initializePlayerPolicy(base release.State, naviPath string) error {
	o.playerLoginBase = base.LoginBonusPolicy
	o.playerNaviNames = map[int8]string{}
	master, err := loadCNNaviRuntimeMaster(naviPath)
	if err != nil {
		return err
	}
	defaults := cnPlayerPolicy{
		Notice:        cnNoticePolicy{Enabled: true, Title: "本地服务公告", Body: "欢迎来到不列颠！祝各位亚瑟游戏愉快。"},
		Login:         cnLoginRewards{Cycle: base.LoginBonusPolicy.Cycle, Beginner: base.LoginBonusPolicy.Beginner, Total: base.LoginBonusPolicy.TotalMilestones},
		StoryCrystals: base.StoryRewardPolicy.MainFirstClear.Num,
		Navigators:    []httpapi.NaviSetting{},
	}
	for _, n := range master.Navigators {
		if n.NaviID == 0 || !n.ClientPublished {
			continue
		}
		id := int8(n.NaviID)
		o.playerNaviNames[id] = n.Name
		defaults.Navigators = append(defaults.Navigators, httpapi.NaviSetting{NaviID: id, Enabled: true, Price: base.User.NaviPurchasePrice})
	}
	o.playerDefaults = &defaults
	return o.loadPlayerPolicy()
}

func (o *cnOperationStore) loadPlayerPolicy() error {
	doc, err := o.readDocument(cnPlayerPolicyKey)
	if err != nil {
		return err
	}
	var value cnPlayerPolicy
	if doc.Revision > 0 {
		if err := json.Unmarshal(doc.Payload, &value); err != nil {
			return err
		}
	} else {
		value = *o.playerDefaults
	}
	if err := o.validatePlayerPolicy(value); err != nil {
		return fmt.Errorf("player policy: %w", err)
	}
	o.playerPolicy.Store(o.playerSnapshot(value, doc.Revision))
	return nil
}

func (o *cnOperationStore) playerSnapshot(p cnPlayerPolicy, revision int) *cnPlayerPolicySnapshot {
	login := o.playerLoginBase
	login.Cycle, login.Beginner, login.TotalMilestones = p.Login.Cycle, p.Login.Beginner, p.Login.Total
	return &cnPlayerPolicySnapshot{Value: p, Revision: revision, Runtime: httpapi.PlayerConfiguration{
		Revision: uint64(revision) + 1, LoginBonus: login, StoryCrystals: p.StoryCrystals, Navigators: p.Navigators,
	}}
}

func validCNPolicyAmount(amount int, allowZero bool) bool {
	return amount <= 10000000 && (amount > 0 || allowZero && amount == 0)
}

func (o *cnOperationStore) validatePlayerPolicy(p cnPlayerPolicy) error {
	if o.playerDefaults == nil {
		return errors.New("运营目录尚未载入")
	}
	if strings.TrimSpace(p.Notice.Title) == "" || len([]rune(p.Notice.Title)) > 80 || len([]rune(p.Notice.Body)) > 8000 {
		return errors.New("公告标题须为1至80字，正文最多8000字")
	}
	if p.Notice.Enabled && strings.TrimSpace(p.Notice.Body) == "" {
		return errors.New("显示公告时正文不能为空")
	}
	for _, n := range []int{p.Initial.Gold, p.Initial.Crystals, p.Initial.FriendPoints} {
		if !validCNPolicyAmount(n, true) {
			return errors.New("新账号起步资源须为0至10000000")
		}
	}
	if !validCNPolicyAmount(p.StoryCrystals, false) {
		return errors.New("剧情首通水晶须为1至10000000")
	}
	for _, pair := range []struct{ days, base []release.LoginBonusDay }{
		{p.Login.Cycle, o.playerDefaults.Login.Cycle}, {p.Login.Beginner, o.playerDefaults.Login.Beginner}, {p.Login.Total, o.playerDefaults.Login.Total},
	} {
		if len(pair.days) != len(pair.base) {
			return errors.New("请保留现有签到天数，只修改奖励和说明")
		}
		for i, d := range pair.days {
			r := d.Reward
			if d.Day != pair.base[i].Day || strings.TrimSpace(d.Comment) == "" || len([]rune(d.Comment)) > 100 || (r.Type != 4 && r.Type != 10) || !validCNPolicyAmount(r.Num, false) || r.RewardTypeID != 0 || r.CardLevel != 0 || r.CardFame != 0 || r.CardLove != 0 || r.CardSkillLevels == nil || len(r.CardSkillLevels) != 0 {
				return errors.New("签到奖励须为金币或水晶，数量1至10000000；天数及奖励格式须正确")
			}
		}
	}
	if len(p.Navigators) != len(o.playerDefaults.Navigators) {
		return errors.New("请保留全部可配置看板")
	}
	seen := map[int8]bool{}
	for _, n := range p.Navigators {
		if _, ok := o.playerNaviNames[n.NaviID]; !ok || seen[n.NaviID] || !validCNPolicyAmount(n.Price, false) {
			return errors.New("看板ID重复、资源未开放或价格无效（1至10000000）")
		}
		seen[n.NaviID] = true
	}
	return nil
}

func (o *cnOperationStore) preparePlayerPolicy(handler http.Handler) {
	p := o.playerPolicy.Load()
	if p == nil {
		return
	}
	if target, ok := handler.(httpapi.PlayerConfigurator); ok {
		target.ApplyPlayerConfiguration(p.Runtime)
	}
}

// Immutable atomic read: account creation already owns a SQLite transaction,
// so it must never acquire the configuration writer's lock here.
func (o *cnOperationStore) initialResources() cnInitialResources {
	if p := o.playerPolicy.Load(); p != nil {
		return p.Value.Initial
	}
	return cnInitialResources{}
}

func (a *cnAdmin) playerPolicy(w http.ResponseWriter, _ *http.Request) {
	p := a.operations.playerPolicy.Load()
	if p == nil {
		writeCNAdminError(w, 503, "运营目录尚未载入")
		return
	}
	writeCNAdminJSON(w, 200, map[string]any{"state": "PASS", "revision": p.Revision, "config": p.Value, "defaults": a.operations.playerDefaults, "navi_names": a.operations.playerNaviNames})
}

func (a *cnAdmin) savePlayerPolicy(w http.ResponseWriter, r *http.Request) {
	if err := requireCNAdminMutation(r); err != nil {
		writeCNAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		Expected *int            `json:"expected_revision"`
		Config   *cnPlayerPolicy `json:"config"`
	}
	if err := decodeCNAdminJSONLimit(r, &body, 256*1024); err != nil || body.Expected == nil || body.Config == nil {
		writeCNAdminError(w, 400, "请提交完整配置和页面版本")
		return
	}
	o := a.operations
	if err := o.validatePlayerPolicy(*body.Config); err != nil {
		writeCNAdminError(w, 400, err.Error())
		return
	}
	// Stable order makes diffs and audit records easy to compare.
	slices.SortFunc(body.Config.Navigators, func(a, b httpapi.NaviSetting) int { return int(a.NaviID) - int(b.NaviID) })
	o.configMu.Lock()
	doc, err := o.writeDocument(cnPlayerPolicyKey, *body.Expected, *body.Config)
	if err == nil {
		o.playerPolicy.Store(o.playerSnapshot(*body.Config, doc.Revision))
	}
	o.configMu.Unlock()
	if err != nil {
		writeContentError(w, err)
		return
	}
	a.playerPolicy(w, r)
}

var cnNoticeTemplate = template.Must(template.New("notice").Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}}</title><style>body{margin:0;background:#17130d;color:#f4e7bd;font-family:sans-serif}main{max-width:760px;margin:auto;padding:28px}h1{color:#ffd66b;border-bottom:1px solid #8b6a2c;padding-bottom:14px}section{background:#282116;border:1px solid #8b6a2c;border-radius:10px;padding:18px;line-height:1.7;white-space:pre-wrap;overflow-wrap:anywhere}</style></head><body><main><h1>{{.Title}}</h1><section>{{.Body}}</section></main></body></html>`))

func (o *cnOperationStore) localNotice(w http.ResponseWriter, r *http.Request) {
	notice := cnNoticePolicy{Title: "公告", Body: "暂无公告。"}
	if p := o.playerPolicy.Load(); p != nil && p.Value.Notice.Enabled {
		notice = p.Value.Notice
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = cnNoticeTemplate.Execute(w, notice)
}
