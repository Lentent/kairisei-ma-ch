package game

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func TestSoloEntryFreezesContinueRuleWithDebit(t *testing.T) {
	const boss = 30010102
	s := &Account{bp: 30, bpMax: 30, bpRecoveryInterval: 3 * time.Minute, coin: 32, coinFree: 20,
		teamBattleSolo: json.RawMessage(`{"9":[],"10":[{"0":300101,"9":0,"10":[{"0":30010102,"5":15,"7":1,"10":0,"24":3}]}],"11":[],"12":[]}`),
	}
	helper := &s.playerProgression.Friends.HelperReward
	helper.OtherPerPartner, helper.FriendPerPartner, helper.MaximumPartners = 5, 10, 3
	profiles := []gamestate.TeamBattleRewardProfile{{BossID: boss, ResultRewards: []gamestate.Reward{{Type: 4, Num: 10}}}}
	startID := "solo:offline"
	start := func() error {
		_, _, _, err := s.BeginTeamBattle(boss, []int8{0}, 15, true, profiles, startID, []TeamBattleFameSource{{ArthurType: 1, LeaderFame: 1}}, 0, 0, 0, nil, "", nil)
		return err
	}
	if err := start(); err == nil || s.bp != 30 || s.activeBattle != nil {
		t.Fatal("multiplayer-only entry started or debited solo")
	}
	s.teamBattleSolo = json.RawMessage(strings.Replace(string(s.teamBattleSolo), `"24":3`, `"24":0`, 1))
	s.disabledTeamBattleBossIDs = map[int]bool{boss: true}
	if err := start(); err == nil || s.bp != 30 || s.activeBattle != nil {
		t.Fatal("closed difficulty accepted a stale start or charged BP")
	}
	s.disabledTeamBattleBossIDs = nil
	if err := start(); err != nil || s.bp != 15 || !s.activeBattle.ContinueAllowed {
		t.Fatalf("solo entry failed to freeze the accepted rule: %v", err)
	}
	firstSeed := s.activeBattle.Seed
	if firstSeed <= 0 || firstSeed > 2147483647 {
		t.Fatal("solo start did not allocate a native Int32 seed")
	}
	if err := start(); err != nil || s.activeBattle.Seed != firstSeed || s.bp != 15 {
		t.Fatal("start retry changed its shuffle seed or debited twice", err)
	}
	// Simulate a rule change and a durable-state reload while the client is
	// still in the accepted battle. The new rule applies to the next entry.
	s.teamBattleSolo = json.RawMessage(strings.Replace(string(s.teamBattleSolo), `"7":1`, `"7":0`, 1))
	encoded, err := json.Marshal(snapshotTeamBattleContext(s.activeBattle))
	if err != nil {
		t.Fatal(err)
	}
	var active gamestate.TeamBattleActiveState
	if err := json.Unmarshal(encoded, &active); err != nil {
		t.Fatal(err)
	}
	s.activeBattle = teamBattleContextFromRelease(&active)
	if s.activeBattle.Seed != firstSeed {
		t.Fatal("account handler reload lost the active battle seed")
	}
	report := TeamBattleContinueReport{PayType: 3, Progress: 1,
		InputCommands: []string{"0,22" + strings.Repeat(",0", 19)}, EnemyDeadBits: []int{0}}
	if _, _, err := s.ContinueTeamBattle(report, gamestate.State{}, func(gamestate.State) error { return errors.New("disk unavailable") }); err == nil || s.coinFree != 20 || s.coin != 32 || len(s.activeBattle.ContinueReceipts) != 0 {
		t.Fatal("failed persistence did not roll back continuation")
	}
	var saved gamestate.State
	writes := 0
	persist := func(state gamestate.State) error { saved = state; writes++; return nil }
	if _, _, err := s.ContinueTeamBattle(report, gamestate.State{}, persist); err != nil || s.coinFree != 0 || s.coin != 2 {
		t.Fatalf("published rule changed active continuation: %v", err)
	}
	s.activeBattle = teamBattleContextFromRelease(saved.ActiveTeamBattle)
	if _, _, err := s.ContinueTeamBattle(report, gamestate.State{}, persist); err != nil || writes != 1 || s.coin != 2 {
		t.Fatalf("durable continuation retry failed or charged again: %v", err)
	}
	// The player killed the client without sending its result. A valid new
	// start replaces the old run; an invalid request must leave it intact.
	startID = "solo:next"
	s.teamBattleSolo = json.RawMessage(strings.Replace(string(s.teamBattleSolo), `"24":0`, `"24":3`, 1))
	if err := start(); err == nil || s.activeBattle.FameSeed != "solo:offline" || s.bp != 15 {
		t.Fatal("invalid new entry discarded the prior battle or charged BP")
	}
	s.teamBattleSolo = json.RawMessage(strings.Replace(string(s.teamBattleSolo), `"24":3`, `"24":0`, 1))
	s.teamBattleSoloReceipts = map[string]gamestate.TeamBattleSoloResultReceipt{"old-retreat": {BossID: boss}}
	if err := start(); err != nil || s.activeBattle.ContinueAllowed {
		t.Fatalf("next entry did not accept the new no-continue rule: %v", err)
	}
	if s.bp != 0 || len(s.teamBattleSoloReceipts) != 0 || s.activeBattle.FameSeed != startID {
		t.Fatal("new run did not replace the old result scope and debit exactly once")
	}
	if err := start(); err != nil || s.bp != 0 {
		t.Fatal("repeated start was rejected or charged twice", err)
	}
	if _, _, err := s.ContinueTeamBattle(report, gamestate.State{}, persist); err == nil || writes != 1 || s.coin != 2 {
		t.Fatal("forbidden continuation was charged")
	}
	s.AbandonSoloBattle()
	if s.activeBattle != nil || s.bp != 0 {
		t.Fatal("returning home retained a battle or refunded its entry")
	}
}
