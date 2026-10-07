package accountstore

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

func TestPartnerRecommendationsBoundReadsAndPreserveFriends(t *testing.T) {
	storage, err := OpenDatabase(filepath.Join(t.TempDir(), "save.json"), "unused", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	if err := storage.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open()
	if err != nil {
		t.Fatal(err)
	}
	accounts := &Accounts{storage: storage}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	insert := func(id int, login, updated time.Time) {
		t.Helper()
		payload, digest, err := EncodeAccountProjection(gamestate.State{User: gamestate.User{UserID: id, Name: "candidate"}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO cn_local_account VALUES (?, ?, ?, ?, ?)`, id,
			fmt.Sprintf("probe-%d", id), fmt.Sprintf("session-%d", id), base.Format(time.RFC3339Nano), login.Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO cn_account_projection VALUES (?, 2, 1, ?, ?, ?)`, id, updated.Format(time.RFC3339Nano), payload, digest); err != nil {
			t.Fatal(err)
		}
	}
	insert(PrimaryUserID, base.Add(48*time.Hour), base.Add(48*time.Hour))
	for i := 0; i < 120; i++ {
		stamp := base.Add(time.Duration(i) * time.Minute)
		insert(2000000+i, stamp, stamp)
	}
	for profession := int8(1); profession <= 4; profession++ {
		insert(SystemPartnerUserID(profession), base, base)
	}
	// Preserve old follows/friends outside the recent window. A recent friend
	// belongs only to the preferred tier and must not reduce the 100 strangers.
	for _, id := range []int{2000000, 2000001, 2000119} {
		if _, err := db.Exec(`INSERT INTO cn_local_account_follow VALUES (?, ?, ?)`, PrimaryUserID, id, base.Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO cn_local_account_follow VALUES (?, ?, ?)`, 2000001, PrimaryUserID, base.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	// Old login with a fresh save and old save with a fresh login both qualify.
	if _, err := db.Exec(`UPDATE cn_account_projection SET updated_utc = ? WHERE user_id = 2000002`, base.Add(24*time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE cn_local_account SET last_login_utc = ? WHERE user_id = 2000003`, base.Add(25*time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	// Corrupt an excluded projection: a full-population decode would fail.
	if _, err := db.Exec(`UPDATE cn_account_projection SET payload_json = 'not-json' WHERE user_id = 2000004`); err != nil {
		t.Fatal(err)
	}
	relations, err := accounts.ListFriendPointPartnerRecommendations(PrimaryUserID)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]int8{}
	recent, preferred, system := 0, 0, 0
	for _, relation := range relations {
		id := relation.State.User.UserID
		if _, duplicate := seen[id]; duplicate || id == PrimaryUserID {
			t.Fatalf("duplicate/self candidate %d", id)
		}
		seen[id] = relation.FriendState
		switch {
		case relation.System:
			system++
		case relation.FriendState == game.FriendStateFollow || relation.FriendState == game.FriendStateFriend:
			preferred++
		default:
			recent++
		}
	}
	if recent != 100 || preferred != 3 || system != 4 || seen[2000001] != game.FriendStateFriend || seen[2000000] != game.FriendStateFollow {
		t.Fatalf("recent=%d preferred=%d system=%d states=%v", recent, preferred, system, seen)
	}
	for _, id := range []int{2000002, 2000003, 2000021, 2000118} {
		if _, found := seen[id]; !found {
			t.Fatalf("recent candidate %d missing", id)
		}
	}
	if _, found := seen[2000020]; found {
		t.Fatal("candidate beyond the recent window was decoded")
	}
	// Details remain available for a valid account that fell out of the window.
	selected, err := accounts.LoadFriendPointAccountRelations(PrimaryUserID, []int{2000020, 2000020, PrimaryUserID, -1})
	if err != nil || len(selected) != 1 || selected[0].State.User.UserID != 2000020 {
		t.Fatalf("targeted detail load: %+v %v", selected, err)
	}
	plan, err := db.Query("EXPLAIN QUERY PLAN "+partnerRecommendationSelection+partnerRelationProjection,
		PrimaryUserID, SystemPartnerUserIDBase, recentPartnerAccountLimit, SystemPartnerUserIDBase+systemPartnerArthurCount)
	if err != nil {
		t.Fatal(err)
	}
	var details []string
	for plan.Next() {
		var id, parent, unused int
		var detail string
		if err := plan.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		details = append(details, detail)
	}
	if err := plan.Close(); err != nil {
		t.Fatal(err)
	}
	for _, index := range []string{"cn_local_account_recent_login", "cn_account_projection_recent_update"} {
		if !strings.Contains(strings.Join(details, "\n"), index) {
			t.Fatalf("recent selection misses %s: %v", index, details)
		}
	}
}
