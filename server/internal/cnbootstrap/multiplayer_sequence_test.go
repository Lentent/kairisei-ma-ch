package cnbootstrap

import (
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/testfixture"
)

func TestRoomSequenceReservesUnfinishedRooms(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	first, err := accounts.NextRoomID(1000000)
	if err != nil {
		t.Fatal(err)
	}
	// Each call opens a new SQLite transaction; no SaveCompleted is needed.
	second, err := accounts.NextRoomID(1000000)
	if err != nil {
		t.Fatal(err)
	}
	if second != first+1 {
		t.Fatalf("unfinished paid room ID reused: %d -> %d", first, second)
	}
}

func TestMultiWaveCompletionPersistsAcrossDatabaseConnections(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	now := time.Now()
	completed := multiplayer.CompletedBattle{RoomID: 123, BossID: 1, OwnerMemberType: 1, Members: make([]multiplayer.Member, 4), OnlineUserIDs: []int{1001}, CompletedAtUnix: now.Unix(), DropLedgerVersion: 1, BattleIndex: 5, Progress: 5, HostCostPaid: true}
	for i := range completed.Members {
		completed.Members[i] = multiplayer.Member{MemberType: i + 1, UserID: 1001 + i}
	}
	for i := 0; i <= completed.BattleIndex; i++ {
		completed.ReleasedDrops = append(completed.ReleasedDrops, gamestate.TeamBattleEnemyDrop{BattleIndex: i, Reward: gamestate.Reward{Type: 4, Num: i + 1, CardSkillLevels: []int16{}}})
	}
	if err := accounts.SaveCompleted(completed, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := accounts.LoadCompleted(123, now)
	if err != nil || loaded.BattleIndex != 5 || loaded.Progress != 5 || len(loaded.ReleasedDrops) != 6 || loaded.ReleasedDrops[5].Reward.Num != 6 {
		t.Fatalf("wave ledger did not survive SQLite reload: %+v %v", loaded, err)
	}
	completed.ReleasedDrops[5].BattleIndex = 6
	if err := accounts.SaveCompleted(completed, now.Add(time.Hour)); err == nil {
		t.Fatal("accepted a reward from a wave not completed")
	}
}
