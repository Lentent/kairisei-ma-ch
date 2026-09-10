package cnbootstrap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestPastPublicationIndependentAndReloaded(t *testing.T) {
	accounts := newFriendCapacityTestAccounts(t)
	ops, err := newCNOperationStore(accounts.storage, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := &cnAdmin{operations: ops, knownGroups: map[int]struct{}{1: {}}, pastGroups: []cnAdminBattleGroup{{GroupID: 2}, {GroupID: 3}}}
	router := chi.NewRouter()
	router.Put("/policy", a.setBossPolicy)
	router.Get("/policy", a.bossPolicy)
	callContentAdmin(t, router, "PUT", "/policy", map[string]any{"mode": "allowlist", "group_ids": []int{1}, "expected_revision": 0}, 200)
	callContentAdmin(t, router, "PUT", "/policy?catalog=past", map[string]any{"mode": "allowlist", "group_ids": []int{1}, "expected_revision": 0}, 400)
	callContentAdmin(t, router, "PUT", "/policy?catalog=past", map[string]any{"mode": "allowlist", "group_ids": []int{3}, "expected_revision": 0}, 200)
	callContentAdmin(t, router, "PUT", "/policy?catalog=past", map[string]any{"mode": "all", "expected_revision": 0}, 409)
	callContentAdmin(t, router, "GET", "/policy?catalog=unknown", nil, 400)
	ops, err = newCNOperationStore(accounts.storage, nil)
	if err != nil {
		t.Fatal(err)
	}
	activity, err := ops.teamBattleGroupAllowlist()
	if err != nil || len(activity) != 1 {
		t.Fatalf("activity policy: %v %v", activity, err)
	}
	if _, ok := activity[1]; !ok {
		t.Fatal("past publish changed activity policy")
	}
	business := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/TeamBattlePastBossShow" {
			t.Fatal("wrong forwarding route")
		}
		_, _ = w.Write([]byte("{\"0\":0}\n{\"0\":[{\"0\":2,\"5\":1},{\"0\":3,\"5\":7}],\"1\":[{\"0\":42}],\"2\":0,\"3\":0}\n{}"))
	})
	show := func(wantCount int) {
		t.Helper()
		r := httptest.NewRequest("POST", "/TeamBattlePastBossShow", strings.NewReader("test-session-00000000"))
		r = r.WithContext(context.WithValue(r.Context(), cnAuthenticatedUserKey{}, cnPrimaryUserID))
		w := httptest.NewRecorder()
		cnBootstrapPastBossShow(business, ops)(w, r)
		var result struct {
			Groups []struct {
				ID     int `json:"0"`
				Period int `json:"5"`
			} `json:"0"`
			Partners []map[string]int `json:"1"`
		}
		lines := strings.Split(w.Body.String(), "\n")
		if w.Code != 200 || len(lines) != 3 {
			t.Fatalf("bad native response: %s", w.Body.String())
		}
		if err := json.Unmarshal([]byte(lines[1]), &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Groups) != wantCount || len(result.Partners) != 1 || result.Partners[0]["0"] != 42 {
			t.Fatalf("response lost catalog or partners: %s", w.Body.String())
		}
		if wantCount == 1 && (result.Groups[0].ID != 3 || result.Groups[0].Period != 7) {
			t.Fatal("archive identity or period changed")
		}
	}
	show(1)
	revision := 1
	if _, err := ops.setBattlePublication(cnPastBattlePublicationKey, cnTeamBattlePublication{Mode: "all", EndUnix: time.Now().Unix() - 1, ExpectedRevision: &revision}); err != nil {
		t.Fatal(err)
	}
	show(0)
}

func auditCompletePastAdmin(t *testing.T, handler http.Handler) {
	t.Helper()
	admin := handler.(interface{ AdminHandler() http.Handler }).AdminHandler()
	var past struct {
		Groups []cnAdminBattleGroup `json:"groups"`
	}
	if err := json.Unmarshal(callContentAdmin(t, admin, "GET", "/api/boss-groups?catalog=past", nil, 200), &past); err != nil {
		t.Fatal(err)
	}
	var drops struct {
		Bosses []cnDropBoss `json:"bosses"`
	}
	if err := json.Unmarshal(callContentAdmin(t, admin, "GET", "/api/boss-drops", nil, 200), &drops); err != nil {
		t.Fatal(err)
	}
	byID := map[int]cnDropBoss{}
	for _, b := range drops.Bosses {
		byID[b.BossID] = b
	}
	count := 0
	if len(past.Groups) == 0 {
		t.Fatal("empty past admin catalog")
	}
	for _, g := range past.Groups {
		if g.PastName == "" || len(g.BossIDs) == 0 {
			t.Fatalf("invalid past group %+v", g)
		}
		for _, id := range g.BossIDs {
			if byID[id].Category != "past" {
				t.Fatalf("past boss %d missing drop editor", id)
			}
			count++
		}
	}
	t.Logf("past admin: %d groups / %d difficulties, all connected to shared drop editor", len(past.Groups), count)
}
