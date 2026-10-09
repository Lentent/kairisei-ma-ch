package cnbootstrap

import (
	"bytes"
	"encoding/json"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	adminapi "kairisei.local/server/internal/admin"
	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/testfixture"
)

type scheduleTestOverride struct {
	Catalog string `json:"catalog"`
	GroupID int    `json:"group_id"`
	Name    string `json:"name"`
	Note    string `json:"note"`
}

type scheduleTestConfig struct {
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Footer      string                 `json:"footer"`
	Entries     []scheduleTestOverride `json:"entries"`
}

type scheduleTestEntry struct {
	Catalog     string `json:"catalog"`
	GroupID     int    `json:"group_id"`
	DisplayName string `json:"display_name"`
	Note        string `json:"note"`
	Schedule    string `json:"schedule"`
	StatusCode  string `json:"status_code"`
	Published   bool   `json:"published"`
	StartUnix   int64  `json:"start_unix"`
	EndUnix     int64  `json:"end_unix"`
	Weekdays    []int  `json:"weekdays"`
}

type scheduleTestResponse struct {
	State       string              `json:"state"`
	Revision    int                 `json:"revision"`
	Config      scheduleTestConfig  `json:"config"`
	Defaults    scheduleTestConfig  `json:"defaults"`
	Entries     []scheduleTestEntry `json:"entries"`
	PreviewHTML string              `json:"preview_html"`
}

