package admin

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/testfixture"
)

func TestNoticePublicationOnlyPublicTextChanges(t *testing.T) {
	o := noticeTestStore(t)
	a := &API{operations: o}
	r := chi.NewRouter()
	r.Put("/policy", a.savePlayerPolicy)
	p := o.playerPolicy.Load().Value
	check := func(want int, enabled bool) {
		t.Helper()
		revision := o.playerPolicy.Load().Revision
		p.Notice.PublicationRevision = 999999 // Client cannot choose publication IDs.
		testfixture.CallContentAdmin(t, r, "PUT", "/policy", map[string]any{"expected_revision": revision, "config": p}, 200)
		n := o.playerPolicy.Load().Runtime.Notice
		if n.Revision != want || n.Enabled != enabled {
			t.Fatalf("publication = %d/%v, want %d/%v", n.Revision, n.Enabled, want, enabled)
		}
		encoded, _ := json.Marshal(o.playerPolicy.Load().Value)
		if err := json.Unmarshal(encoded, &p); err != nil {
			t.Fatal(err)
		}
	}
	p.StoryCrystals++
	check(1, true)
	p.Notice.Entries = append(p.Notice.entries(), noticeEntry{Title: "草稿", Body: "未公开"})
	check(1, true)
	p.Notice.Entries[0].Pinned = true
	check(1, true)
	p.Notice.Entries[1].Enabled = true
	check(5, true)
	p.Notice.Entries[0], p.Notice.Entries[1] = p.Notice.Entries[1], p.Notice.Entries[0]
	check(5, true)
	p.Notice.Entries[0].Body = "更新正文"
	check(7, true)
	p.Notice.Enabled = false
	check(8, false)
	p.Notice.Enabled = true
	check(9, true)
	if err := o.loadPlayerPolicy(); err != nil {
		t.Fatal(err)
	}
	if o.playerPolicy.Load().Runtime.Notice.Revision != 9 {
		t.Fatal("publication changed on reload")
	}
	p.Notice.Entries = []noticeEntry{}
	check(10, false)
}

func TestNoticeReadClaimRejectsTampering(t *testing.T) {
	o := noticeTestStore(t)
	q := url.Values{"user": {"1000001"}, "revision": {"1"}, "token": {game.NoticeReadToken(o.noticeSigningKey, 1000001, 1)}}
	claim := func(q url.Values) bool {
		_, _, ok := o.NoticeReadClaim(httptest.NewRequest("GET", "/disabled/web/auto?"+q.Encode(), nil))
		return ok
	}
	if !claim(q) {
		t.Fatal("valid notice claim rejected")
	}
	for _, field := range []string{"user", "revision", "token"} {
		old := q.Get(field)
		q.Set(field, strconv.Itoa(12345))
		if claim(q) {
			t.Fatalf("tampered %s accepted", field)
		}
		q.Set(field, old)
	}
}
