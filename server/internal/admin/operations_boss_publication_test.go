package admin

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

func TestBossWeeklyPublication(t *testing.T) {
	for _, mode := range []string{"all", "allowlist"} {
		for day := 1; day <= 7; day++ {
			p := TeamBattlePublication{Mode: mode, GroupIDs: []int{1, 2, 3}, GroupSchedules: []BattleGroupSchedule{
				{GroupID: 1, Weekdays: []int{1, 3, 5, 7}},
				{GroupID: 2, Weekdays: []int{2, 4, 6, 7}},
			}}
			now := time.Date(2026, 9, 27+day, 12, 0, 0, 0, battleScheduleZone)
			allowed, err := battlePublicationAllowlist(p, []int{1, 2, 3}, now)
			if err != nil {
				t.Fatal(err)
			}
			for id, want := range map[int]bool{1: day%2 == 1, 2: day%2 == 0 || day == 7, 3: true} {
				_, open := allowed[id]
				if allowed == nil {
					open = true
				}
				if open != want {
					t.Fatalf("mode=%s day=%d group=%d open=%v want=%v", mode, day, id, open, want)
				}
			}
		}
	}
	// A weekday rule never opens a group omitted from the allowlist.
	p := TeamBattlePublication{Mode: "allowlist", GroupIDs: []int{1}, GroupSchedules: []BattleGroupSchedule{{GroupID: 2, Weekdays: []int{7}}}}
	allowed, err := battlePublicationAllowlist(p, []int{1, 2}, time.Date(2026, 10, 4, 0, 0, 0, 0, battleScheduleZone))
	if err != nil || len(allowed) != 1 {
		t.Fatalf("weekday bypassed allowlist: %v %v", allowed, err)
	}
	if _, open := allowed[2]; open {
		t.Fatal("unselected group was opened")
	}
}

func TestBossPublicationDateBoundaries(t *testing.T) {
	start := time.Date(2026, 10, 4, 0, 0, 0, 0, battleScheduleZone)
	s := BattleGroupSchedule{GroupID: 1, StartUnix: start.Unix(), EndUnix: start.Add(24 * time.Hour).Unix(), Weekdays: []int{7}}
	for _, tc := range []struct {
		now  time.Time
		want bool
	}{
		{start.Add(-time.Second).UTC(), false},
		{start.UTC(), true}, // Saturday 16:00 UTC is Sunday in Beijing.
		{start.Add(24*time.Hour - time.Second).UTC(), true},
		{start.Add(24 * time.Hour).UTC(), false},
	} {
		if got := s.active(tc.now); got != tc.want {
			t.Fatalf("at %s open=%v want=%v", tc.now, got, tc.want)
		}
	}
	// Weekday boundary also applies without any start/end restriction.
	s.StartUnix, s.EndUnix = 0, 0
	if s.active(start.Add(-time.Second).UTC()) || !s.active(start.UTC()) || s.active(start.Add(24*time.Hour).UTC()) {
		t.Fatal("weekday did not switch at Beijing midnight")
	}
	for _, mode := range []string{"all", "allowlist"} {
		p := TeamBattlePublication{Mode: mode, GroupIDs: []int{1}, StartUnix: start.Add(time.Hour).Unix(), GroupSchedules: []BattleGroupSchedule{s}}
		allowed, err := battlePublicationAllowlist(p, []int{1}, start)
		if err != nil || allowed == nil || len(allowed) != 0 {
			t.Fatalf("group schedule bypassed directory start: %v %v", allowed, err)
		}
		p.StartUnix, p.EndUnix = 0, start.Unix()
		allowed, err = battlePublicationAllowlist(p, []int{1}, start)
		if err != nil || allowed == nil || len(allowed) != 0 {
			t.Fatalf("group schedule bypassed directory end: %v %v", allowed, err)
		}
	}
	allowed, err := battlePublicationAllowlist(TeamBattlePublication{Mode: "all"}, []int{1}, start)
	if err != nil || allowed != nil {
		t.Fatal("legacy unrestricted default changed")
	}
}

