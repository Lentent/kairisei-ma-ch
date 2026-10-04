package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/testfixture"
)

func newDungeonScheduleTestAPI(t *testing.T) (*API, http.Handler) {
	t.Helper()
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	o, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	groups := []AdminBattleGroup{
		{GroupID: 1, Name: "活动奇美拉", Category: "3d", Difficulties: []string{"超级", "超弩级"}},
		{GroupID: 2, Name: "素材副本", Category: "material", Difficulties: []string{"上级"}},
		{GroupID: 3, Name: "未发布巨龙", Category: "2d", Difficulties: []string{"中级"}},
	}
	past := []AdminBattleGroup{{GroupID: 1, Name: "往期奇美拉", Category: "3d", Difficulties: []string{"超弩级"}}}
	o.dungeonScheduleGroups = map[string][]AdminBattleGroup{"activity": groups, "past": past}
	a := &API{accounts: accounts, operations: o, groups: groups, knownGroups: map[int]struct{}{1: {}, 2: {}, 3: {}}, pastGroups: past}
	router := chi.NewRouter()
	router.Get("/schedule", a.dungeonSchedule)
	router.Put("/schedule", a.saveDungeonSchedule)
	router.Post("/schedule/preview", a.previewDungeonSchedule)
	router.Get("/public", o.LocalDungeonSchedule)
	router.Put("/boss-policy", a.setBossPolicy)
	return a, router
}