// Uses the existing opt-in, immutable resource fixture. Every document, account,
// log and uploaded asset belongs to root; no player's database is opened.
func newDungeonScheduleTestConfig(t *testing.T, root string) Config {
	t.Helper()
	resourceRoot := os.Getenv("CN602_RUNTIME_SET")
	if resourceRoot == "" {
		t.Skip("set CN602_RUNTIME_SET to the complete resource fixture")
	}
	if !filepath.IsAbs(resourceRoot) || !filepath.IsAbs(root) {
		t.Fatal("absolute resource and writable test roots required")
	}
	content, err := os.ReadFile(filepath.Join(resourceRoot, "resource-set.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Entrypoints map[string]string `json:"entrypoints"`
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatal(err)
	}
	entrypoint := func(key string) string {
		t.Helper()
		path := manifest.Entrypoints[key]
		if path == "" {
			t.Fatalf("missing resource entrypoint %s", key)
		}
		return filepath.Join(resourceRoot, filepath.FromSlash(path))
	}
	requestLog := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(requestLog, nil, 0600); err != nil {
		t.Fatal(err)
	}
	return Config{
		Persistence: PersistenceConfig{
			RequestLog: requestLog,
			SavePath:   filepath.Join(root, "save.json"),
			SeedPath:   entrypoint("cn-save-seed"),
		},
		Resources: ResourcesConfig{
			AssetMap:            entrypoint("cn-asset-map"),
			GachaBanner:         entrypoint("cn-gacha-banner"),
			FiveStarGachaBanner: entrypoint("cn-five-star-gacha-banner"),
			HomeBanner:          entrypoint("cn-home-banner"),
			CPKRoot:             entrypoint("cn-cpk-root"),
			ImageRoot:           entrypoint("cn-image-root"),
			CPKAliases:          entrypoint("cn-cpk-aliases"),
			PatchRoots:          []string{entrypoint("cn-patch-root")},
		},
		Masters: MastersConfig{
			Cards:             entrypoint("cn-card-master"),
			Explore:           entrypoint("cn-explore-master"),
			Story:             entrypoint("cn-story-master"),
			Battle:            entrypoint("cn-battle-master"),
			Navi:              entrypoint("cn-navi-master"),
			Items:             entrypoint("cn-item-master"),
			Avatar:            entrypoint("cn-avatar-master"),
			Stamps:            entrypoint("cn-stamp-master"),
			Honors:            entrypoint("cn-honor-master"),
			PVP:               entrypoint("cn-pvp-master"),
			PlayerProgression: entrypoint("cn-player-progression"),
			LoginBonus:        entrypoint("cn-login-bonus"),
		},
		Network: NetworkConfig{
			AdvertiseHost: "127.0.0.1",
			HTTPPort:      26020,
			BattleSV:      multiplayer.Endpoint{Host: "127.0.0.1", Port: 26021},
		},
		Multiplayer: multiplayer.NewHub(),
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestDungeonScheduleProductionEditingAndPublication(t *testing.T) {
	config := newDungeonScheduleTestConfig(t, t.TempDir())
	handler, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if handler != nil {
			if err := handler.(io.Closer).Close(); err != nil {
				t.Error(err)
			}
		}
	})
	admin := handler.(interface{ AdminHandler() http.Handler }).AdminHandler()
	read := func() scheduleTestResponse {
		t.Helper()
		var result scheduleTestResponse
		body := testfixture.CallContentAdmin(t, admin, http.MethodGet, "/api/dungeon-schedule", nil, http.StatusOK)
		if err := json.Unmarshal(body, &result); err != nil || result.State != "OK" {
			t.Fatalf("read schedule: %s; error=%v", body, err)
		}
		return result
	}
	page := func() string {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://local/disabled/web/information/2015/7/kechengbiao?userid=1000001&type_id=1", nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), "text/html") || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("public schedule: status=%d headers=%v body=%.500s", response.Code, response.Header(), response.Body.String())
		}
		return response.Body.String()
	}
	groups := func(catalog string) []adminapi.AdminBattleGroup {
		t.Helper()
		var result struct {
			Groups []adminapi.AdminBattleGroup `json:"groups"`
		}
		body := testfixture.CallContentAdmin(t, admin, http.MethodGet, "/api/boss-groups?catalog="+catalog, nil, http.StatusOK)
		if err := json.Unmarshal(body, &result); err != nil || len(result.Groups) < 3 {
			t.Fatalf("catalog %s: groups=%d error=%v", catalog, len(result.Groups), err)
		}
		return result.Groups
	}
	initial := read()
	if initial.Revision != 0 || initial.Config.Title != "副本日程表" || len(initial.Entries) == 0 {
		t.Fatalf("unexpected initial schedule: revision=%d title=%q entries=%d", initial.Revision, initial.Config.Title, len(initial.Entries))
	}
	activity, past := groups("activity"), groups("past")
	draft := initial.Config
	draft.Title = "本周副本 <日程>"
	draft.Description = "活动说明第一行\n活动说明第二行 <script>alert('schedule')</script>"
	draft.Footer = "页尾说明：请以当前发布排期为准。"
	draft.Entries = []scheduleTestOverride{
		{Catalog: "activity", GroupID: activity[0].GroupID, Name: "编辑后的活动Boss", Note: "活动备注第一行\n活动备注第二行"},
		{Catalog: "activity", GroupID: activity[1].GroupID, Name: "即将开始的Boss", Note: "稍后开放"},
		{Catalog: "activity", GroupID: activity[2].GroupID, Name: "未发布Boss不得展示", Note: "保留草稿文字"},
		{Catalog: "past", GroupID: past[0].GroupID, Name: "编辑后的往期Boss", Note: "往期独立备注"},
	}
	var preview scheduleTestResponse
	previewBody := testfixture.CallContentAdmin(t, admin, http.MethodPost, "/api/dungeon-schedule/preview", map[string]any{"config": draft}, http.StatusOK)
	if err := json.Unmarshal(previewBody, &preview); err != nil || preview.State != "OK" || !strings.Contains(preview.PreviewHTML, html.EscapeString(draft.Title)) {
		t.Fatalf("preview: %.500s; error=%v", previewBody, err)
	}
	if after := read(); after.Revision != 0 || !reflect.DeepEqual(after.Config, initial.Config) || strings.Contains(page(), "编辑后的活动Boss") {
		t.Fatal("draft preview changed the published page")
	}
	mutation := map[string]any{"expected_revision": initial.Revision, "config": draft}
	for _, gate := range []struct {
		remote string
		action string
	}{
		{remote: "127.0.0.1:12345"},
		{remote: "192.0.2.1:12345", action: "apply"},
	} {
		body, err := json.Marshal(mutation)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPut, "http://localhost/api/dungeon-schedule", bytes.NewReader(body))
		request.RemoteAddr = gate.remote
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Kairisei-Admin-Action", gate.action)
		response := httptest.NewRecorder()
		admin.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("mutation gate accepted remote=%q action=%q: %d %s", gate.remote, gate.action, response.Code, response.Body.String())
		}
	}
	testfixture.CallContentAdmin(t, admin, http.MethodPut, "/api/dungeon-schedule", map[string]any{"config": draft}, http.StatusBadRequest)
	testfixture.CallContentAdmin(t, admin, http.MethodPut, "/api/dungeon-schedule", mutation, http.StatusOK)
	testfixture.CallContentAdmin(t, admin, http.MethodPut, "/api/dungeon-schedule", mutation, http.StatusConflict)
	saved := read()
	if saved.Revision != 1 || !reflect.DeepEqual(saved.Config, draft) || !reflect.DeepEqual(saved.Defaults, initial.Defaults) {
		t.Fatal("published content or default reset values differ from the submitted draft")
	}
	assertText := func(body string) {
		t.Helper()
		for _, text := range []string{draft.Title, draft.Description, draft.Footer, draft.Entries[0].Name, draft.Entries[0].Note, draft.Entries[3].Name, draft.Entries[3].Note} {
			if !strings.Contains(body, html.EscapeString(text)) {
				t.Fatalf("page is missing saved text %q", text)
			}
		}
		if strings.Contains(body, "<script>alert") {
			t.Fatal("page executes editable text as HTML")
		}
	}
	assertText(page())

	// Selected groups retain upcoming/weekly rows in the timetable; only groups
	// admitted by the same policy are playable now. An override cannot publish one.
	now := time.Now()
	today := (int(now.In(time.FixedZone("test-beijing", 8*60*60)).Weekday())+6)%7 + 1
	tomorrow := today%7 + 1
	start, end := now.Unix()-7200, now.Unix()+7200
	testfixture.CallContentAdmin(t, admin, http.MethodPut, "/api/boss-policy?catalog=activity", map[string]any{
		"expected_revision": 0, "mode": "allowlist", "group_ids": []int{activity[0].GroupID, activity[1].GroupID},
		"start_unix": start, "end_unix": end,
		"group_schedules": []adminapi.BattleGroupSchedule{
			{GroupID: activity[0].GroupID, Weekdays: []int{today}},
			{GroupID: activity[1].GroupID, StartUnix: now.Unix() + 3600, EndUnix: now.Unix() + 10800},
			{GroupID: activity[2].GroupID, Weekdays: []int{today}},
		},
	}, http.StatusOK)
	testfixture.CallContentAdmin(t, admin, http.MethodPut, "/api/boss-policy?catalog=past", map[string]any{
		"expected_revision": 0, "mode": "allowlist", "group_ids": []int{past[0].GroupID},
		"group_schedules": []adminapi.BattleGroupSchedule{{GroupID: past[0].GroupID, Weekdays: []int{tomorrow}}},
	}, http.StatusOK)
	current := read()
	if current.Revision != saved.Revision || !reflect.DeepEqual(current.Config, saved.Config) || len(current.Entries) != 4 {
		t.Fatalf("boss publication changed page content or retained unselected rows: revision=%d entries=%d", current.Revision, len(current.Entries))
	}
	entry := func(catalog string, id int) scheduleTestEntry {
		t.Helper()
		for _, value := range current.Entries {
			if value.Catalog == catalog && value.GroupID == id {
				return value
			}
		}
		t.Fatalf("missing schedule identity %s/%d", catalog, id)
		return scheduleTestEntry{}
	}
	open, upcoming := entry("activity", activity[0].GroupID), entry("activity", activity[1].GroupID)
	closed, unpublished := entry("past", past[0].GroupID), entry("activity", activity[2].GroupID)
	if !open.Published || open.StatusCode != "open" || !reflect.DeepEqual(open.Weekdays, []int{today}) || open.StartUnix != start || open.EndUnix != end {
		t.Fatalf("open schedule differs from directory/weekday policy: %+v", open)
	}
	if !upcoming.Published || upcoming.StatusCode != "upcoming" || upcoming.StartUnix != now.Unix()+3600 || upcoming.EndUnix != end {
		t.Fatalf("effective schedule did not intersect directory/group windows: %+v", upcoming)
	}
	if !closed.Published || closed.StatusCode != "closed" || !reflect.DeepEqual(closed.Weekdays, []int{tomorrow}) || unpublished.Published || unpublished.StatusCode != "unpublished" {
		t.Fatalf("weekly or allowlist state diverged: closed=%+v unpublished=%+v", closed, unpublished)
	}
	public := page()
	assertText(public)
	for _, value := range []scheduleTestEntry{open, upcoming, closed} {
		if !strings.Contains(public, html.EscapeString(value.Schedule)) {
			t.Fatalf("public page is missing effective schedule %q", value.Schedule)
		}
	}
	if strings.Contains(public, draft.Entries[2].Name) || strings.Count(public, `<article class="card">`) != 3 {
		t.Fatal("public page exposed an unpublished override or stale catalog rows")
	}
	deployment := handler.(*cnDeploymentHandler)
	allowed, err := deployment.operations.TeamBattleGroupAllowlist()
	if err != nil || len(allowed) != 1 {
		t.Fatalf("live activity allowlist differs from schedule: %v %v", allowed, err)
	}
	if _, ok := allowed[open.GroupID]; !ok {
		t.Fatal("open timetable group is missing from the live allowlist")
	}
	pastAllowed, err := deployment.operations.BattleGroupAllowlist(adminapi.PastBattlePublicationKey)
	if err != nil || len(pastAllowed) != 0 || pastAllowed == nil {
		t.Fatalf("closed past weekday still admitted gameplay: %v %v", pastAllowed, err)
	}

	if err := handler.(io.Closer).Close(); err != nil {
		t.Fatal(err)
	}
	handler = nil
	config.Multiplayer = multiplayer.NewHub()
	handler, err = New(config)
	if err != nil {
		t.Fatal(err)
	}
	admin = handler.(interface{ AdminHandler() http.Handler }).AdminHandler()
	reloaded := read()
	if reloaded.Revision != current.Revision || !reflect.DeepEqual(reloaded.Config, current.Config) || !reflect.DeepEqual(reloaded.Entries, current.Entries) {
		t.Fatal("handler rebuild lost saved content, catalog overrides or boss publication")
	}
	assertText(page())
	if strings.Contains(page(), draft.Entries[2].Name) {
		t.Fatal("unpublished override appeared after restart")
	}
}
