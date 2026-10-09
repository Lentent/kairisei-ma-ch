package admin

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/testfixture"
)

func noticeTestStore(t *testing.T) *Operations {
	t.Helper()
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	o, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	o.playerDefaults = &PlayerPolicy{Notice: noticePolicy{Enabled: true, Title: "旧公告", Body: "原有公告正文"}, StoryCrystals: 50}
	if err := o.loadPlayerPolicy(); err != nil {
		t.Fatal(err)
	}
	return o
}

func noticeTestPage(o *Operations) string {
	w := httptest.NewRecorder()
	o.LocalNotice(w, httptest.NewRequest("GET", "/disabled/web", nil))
	return w.Body.String()
}

func TestNoticesLegacySaveRestartVisibilityAndDeleteAll(t *testing.T) {
	o := noticeTestStore(t)
	legacy := map[string]any{"notice": map[string]any{"enabled": true, "title": "旧公告", "body": "原有公告正文"}, "story_first_clear_crystals": 50}
	if _, err := o.writeDocument(playerPolicyKey, 0, legacy); err != nil {
		t.Fatal(err)
	}
	before, _ := o.storage.ReadDocument(playerPolicyKey)
	if err := o.loadPlayerPolicy(); err != nil || !strings.Contains(noticeTestPage(o), "原有公告正文") {
		t.Fatal("legacy notice did not load", err)
	}
	after, _ := o.storage.ReadDocument(playerPolicyKey)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("reading legacy data rewrote the database")
	}
	a := &API{operations: o}
	r := chi.NewRouter()
	r.Put("/policy", a.savePlayerPolicy)
	config := o.playerPolicy.Load().Value
	config.Notice.Entries = []noticeEntry{
		{Enabled: true, Title: "普通一", Body: "普通一正文\n保留换行"},
		{Enabled: false, Pinned: true, Title: "隐藏标题", Body: "隐藏正文"},
		{Enabled: true, Pinned: true, Title: "置顶一", Body: "置顶一正文"},
		{Enabled: true, Pinned: true, Title: "置顶二", Body: "置顶二正文"},
		{Enabled: true, Title: "旧公告", Body: "原有公告正文"},
	}
	testfixture.CallContentAdmin(t, r, "PUT", "/policy", map[string]any{"expected_revision": 1, "config": config}, 200)
	testfixture.CallContentAdmin(t, r, "PUT", "/policy", map[string]any{"expected_revision": 1, "config": config}, 409)
	// Recreate Operations, sharing only the durable database and defaults.
	restarted, err := NewOperations(o.storage, nil)
	if err != nil {
		t.Fatal(err)
	}
	restarted.playerDefaults = o.playerDefaults
	if err := restarted.loadPlayerPolicy(); err != nil {
		t.Fatal(err)
	}
	page := noticeTestPage(restarted)
	config.Notice.PublicationRevision = 3
	if !reflect.DeepEqual(restarted.playerPolicy.Load().Value, config) {
		t.Fatal("reload or rendering changed the saved order or sibling settings")
	}
	previous := -1
	for _, body := range []string{"置顶一正文", "置顶二正文", "普通一正文\n保留换行", "原有公告正文"} {
		index := strings.Index(page, body)
		if index <= previous {
			t.Fatalf("body missing, collapsed, or out of order: %s", body)
		}
		previous = index
	}
	if strings.Contains(page, "隐藏") || strings.Contains(page, "<nav") || strings.Contains(page, "返回公告目录") || strings.Contains(page, "<details") {
		t.Fatal("hidden content leaked, unwanted directory navigation present, or body requires expansion")
	}
	config.Notice.Enabled = false
	testfixture.CallContentAdmin(t, r, "PUT", "/policy", map[string]any{"expected_revision": 2, "config": config}, 200)
	if !strings.Contains(noticeTestPage(o), "暂无公告") || strings.Contains(noticeTestPage(o), "正文") {
		t.Fatal("global switch did not hide all notices")
	}
	config.Notice.Enabled = true
	config.Notice.Entries = []noticeEntry{}
	testfixture.CallContentAdmin(t, r, "PUT", "/policy", map[string]any{"expected_revision": 3, "config": config}, 200)
	if err := restarted.loadPlayerPolicy(); err != nil || restarted.playerPolicy.Load().Value.Notice.Entries == nil || !strings.Contains(noticeTestPage(restarted), "暂无公告") || strings.Contains(noticeTestPage(restarted), "原有公告正文") {
		t.Fatal("deleting all entries revived the legacy notice", err)
	}
}

func TestNoticesEscapingAndAllHidden(t *testing.T) {
	o := noticeTestStore(t)
	p := *o.playerDefaults
	p.Notice.Entries = []noticeEntry{{Enabled: true, Title: `<img src=x onerror=alert(1)>`, Body: `<script>alert(1)</script>`}}
	o.playerPolicy.Store(o.playerSnapshot(p, 1))
	page := noticeTestPage(o)
	if strings.Contains(page, "<script>") || strings.Contains(page, "<img ") || !strings.Contains(page, "&lt;script&gt;") {
		t.Fatal("plain text was rendered as executable markup")
	}
	p.Notice.Entries = []noticeEntry{{Title: "未发布", Body: "不可见正文", Pinned: true}}
	o.playerPolicy.Store(o.playerSnapshot(p, 2))
	if page = noticeTestPage(o); !strings.Contains(page, "暂无公告") || strings.Contains(page, "不可见") {
		t.Fatal("unpublished content leaked to the public page")
	}
}

func TestNoticesValidationAndFullCapacitySave(t *testing.T) {
	o := noticeTestStore(t)
	a := &API{operations: o}
	r := chi.NewRouter()
	r.Put("/policy", a.savePlayerPolicy)
	for _, tc := range []struct {
		name    string
		entries []noticeEntry
	}{
		{"blank title", []noticeEntry{{Enabled: true, Title: "  ", Body: "正文"}}},
		{"long title", []noticeEntry{{Title: strings.Repeat("字", 81)}}},
		{"blank published body", []noticeEntry{{Enabled: true, Title: "标题", Body: " \n"}}},
		{"long body", []noticeEntry{{Title: "标题", Body: strings.Repeat("字", 8001)}}},
		{"too many", make([]noticeEntry, maxNotices+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := *o.playerDefaults
			p.Notice.Entries = tc.entries
			testfixture.CallContentAdmin(t, r, "PUT", "/policy", map[string]any{"expected_revision": 0, "config": p}, 400)
			if o.playerPolicy.Load().Revision != 0 {
				t.Fatal("invalid input changed the active document")
			}
		})
	}
	p := *o.playerDefaults
	p.Notice.Entries = make([]noticeEntry, maxNotices)
	for i := range p.Notice.Entries {
		p.Notice.Entries[i] = noticeEntry{Enabled: true, Title: fmt.Sprintf("公告 %d", i+1), Body: strings.Repeat("<", 8000)}
	}
	payload := map[string]any{"expected_revision": 0, "config": p}
	encoded, _ := json.Marshal(payload)
	if len(encoded) <= 256*1024 {
		t.Fatal("test did not exceed the previous request limit")
	}
	testfixture.CallContentAdmin(t, r, "PUT", "/policy", payload, 200)
	if err := o.loadPlayerPolicy(); err != nil || len(o.playerPolicy.Load().Value.Notice.Entries) != maxNotices {
		t.Fatal("full-capacity document did not survive reload", err)
	}
}
