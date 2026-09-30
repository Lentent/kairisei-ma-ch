package accountstore_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/testfixture"
)

func TestBattleClearStatisticsCommitOnceAndSeparateDaysModes(t *testing.T) {
	a := testfixture.NewFriendCapacityTestAccounts(t)
	_ = testfixture.CreateNamedFriendCapacityAccount(t, a, 0x584)
	other := testfixture.CreateNamedFriendCapacityAccount(t, a, 0x585)
	now := time.Now()
	dayStart := gamestate.BattleClearDay(now.Unix())*86400 - 8*3600
	yesterday := dayStart - 1
	decks := func(user int) []gamestate.BattleClearDeck {
		out := make([]gamestate.BattleClearDeck, 4)
		for i := range out {
			out[i] = gamestate.BattleClearDeck{UserID: user, Name: "通关时昵称", ArthurType: i + 1, HonorIDs: make([]int, 4), Deck: json.RawMessage(fmt.Sprintf(`{"userid":%d,"arthur_type":%d,"name":"通关时卡组"}`, user, i+1))}
		}
		return out
	}
	load := func(id int) gamestate.State {
		t.Helper()
		s, err := a.LoadState(id)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	for _, id := range []int{accountstore.PrimaryUserID, other} {
		state := load(id)
		hash := strings.Repeat("a", 64)
		state.User.Gold += 11
		state.TeamBattleSoloResultReceipts = []gamestate.TeamBattleSoloResultReceipt{{RequestSHA256: hash, BossID: 1, ClaimedAtUnix: yesterday, Response: json.RawMessage(`{"is_clear":1,"user":{},"partners":[]}`)}}
		r := gamestate.BattleClearRecord{BossID: 1, UserID: id, Mode: 0, CompletedAt: yesterday, EventKey: fmt.Sprintf("solo:%d:1", id), Decks: decks(id)}
		// Account save and counter rollback together if the statistics write fails.
		db, _ := a.Database().Open()
		if _, err := db.Exec(`CREATE TRIGGER fail_clear BEFORE INSERT ON cn_battle_clear_daily BEGIN SELECT RAISE(ABORT,'forced'); END`); err != nil {
			t.Fatal(err)
		}
		before := load(id).User.Gold
		if err := a.PersistSoloClear(state, r, hash); err == nil {
			t.Fatal("failed statistics write committed account")
		}
		if got := load(id); got.User.Gold != before || len(got.TeamBattleSoloResultReceipts) != 0 {
			t.Fatal("partial solo settlement")
		}
		if _, err := db.Exec(`DROP TRIGGER fail_clear`); err != nil {
			t.Fatal(err)
		}
		if err := a.PersistSoloClear(state, r, hash); err != nil {
			t.Fatal(err)
		}
		if err := a.PersistSoloClear(state, r, hash); err != nil {
			t.Fatal(err)
		}
		if load(id).User.Gold != before+11 {
			t.Fatal("retry changed reward balance")
		}
		state = load(id)
		state.Decks[0].Name = "后来修改的卡组"
		if err := a.PersistState(id, state); err != nil {
			t.Fatal(err)
		}
	}
	ranks, err := a.YesterdayBattleRanks(1, 0, now)
	if err != nil || len(ranks) != 2 || ranks[0].Count != 1 || ranks[1].Count != 1 || ranks[0].UserID != accountstore.PrimaryUserID || !strings.Contains(string(ranks[0].Decks[0].Deck), "通关时卡组") {
		t.Fatalf("solo ranking / frozen snapshot: %+v %v", ranks, err)
	}
	if rows, err := a.YesterdayBattleRanks(1, 0, time.Unix(dayStart-1, 0)); err != nil || len(rows) != 0 {
		t.Fatal("China midnight selected the wrong day", err)
	}
	// Today must not enter yesterday's ranking; old display statistics expire.
	state := load(other)
	for i, when := range []int64{now.Unix(), now.Add(-31 * 24 * time.Hour).Unix()} {
		hash := fmt.Sprintf("%064x", i+1)
		state.TeamBattleSoloResultReceipts = append(state.TeamBattleSoloResultReceipts, gamestate.TeamBattleSoloResultReceipt{RequestSHA256: hash, BossID: 1, ClaimedAtUnix: when, Response: json.RawMessage(`{"is_clear":1,"user":{},"partners":[]}`)})
		if err := a.PersistSoloClear(state, gamestate.BattleClearRecord{BossID: 1, UserID: other, CompletedAt: when, EventKey: hash, Decks: decks(other)}, hash); err != nil {
			t.Fatal(err)
		}
	}
	if rows, _ := a.YesterdayBattleRanks(1, 0, now); len(rows) != 2 || rows[1].Count != 1 {
		t.Fatal("today contaminated yesterday")
	}
	// A shared room completion counts each eligible human once even when saved
	// concurrently; the other two slots (dead-exit/CPU) never enter a ranking.
	completed := multiplayer.CompletedBattle{RoomID: 585, BossID: 1, OwnerMemberType: 1, CompletedAtUnix: yesterday,
		OnlineUserIDs: []int{accountstore.PrimaryUserID, other}, Members: make([]multiplayer.Member, 4)}
	for i := range completed.Members {
		id := accountstore.PrimaryUserID + i
		if i == 1 {
			id = other
		}
		deck := decks(id)[i]
		completed.Members[i] = multiplayer.Member{UserID: id, MemberType: i + 1, ArthurType: i + 1, Name: deck.Name, DeckHonorIDs: deck.HonorIDs, ClearDeck: deck.Deck}
	}
	results := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() { results <- a.SaveCompleted(completed, now.Add(time.Hour)) }()
	}
	for i := 0; i < 4; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	ranks, err = a.YesterdayBattleRanks(1, 1, now)
	if err != nil || len(ranks) != 2 || ranks[0].ArthurType != 1 || ranks[1].ArthurType != 2 || ranks[0].Count != 1 || ranks[1].Count != 1 {
		t.Fatalf("multi rankings: %+v %v", ranks, err)
	}
	completed.RoomID++
	if err := a.SaveCompleted(completed, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	ranks, err = a.YesterdayBattleRanks(1, 1, now)
	if err != nil || ranks[0].Count != 2 || ranks[1].Count != 2 {
		t.Fatal("second success did not increment independently", err)
	}
	recent, err := a.RecentBattleClears(1, now)
	if err != nil || len(recent) != 5 {
		t.Fatalf("recent samples / retention: %d %v", len(recent), err)
	}
}