func TestBossGroupScheduleValidationAndReload(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	ops, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	a := &API{operations: ops, knownGroups: map[int]struct{}{1: {}, 2: {}}, pastGroups: []AdminBattleGroup{{GroupID: 3}}}
	router := chi.NewRouter()
	router.Put("/policy", a.setBossPolicy)
	router.Get("/policy", a.bossPolicy)
	for _, schedules := range [][]BattleGroupSchedule{
		{{GroupID: 99}}, {{GroupID: 0}}, {{GroupID: 1}, {GroupID: 1}},
		{{GroupID: 1, Weekdays: []int{0}}}, {{GroupID: 1, Weekdays: []int{8}}},
		{{GroupID: 1, StartUnix: -1}}, {{GroupID: 1, EndUnix: -1}},
		{{GroupID: 1, StartUnix: 10, EndUnix: 10}}, {{GroupID: 1, StartUnix: 20, EndUnix: 10}},
	} {
		testfixture.CallContentAdmin(t, router, "PUT", "/policy", map[string]any{"mode": "all", "expected_revision": 0, "group_schedules": schedules}, 400)
	}
	schedules := []BattleGroupSchedule{{GroupID: 2, Weekdays: []int{7, 6, 4, 2, 7}}, {GroupID: 1, StartUnix: 10, EndUnix: 100, Weekdays: []int{7, 5, 3, 1}}}
	testfixture.CallContentAdmin(t, router, "PUT", "/policy", map[string]any{"mode": "all", "expected_revision": 0, "group_schedules": schedules}, 200)
	testfixture.CallContentAdmin(t, router, "PUT", "/policy", map[string]any{"mode": "all", "expected_revision": 0, "group_schedules": []BattleGroupSchedule{}}, 409)
	testfixture.CallContentAdmin(t, router, "PUT", "/policy?catalog=past", map[string]any{"mode": "all", "expected_revision": 0, "group_schedules": schedules}, 400)
	testfixture.CallContentAdmin(t, router, "PUT", "/policy?catalog=past", map[string]any{"mode": "all", "expected_revision": 0, "group_schedules": []BattleGroupSchedule{{GroupID: 3, Weekdays: []int{7}}}}, 200)
	a.operations, err = NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := a.operations.storage.ReadDocument(teamBattlePublicationKey)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := adminPublicationFromDocument(doc)
	want := []BattleGroupSchedule{{GroupID: 1, StartUnix: 10, EndUnix: 100, Weekdays: []int{1, 3, 5, 7}}, {GroupID: 2, Weekdays: []int{2, 4, 6, 7}}}
	if err != nil || policy.Revision != 1 || !reflect.DeepEqual(policy.GroupSchedules, want) {
		t.Fatalf("reloaded policy=%+v err=%v", policy, err)
	}
	var response struct {
		Publication adminPublicationState `json:"publication"`
	}
	if err := json.Unmarshal(testfixture.CallContentAdmin(t, router, "GET", "/policy", nil, 200), &response); err != nil || !reflect.DeepEqual(response.Publication.GroupSchedules, want) {
		t.Fatalf("API reloaded schedules=%+v err=%v", response.Publication, err)
	}
	// Clearing schedules is an explicit replacement and keeps the original policy.
	testfixture.CallContentAdmin(t, router, "PUT", "/policy", map[string]any{"mode": "all", "expected_revision": 1, "group_schedules": []BattleGroupSchedule{}}, 200)
	allowed, err := a.operations.TeamBattleGroupAllowlist()
	if err != nil || allowed != nil {
		t.Fatalf("clearing schedules: %v %v", allowed, err)
	}
}

func TestBossPublicationCatalogIdentities(t *testing.T) {
	groups, err := battlePublicationGroupIDs(gamestate.State{
		TeamBattleSolo:           json.RawMessage(`{"0":123,"9":[{"0":99},{"0":760000010}],"10":[{"0":1}],"11":[{"0":2}],"12":[{"0":3}]}`),
		TeamBattlePastBossGroups: []json.RawMessage{json.RawMessage(`{"0":4,"13":[]}`)},
	})
	if err != nil || !reflect.DeepEqual(groups[teamBattlePublicationKey], []int{99, 760000010, 1, 2, 3}) || !reflect.DeepEqual(groups[PastBattlePublicationKey], []int{4}) {
		t.Fatalf("publication catalog=%v err=%v", groups, err)
	}
	// Hiding one scheduled group must retain generated activities which are
	// recategorized from source 9 by the account's presentation projection.
	allowed, err := battlePublicationAllowlist(TeamBattlePublication{Mode: "all", GroupSchedules: []BattleGroupSchedule{{GroupID: 1, StartUnix: 100}}}, groups[teamBattlePublicationKey], time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := allowed[760000010]; !exists {
		t.Fatal("unrelated generated activity disappeared")
	}
}