func TestDungeonScheduleSavePreviewAndReload(t *testing.T) {
	a, router := newDungeonScheduleTestAPI(t)
	db, err := a.operations.storage.OpenRead()
	if err != nil {
		t.Fatal(err)
	}
	var beforeRevision, beforeCount int
	if err := db.QueryRow(`SELECT SUM(revision), COUNT(*) FROM cn_account_snapshot`).Scan(&beforeRevision, &beforeCount); err != nil {
		t.Fatal(err)
	}
	config := defaultDungeonScheduleConfig()
	config.Title = " 亚瑟的副本日程 "
	config.Description = "第一行\n第二行 <script>alert(1)</script>"
	config.Footer = "自定义页尾"
	config.Entries = []dungeonScheduleOverride{
		{Catalog: "activity", GroupID: 1, Name: " 自定义奇美拉 ", Note: "<img src=x onerror=alert(1)>"},
		{Catalog: "past", GroupID: 1, Note: "往期独立备注"},
	}
	preview := testfixture.CallContentAdmin(t, router, http.MethodPost, "/schedule/preview", map[string]any{"config": config}, 200)
	var result struct {
		State       string                 `json:"state"`
		Revision    int                    `json:"revision"`
		Config      dungeonScheduleConfig  `json:"config"`
		Entries     []dungeonScheduleEntry `json:"entries"`
		PreviewHTML string                 `json:"preview_html"`
	}
	if err := json.Unmarshal(preview, &result); err != nil {
		t.Fatal(err)
	}
	if result.State != "OK" || result.Config.Title != "亚瑟的副本日程" || !strings.Contains(result.PreviewHTML, "&lt;script&gt;") || strings.Contains(result.PreviewHTML, "<script>") || strings.Contains(result.PreviewHTML, "<img ") {
		t.Fatalf("unsafe or incomplete preview: %+v", result)
	}
	doc, err := a.operations.storage.ReadDocument(dungeonScheduleKey)
	if err != nil || doc.Revision != 0 {
		t.Fatal("preview persisted the draft", doc.Revision, err)
	}
	testfixture.CallContentAdmin(t, router, http.MethodPut, "/schedule", map[string]any{"expected_revision": 0, "config": config}, 200)
	testfixture.CallContentAdmin(t, router, http.MethodPut, "/schedule", map[string]any{"expected_revision": 0, "config": config}, 409)
	doc, err = a.operations.storage.ReadDocument(dungeonScheduleKey)
	if err != nil || doc.Revision != 1 {
		t.Fatal("draft was not durably saved", doc.Revision, err)
	}
	// New process state reads the saved independent document without modifying
	// the actual boss policy or any player snapshot.
	reloaded, err := NewOperations(a.operations.storage, nil)
	if err != nil {
		t.Fatal(err)
	}
	reloaded.dungeonScheduleGroups = a.operations.dungeonScheduleGroups
	saved, revision, err := reloaded.readDungeonScheduleConfig()
	if err != nil || revision != 1 || saved.Title != "亚瑟的副本日程" || saved.Entries[0].Name != "自定义奇美拉" {
		t.Fatal("reload lost schedule text", saved, revision, err)
	}
	policy, err := a.operations.storage.ReadDocument(teamBattlePublicationKey)
	if err != nil || policy.Revision != 0 {
		t.Fatal("presentation save changed boss publication", policy.Revision, err)
	}
	var afterRevision, afterCount, auditCount int
	if err := db.QueryRow(`SELECT SUM(revision), COUNT(*) FROM cn_account_snapshot`).Scan(&afterRevision, &afterCount); err != nil {
		t.Fatal(err)
	}
	if beforeRevision != afterRevision || beforeCount != afterCount {
		t.Fatal("presentation edit changed player snapshots")
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM cn_admin_audit WHERE operation = ?`, dungeonScheduleKey).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatal("schedule save audit mismatch", auditCount, err)
	}
	public := httptest.NewRecorder()
	reloaded.LocalDungeonSchedule(public, httptest.NewRequest(http.MethodGet, "/public", nil))
	if public.Code != 200 || public.Header().Get("Cache-Control") != "no-store" || !strings.Contains(public.Body.String(), "自定义奇美拉") || !strings.Contains(public.Body.String(), "往期独立备注") || !strings.Contains(public.Body.String(), "&lt;img") {
		t.Fatal("public page does not render the saved escaped content", public.Code, public.Body.String())
	}
}

func TestDungeonScheduleMatchesPublishedCatalogAndRetainsHiddenText(t *testing.T) {
	a, router := newDungeonScheduleTestAPI(t)
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, battleScheduleZone)
	testfixture.CallContentAdmin(t, router, http.MethodPut, "/boss-policy", map[string]any{
		"expected_revision": 0, "mode": "allowlist", "group_ids": []int{1, 2},
		"start_unix": now.Add(-24 * time.Hour).Unix(), "end_unix": now.Add(10 * 24 * time.Hour).Unix(),
		"group_schedules": []BattleGroupSchedule{
			{GroupID: 1, StartUnix: now.Add(time.Hour).Unix(), EndUnix: now.Add(24 * time.Hour).Unix(), Weekdays: []int{7}},
			{GroupID: 2, Weekdays: []int{1}},
		},
	}, 200)
	testfixture.CallContentAdmin(t, router, http.MethodPut, "/boss-policy?catalog=past", map[string]any{
		"expected_revision": 0, "mode": "allowlist", "group_ids": []int{},
	}, 200)
	config := defaultDungeonScheduleConfig()
	config.Entries = []dungeonScheduleOverride{
		{Catalog: "activity", GroupID: 1, Note: "活动备注"},
		{Catalog: "activity", GroupID: 3, Name: "关闭后保留改名", Note: "保留的关闭组备注"},
		{Catalog: "past", GroupID: 1, Note: "只属于往期的备注"},
	}
	entries, err := a.operations.dungeonScheduleEntries(config, now.UTC())
	if err != nil || len(entries) != 4 {
		t.Fatal("selected/edited group membership mismatch", entries, err)
	}
	if entries[0].Status != "未开始" || entries[0].DisplayName != "活动奇美拉" || entries[0].Note != "活动备注" || !reflect.DeepEqual(entries[0].Difficulties, []string{"超级", "超弩级"}) || !strings.Contains(entries[0].Schedule, "2026-10-04 01:00") || !strings.Contains(entries[0].Schedule, "周日") {
		t.Fatalf("upcoming entry mismatch: %+v", entries[0])
	}
	if entries[1].Status != "本日关闭" || entries[2].Published || entries[3].Published || entries[3].Note != "只属于往期的备注" {
		t.Fatal("weekday/hidden/composite identity mismatch", entries)
	}
	page, err := renderDungeonSchedule(config, entries, now)
	if err != nil || strings.Contains(page, "关闭后保留改名") || strings.Contains(page, "保留的关闭组备注") || strings.Contains(page, "只属于往期的备注") {
		t.Fatal("unpublished edited group leaked to players", page, err)
	}
	// A later Boss publish appears immediately without resaving the text page.
	testfixture.CallContentAdmin(t, router, http.MethodPut, "/boss-policy", map[string]any{
		"expected_revision": 1, "mode": "allowlist", "group_ids": []int{3},
	}, 200)
	entries, err = a.operations.dungeonScheduleEntries(config, now)
	page, pageErr := renderDungeonSchedule(config, entries, now)
	if err != nil || pageErr != nil || !strings.Contains(page, "关闭后保留改名") || !strings.Contains(page, "保留的关闭组备注") || strings.Contains(page, "活动奇美拉") {
		t.Fatal("publication changes did not immediately refresh the schedule", page, err, pageErr)
	}
}

func TestDungeonScheduleDateAndBeijingWeekdayBoundaries(t *testing.T) {
	sunday := time.Date(2026, 10, 4, 0, 0, 0, 0, battleScheduleZone)
	for _, tc := range []struct {
		now  time.Time
		want string
	}{
		{sunday.Add(-time.Second).UTC(), "未开始"},
		{sunday.UTC(), "开放中"},
		{sunday.Add(24*time.Hour - time.Second).UTC(), "开放中"},
		{sunday.Add(24 * time.Hour).UTC(), "已结束"},
	} {
		status, _ := dungeonScheduleStatus(sunday.Unix(), sunday.Add(24*time.Hour).Unix(), []int{7}, tc.now)
		if status != tc.want {
			t.Fatalf("at %s: %s, want %s", tc.now, status, tc.want)
		}
	}
	if status, _ := dungeonScheduleStatus(0, 0, []int{7}, sunday.Add(-time.Second).UTC()); status != "本日关闭" {
		t.Fatal("weekday used host timezone", status)
	}
	policy := TeamBattlePublication{StartUnix: 10, EndUnix: 30}
	start, end, days := dungeonScheduleWindow(policy, BattleGroupSchedule{StartUnix: 20, EndUnix: 40, Weekdays: []int{2}})
	if start != 20 || end != 30 || !reflect.DeepEqual(days, []int{2}) {
		t.Fatal("directory and group windows are not intersected", start, end, days)
	}
	if status, _ := dungeonScheduleStatus(40, 30, nil, sunday); status != "排期无交集" {
		t.Fatal("impossible schedule was shown open", status)
	}
}

func TestDungeonScheduleRejectsInvalidTextAndRemoteSave(t *testing.T) {
	a, router := newDungeonScheduleTestAPI(t)
	for _, config := range []dungeonScheduleConfig{
		{Title: "", Entries: []dungeonScheduleOverride{}},
		{Title: strings.Repeat("日", 81), Entries: []dungeonScheduleOverride{}},
		{Title: "日程表", Entries: nil},
		{Title: "日程表", Description: strings.Repeat("字", 4001), Entries: []dungeonScheduleOverride{}},
		{Title: "日程表", Entries: []dungeonScheduleOverride{{Catalog: "bad", GroupID: 1}}},
		{Title: "日程表", Entries: []dungeonScheduleOverride{{Catalog: "activity", GroupID: 99}}},
		{Title: "日程表", Entries: []dungeonScheduleOverride{{Catalog: "activity", GroupID: 1}, {Catalog: "activity", GroupID: 1}}},
		{Title: "日程表", Entries: []dungeonScheduleOverride{{Catalog: "activity", GroupID: 1, Note: strings.Repeat("字", 1001)}}},
	} {
		testfixture.CallContentAdmin(t, router, http.MethodPut, "/schedule", map[string]any{"expected_revision": 0, "config": config}, 400)
	}
	request := httptest.NewRequest(http.MethodPut, "/schedule", strings.NewReader(`{}`))
	request.RemoteAddr = "192.168.1.12:12345"
	w := httptest.NewRecorder()
	a.saveDungeonSchedule(w, request)
	if w.Code != http.StatusForbidden {
		t.Fatal("remote client can mutate page content", w.Code)
	}
	doc, err := a.operations.storage.ReadDocument(dungeonScheduleKey)
	if err != nil || doc.Revision != 0 {
		t.Fatal("invalid request persisted content", doc, err)
	}
	if _, err := a.operations.storage.WriteDocument(dungeonScheduleKey, -1, defaultDungeonScheduleConfig(), dungeonScheduleKey); err != accountstore.ErrDocumentConflict {
		t.Fatal("negative revision unexpectedly accepted", err)
	}
}
