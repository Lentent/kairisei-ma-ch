package admin

import (
	"testing"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/testfixture"
)

func TestPastPublicationIndependentAndReloaded(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	ops, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	a := &API{operations: ops, knownGroups: map[int]struct{}{1: {}}, pastGroups: []AdminBattleGroup{{GroupID: 2}, {GroupID: 3}}}
	router := chi.NewRouter()
	router.Put("/policy", a.setBossPolicy)
	router.Get("/policy", a.bossPolicy)
	testfixture.CallContentAdmin(t, router, "PUT", "/policy", map[string]any{"mode": "allowlist", "group_ids": []int{1}, "expected_revision": 0}, 200)
	testfixture.CallContentAdmin(t, router, "PUT", "/policy?catalog=past", map[string]any{"mode": "allowlist", "group_ids": []int{1}, "expected_revision": 0}, 400)
	testfixture.CallContentAdmin(t, router, "PUT", "/policy?catalog=past", map[string]any{"mode": "allowlist", "group_ids": []int{3}, "expected_revision": 0}, 200)
	testfixture.CallContentAdmin(t, router, "PUT", "/policy?catalog=past", map[string]any{"mode": "all", "expected_revision": 0}, 409)
	testfixture.CallContentAdmin(t, router, "GET", "/policy?catalog=unknown", nil, 400)
	ops, err = NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	activity, err := ops.TeamBattleGroupAllowlist()
	if err != nil || len(activity) != 1 {
		t.Fatalf("activity policy: %v %v", activity, err)
	}
	if _, ok := activity[1]; !ok {
		t.Fatal("past publish changed activity policy")
	}
	a.operations = ops
	testfixture.CallContentAdmin(t, router, "PUT", "/policy", map[string]any{"mode": "allowlist", "group_ids": []int{}, "expected_revision": 1}, 200)
	closed, err := ops.TeamBattleGroupAllowlist()
	if err != nil || closed == nil || len(closed) != 0 {
		t.Fatalf("closing all groups did not produce an empty allowlist: %v %v", closed, err)
	}
}
