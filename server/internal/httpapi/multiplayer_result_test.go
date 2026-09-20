package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
)

func TestMultiplayerResultUsesEveryWaveAndReplaysReceipt(t *testing.T) {
	const boss = 30010102
	completed := multiplayer.CompletedBattle{RoomID: 123, BossID: boss, OwnerMemberType: 1,
		BattleIndex: 5, Progress: 5, DropLedgerVersion: 1, OnlineUserIDs: []int{1001}, CompletedAtUnix: time.Now().Unix(), HostCostPaid: true}
	for i := 1; i <= 4; i++ {
		completed.Members = append(completed.Members, multiplayer.Member{MemberType: i, ArthurType: i, UserID: 1000 + i,
			LeaderFame: 1, DeckHonorIDs: make([]int, 4)})
	}
	replay := gamestate.TeamBattleReplay{BossID: boss, EnemyPartyID: 1}
	profile := gamestate.TeamBattleRewardProfile{BossID: boss}
	for i := 0; i < 6; i++ {
		replay.Battles = append(replay.Battles, gamestate.TeamBattleReplayBattle{EnemyPartyID: i + 1})
		drop := gamestate.TeamBattleEnemyDrop{BattleIndex: i, Reward: gamestate.Reward{Type: 4, Num: i + 1}}
		profile.EnemyDrops = append(profile.EnemyDrops, drop)
		completed.ReleasedDrops = append(completed.ReleasedDrops, drop)
	}
	hub := multiplayer.NewHub()
	if err := hub.AttachCompletionRepository(resultCompletionRepository{completed: completed}); err != nil {
		t.Fatal(err)
	}
	s := testAccount(t, func(state *gamestate.State) {
		state.User.Gold, state.User.BP, state.User.BPMax = 0, 0, 20
		state.BattlePoint.RecoverySeconds = 180
		state.TeamBattleSolo = json.RawMessage(`{"9":[],"10":[{"0":300101,"9":0,"10":[{"0":30010102,"5":15,"10":0}]}],"11":[],"12":[]}`)
		state.TeamBattleStartReceipts = []gamestate.TeamBattleStartReceipt{{RoomID: 123, BossID: boss, BPUse: 15}}
		helper := &state.PlayerProgressionPolicy.Friends.HelperReward
		helper.OtherPerPartner, helper.FriendPerPartner, helper.MaximumPartners = 5, 10, 3
	})
	runtime := gamestate.State{TeamBattleReplays: []gamestate.TeamBattleReplay{replay}, TeamBattleRewards: []gamestate.TeamBattleRewardProfile{profile}}
	runtime.User.UserID = 1001
	writes := 0
	var saved gamestate.State
	a := &API{initialState: runtime, account: s, multiplayer: hub, logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		persistState: func(state gamestate.State) error { saved = state; writes++; return nil }}
	request := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		a.teamBattleResult(w, httptest.NewRequest(http.MethodPost, "/result", strings.NewReader(`{"roomid":123}`)))
		return w
	}
	first := request()
	if first.Code != http.StatusOK {
		t.Fatalf("multi-wave result rejected: %d %s", first.Code, first.Body.String())
	}
	if writes != 1 || s.Snapshot(gamestate.State{}).User.Gold != 21 || s.Snapshot(gamestate.State{}).User.BP != 0 || saved.User.Gold != 21 || len(saved.TeamBattleResultReceipts) != 1 {
		t.Fatalf("incorrect wave rewards or debit: writes=%d gold=%d BP=%d", writes, s.Snapshot(gamestate.State{}).User.Gold, s.Snapshot(gamestate.State{}).User.BP)
	}
	// Once saved, a retry is independent of the expired room and must return
	// the exact original response without rerolling rewards or charging again.
	a.multiplayer = multiplayer.NewHub()
	second := request()
	if second.Code != http.StatusOK || second.Body.String() != first.Body.String() || writes != 1 || s.Snapshot(gamestate.State{}).User.Gold != 21 {
		t.Fatal("result retry changed the receipt or granted rewards twice")
	}
}

type resultCompletionRepository struct {
	multiplayer.CompletionRepository
	completed multiplayer.CompletedBattle
	err       error
}

func TestLostMultiplayerResultDoesNotBlockLogin(t *testing.T) {
	for _, ineligible := range []bool{false, true} {
		hub := multiplayer.NewHub()
		if ineligible {
			if err := hub.AttachCompletionRepository(resultCompletionRepository{completed: multiplayer.CompletedBattle{RoomID: 123, OnlineUserIDs: []int{1002}}}); err != nil {
				t.Fatal(err)
			}
		}
		s := testAccount(t, func(state *gamestate.State) { state.User.Gold = 123 })
		runtime := gamestate.State{}
		runtime.User.UserID = 1001
		a := &API{initialState: runtime, account: s, multiplayer: hub}
		for i := 0; i < 2; i++ {
			w := httptest.NewRecorder()
			a.teamBattleResult(w, httptest.NewRequest(http.MethodPost, "/TeamBattleResult", strings.NewReader(`{"roomid":123}`)))
			lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
			var common commonResponse
			if w.Code != 200 || len(lines) != 3 || json.Unmarshal([]byte(lines[0]), &common) != nil || common.ResultCode != -3207 || common.ResultDeleteSaveData != 0 {
				t.Fatalf("lost result still blocks native recovery: %d %s", w.Code, w.Body.String())
			}
		}
		if s.Snapshot(gamestate.State{}).User.Gold != 123 || len(s.Snapshot(gamestate.State{}).TeamBattleResultReceipts) != 0 {
			t.Fatal("lost result mutated rewards")
		}
	}
}

func (resultCompletionRepository) NextRoomID(minimum int64) (int64, error) { return minimum, nil }

func (repository resultCompletionRepository) LoadCompleted(int64, time.Time) (multiplayer.CompletedBattle, time.Time, error) {
	return repository.completed, time.Now().Add(time.Minute), repository.err
}

func TestMultiplayerResultStorageFailureRemainsRetryable(t *testing.T) {
	hub := multiplayer.NewHub()
	if err := hub.AttachCompletionRepository(resultCompletionRepository{err: errors.New("temporary storage failure")}); err != nil {
		t.Fatal(err)
	}
	runtime := gamestate.State{}
	runtime.User.UserID = 1001
	a := &API{initialState: runtime, account: &game.Account{}, multiplayer: hub}
	w := httptest.NewRecorder()
	a.teamBattleResult(w, httptest.NewRequest(http.MethodPost, "/TeamBattleResult", strings.NewReader(`{"roomid":123}`)))
	if w.Code != http.StatusConflict {
		t.Fatalf("storage failure discarded a recoverable battle: %d %s", w.Code, w.Body.String())
	}
}
