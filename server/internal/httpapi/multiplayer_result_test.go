package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/release"
)

func TestMultiplayerResultUsesEveryWaveAndReplaysReceipt(t *testing.T) {
	const boss = 30010102
	completed := multiplayer.CompletedBattle{RoomID: 123, BossID: boss, OwnerMemberType: 1,
		BattleIndex: 5, Progress: 5, DropLedgerVersion: 1, OnlineUserIDs: []int{1001}, CompletedAtUnix: time.Now().Unix(), HostCostPaid: true}
	for i := 1; i <= 4; i++ {
		completed.Members = append(completed.Members, multiplayer.Member{MemberType: i, ArthurType: i, UserID: 1000 + i,
			LeaderFame: 1, DeckHonorIDs: make([]int, 4)})
	}
	replay := release.TeamBattleReplay{BossID: boss, EnemyPartyID: 1}
	profile := release.TeamBattleRewardProfile{BossID: boss}
	for i := 0; i < 6; i++ {
		replay.Battles = append(replay.Battles, release.TeamBattleReplayBattle{EnemyPartyID: i + 1})
		drop := release.TeamBattleEnemyDrop{BattleIndex: i, Reward: release.Reward{Type: 4, Num: i + 1}}
		profile.EnemyDrops = append(profile.EnemyDrops, drop)
		completed.ReleasedDrops = append(completed.ReleasedDrops, drop)
	}
	hub := multiplayer.NewHub()
	if err := hub.AttachCompletionRepository(resultCompletionRepository{completed: completed}); err != nil {
		t.Fatal(err)
	}
	s := &store{bpMax: 20, bpRecoveryInterval: 3 * time.Minute,
		teamBattleSolo:          json.RawMessage(`{"9":[],"10":[{"0":300101,"9":0,"10":[{"0":30010102,"5":15,"10":0}]}],"11":[],"12":[]}`),
		teamBattleStartReceipts: []release.TeamBattleStartReceipt{{RoomID: 123, BossID: boss, BPUse: 15}},
		teamBattleReceipts:      make(map[int64]release.TeamBattleResultReceipt),
	}
	helper := &s.playerProgression.Friends.HelperReward
	helper.SourceState, helper.OtherPerPartner, helper.FriendPerPartner, helper.MaximumPartners = "PLACEHOLDER", 5, 10, 3
	runtime := &release.Release{State: release.State{TeamBattleReplays: []release.TeamBattleReplay{replay}, TeamBattleRewards: []release.TeamBattleRewardProfile{profile}}}
	runtime.State.User.UserID = 1001
	writes := 0
	var saved release.State
	a := &API{release: runtime, store: s, multiplayer: hub, logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		persistState: func(state release.State) error { saved = state; writes++; return nil }}
	request := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		a.teamBattleResult(w, httptest.NewRequest(http.MethodPost, "/result", strings.NewReader(`{"roomid":123}`)))
		return w
	}
	first := request()
	if first.Code != http.StatusOK {
		t.Fatalf("multi-wave result rejected: %d %s", first.Code, first.Body.String())
	}
	if writes != 1 || s.gold != 21 || s.bp != 0 || saved.User.Gold != 21 || len(saved.TeamBattleResultReceipts) != 1 {
		t.Fatalf("incorrect wave rewards or debit: writes=%d gold=%d BP=%d", writes, s.gold, s.bp)
	}
	// Once saved, a retry is independent of the expired room and must return
	// the exact original response without rerolling rewards or charging again.
	a.multiplayer = multiplayer.NewHub()
	second := request()
	if second.Code != http.StatusOK || second.Body.String() != first.Body.String() || writes != 1 || s.gold != 21 {
		t.Fatal("result retry changed the receipt or granted rewards twice")
	}
}

type resultCompletionRepository struct {
	multiplayer.CompletionRepository
	completed multiplayer.CompletedBattle
}

func (resultCompletionRepository) NextRoomID(minimum int64) (int64, error) { return minimum, nil }

func (repository resultCompletionRepository) LoadCompleted(int64, time.Time) (multiplayer.CompletedBattle, time.Time, error) {
	return repository.completed, time.Now().Add(time.Minute), nil
}
