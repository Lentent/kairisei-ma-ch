package game

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
)

func TestMultiplayerStartDebitIsDurableAndAtomic(t *testing.T) {
	s := &Account{bp: 20, bpMax: 20, bpRecoveryInterval: 3 * time.Minute}
	start := multiplayer.BattleStart{RoomID: 6020000001, BossID: 30010102, OwnerUserID: 1000001, BPUse: 15}
	var saved gamestate.State
	writes := 0
	persist := func(state gamestate.State) error { saved = state; writes++; return nil }
	if err := s.ChargeMultiplayerStart(start, gamestate.State{}, persist); err != nil {
		t.Fatal(err)
	}
	if s.bp != 5 || saved.User.BP != 5 || len(saved.TeamBattleStartReceipts) != 1 {
		t.Fatal("start debit was not persisted with its receipt")
	}
	if err := s.ChargeMultiplayerStart(start, gamestate.State{}, persist); err != nil || writes != 1 || s.bp != 5 {
		t.Fatal("repeated start charged twice")
	}
	// Rehydrated debit state must also acknowledge the same accepted start.
	restarted := &Account{bp: saved.User.BP, bpMax: 20, bpRecoveryInterval: 3 * time.Minute, teamBattleStartReceipts: append([]gamestate.TeamBattleStartReceipt(nil), saved.TeamBattleStartReceipts...)}
	if err := restarted.ChargeMultiplayerStart(start, gamestate.State{}, persist); err != nil || writes != 1 || restarted.bp != 5 {
		t.Fatal("restart replay charged twice")
	}
	start.RoomID++
	if err := s.ChargeMultiplayerStart(start, gamestate.State{}, persist); err == nil || s.bp != 5 || writes != 1 {
		t.Fatal("insufficient balance start was committed")
	}
	start.BPUse = 2
	oldRecovery := s.bpNextRecovery
	if err := s.ChargeMultiplayerStart(start, gamestate.State{}, func(gamestate.State) error { return errors.New("disk unavailable") }); err == nil || s.bp != 5 || len(s.teamBattleStartReceipts) != 1 || !s.bpNextRecovery.Equal(oldRecovery) {
		t.Fatal("failed persistence did not roll back debit")
	}
}

func TestMultiplayerContinueDebitIsDurableAndAtomic(t *testing.T) {
	s := &Account{coin: 102, coinFree: 20}
	request := multiplayer.BattleContinue{RoomID: 6020000001, BossID: 30010102, UserID: 1000001, Sequence: 1}
	var saved gamestate.State
	writes := 0
	persist := func(state gamestate.State) error { saved = state; writes++; return nil }
	if _, err := s.ChargeMultiplayerContinue(request, gamestate.State{}, persist); err != nil {
		t.Fatal(err)
	}
	if s.coin != 72 || s.coinFree != 0 || len(saved.TeamBattleContinueReceipts) != 1 {
		t.Fatal("continue debit and receipt did not persist together")
	}
	restored := &Account{coin: saved.User.Coin, coinFree: saved.User.CoinFree, teamBattleContinueReceipts: saved.TeamBattleContinueReceipts}
	if _, err := restored.ChargeMultiplayerContinue(request, gamestate.State{}, persist); err != nil || writes != 1 {
		t.Fatal("durable retry charged twice")
	}
	request.Sequence++
	if _, err := s.ChargeMultiplayerContinue(request, gamestate.State{}, func(gamestate.State) error { return errors.New("disk unavailable") }); err == nil || s.coin != 72 || s.coinFree != 0 || len(s.teamBattleContinueReceipts) != 1 {
		t.Fatal("failed debit did not roll back")
	}
	if _, err := s.ChargeMultiplayerContinue(request, gamestate.State{}, persist); err != nil || s.coin != 22 || writes != 2 {
		t.Fatal("next continuation did not use paid crystal")
	}
	request.BossID++
	if _, err := s.ChargeMultiplayerContinue(request, gamestate.State{}, persist); err == nil {
		t.Fatal("conflicting receipt accepted")
	}
	request.BossID--
	request.Sequence++
	if _, err := s.ChargeMultiplayerContinue(request, gamestate.State{}, persist); err == nil || s.coin != 22 || writes != 2 {
		t.Fatal("insufficient continuation balance was committed")
	}
}

func TestPrepaidHostAndFreeGuestCanSettleAtZeroBP(t *testing.T) {
	const boss = 30010102
	for _, host := range []bool{true, false} {
		s := &Account{bpMax: 20, bpRecoveryInterval: 3 * time.Minute,
			teamBattleSolo: json.RawMessage(`{"9":[],"10":[{"0":300101,"9":0,"10":[{"0":30010102,"5":15,"10":0}]}],"11":[],"12":[]}`),
		}
		helper := &s.playerProgression.Friends.HelperReward
		helper.OtherPerPartner, helper.FriendPerPartner, helper.MaximumPartners = 5, 10, 3
		roomID := int64(0)
		hostType := 0
		if host {
			roomID = 602000001
			hostType = 1
			s.teamBattleStartReceipts = []gamestate.TeamBattleStartReceipt{{RoomID: roomID, BossID: boss, BPUse: 8}}
		}
		profiles := []gamestate.TeamBattleRewardProfile{{BossID: boss, ResultRewards: []gamestate.Reward{{Type: 4, Num: 10}}}}
		context, _, started, err := s.BeginTeamBattle(boss, []int8{0}, 15, host, profiles, "multi:room=602000001", []TeamBattleFameSource{{ArthurType: 1, LeaderFame: 1}}, hostType, 0, 0, nil, "", nil, roomID)
		if err != nil || !started || s.bp != 0 || context.ConsumesBattlePoints != host || context.HostBonusArthurType != hostType {
			t.Fatalf("host=%v: prepaid/free result context=%+v started=%v err=%v BP=%d", host, context, started, err, s.bp)
		}
		if (host && context.BPUse != 8) || (!host && context.BPUse != 0) {
			t.Fatalf("host=%v settlement did not preserve the paid cost: %d", host, context.BPUse)
		}
		if _, _, ok, err := s.BeginTeamBattle(boss, []int8{0}, 15, host, profiles, "multi:room=602000001", nil, hostType, 0, 0, nil, "", nil, roomID); err != nil || !ok {
			t.Fatalf("same-room result retry cannot resume: %v", err)
		}
		if _, err := s.CompleteTeamBattle(boss, true, profiles, TeamBattleDropReport{EnemyDeadBits: []int{1}}); err != nil {
			t.Fatal(err)
		}
		if s.bp != 0 || s.gold != 10 {
			t.Fatalf("host=%v settlement BP=%d gold=%d", host, s.bp, s.gold)
		}
	}
}
