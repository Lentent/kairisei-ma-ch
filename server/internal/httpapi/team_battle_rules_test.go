package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/internal/release"
)

func TestLostSoloBattleReportReturnsClientResult(t *testing.T) {
	for _, cleared := range []int{0, 1} {
		s := &store{coin: 123, teamBattleSolo: json.RawMessage(`{"9":[],"10":[{"0":300101,"10":[{"0":30010102}]}],"11":[],"12":[]}`)}
		a := &API{store: s, release: &release.Release{}}
		response := httptest.NewRecorder()
		a.teamBattleSoloEnd(response, httptest.NewRequest(http.MethodPost, "/TeamBattleSoloEnd", strings.NewReader(
			fmt.Sprintf(`{"progress":0,"is_clear":%d,"input_cmd":[""],"enemy_dead_bit":[0],"bossid":30010102}`, cleared))))
		lines := strings.Split(strings.TrimSpace(response.Body.String()), "\n")
		if response.Code != http.StatusOK || len(lines) != 3 {
			t.Fatal("expired report returned a transport failure", response.Body.String())
		}
		var common commonResponse
		if err := json.Unmarshal([]byte(lines[0]), &common); err != nil {
			t.Fatal(err)
		}
		wantCode := 0
		if cleared == 1 {
			wantCode = -3207 // Original SV_ERR.TEAMBATTLE_REWARD_NOT_FOUND.
		}
		if common.ResultCode != wantCode || s.coin != 123 || s.activeBattle != nil || len(s.teamBattleSoloReceipts) != 0 {
			t.Fatal("expired report changed the account or returned the wrong business result")
		}
	}
}

func TestSoloRetreatBeforeBattleEndAllowsRetainedReport(t *testing.T) {
	command := "0,22," + strings.Repeat("2147483647,", 18) + "2147483647"
	for _, enemies := range [][]int8{{1}, {1, 1, 4}} {
		commands, dead := make([]string, len(enemies)), make([]int, len(enemies))
		if err := validateNativeTeamBattleReport(0, false, commands, dead, enemies); err != nil {
			t.Fatal("original zero-progress retreat cannot finish its login retry", err)
		}
		if err := validateNativeTeamBattleReport(0, true, commands, dead, enemies); err == nil {
			t.Fatal("empty retreat accepted as victory")
		}
		dead[0] = 1
		if err := validateNativeTeamBattleReport(0, false, commands, dead, enemies); err == nil {
			t.Fatal("zero-progress retreat accepted enemy drops")
		}
		dead[0], commands[0] = 0, "unexpected command"
		if err := validateNativeTeamBattleReport(0, false, commands, dead, enemies); err == nil {
			t.Fatal("zero-progress retreat accepted malformed commands")
		}
		commands[0], dead[0] = command, 2
		if err := validateNativeTeamBattleReport(0, false, commands, dead, enemies); err != nil {
			t.Fatal("retreat after Continue rejected the retained native report", err)
		}
		if err := validateNativeTeamBattleReport(1, false, commands, dead, enemies); err != nil {
			t.Fatal("normal defeat no longer accepts the advanced battle index", err)
		}
		if len(enemies) > 1 {
			commands[1] = command
			if err := validateNativeTeamBattleReport(0, false, commands, dead, enemies); err == nil {
				t.Fatal("retreat accepted future wave state")
			}
			commands[0] = ""
			if err := validateNativeTeamBattleReport(1, false, commands, dead, enemies); err == nil {
				t.Fatal("later-wave retreat accepted a prior wave without commands")
			}
			commands[0] = command
			if err := validateNativeTeamBattleReport(1, false, commands, dead, enemies); err != nil {
				t.Fatal("retreat from a later wave rejected its retained report", err)
			}
		}
	}
}

func TestSoloVictoryDoesNotRequireEnemyBodyDeath(t *testing.T) {
	command := "0,22," + strings.Repeat("2147483647,", 18) + "2147483647"
	for _, mask := range []int{0, 6, 7} {
		if err := validateNativeTeamBattleReport(1, true, []string{command}, []int{mask}, []int8{1}); err != nil {
			t.Fatalf("native scripted victory with death mask %d: %v", mask, err)
		}
	}
	// A truly malformed report must also leave the original client's login
	// retry path through its result callback, rather than HTTP 400.
	s := &store{
		teamBattleSolo: json.RawMessage(`{"9":[],"10":[{"0":300101,"10":[{"0":30010102}]}],"11":[],"12":[]}`),
		activeBattle:   &teamBattleContext{BossID: 30010102, BattleEnemyTypes: []int8{1}},
	}
	a := &API{store: s, release: &release.Release{}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	response := httptest.NewRecorder()
	a.teamBattleSoloEnd(response, httptest.NewRequest(http.MethodPost, "/TeamBattleSoloEnd", strings.NewReader(
		`{"progress":1,"is_clear":1,"input_cmd":["invalid"],"enemy_dead_bit":[6],"bossid":30010102}`)))
	var common commonResponse
	if err := json.Unmarshal([]byte(strings.Split(response.Body.String(), "\n")[0]), &common); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || common.ResultCode != -3207 || s.activeBattle == nil || len(s.teamBattleSoloReceipts) != 0 {
		t.Fatal("invalid report did not return the non-settling client business error", response.Body.String())
	}
}

func TestSoloEntryFreezesContinueRuleWithDebit(t *testing.T) {
	const boss = 30010102
	s := &store{bp: 30, bpMax: 30, bpRecoveryInterval: 3 * time.Minute, coin: 32, coinFree: 20,
		teamBattleSolo: json.RawMessage(`{"9":[],"10":[{"0":300101,"9":0,"10":[{"0":30010102,"5":15,"7":1,"10":0,"24":3}]}],"11":[],"12":[]}`),
	}
	helper := &s.playerProgression.Friends.HelperReward
	helper.SourceState, helper.OtherPerPartner, helper.FriendPerPartner, helper.MaximumPartners = "PLACEHOLDER", 5, 10, 3
	profiles := []release.TeamBattleRewardProfile{{BossID: boss, ResultRewards: []release.Reward{{Type: 4, Num: 10}}}}
	startID := "solo:offline"
	start := func() error {
		_, _, _, err := s.beginTeamBattle(boss, []int8{0}, 15, true, profiles, startID, []teamBattleFameSource{{ArthurType: 1, LeaderFame: 1}}, 0, 0, 0, nil, "", nil)
		return err
	}
	if err := start(); err == nil || s.bp != 30 || s.activeBattle != nil {
		t.Fatal("multiplayer-only entry started or debited solo")
	}
	s.teamBattleSolo = json.RawMessage(strings.Replace(string(s.teamBattleSolo), `"24":3`, `"24":0`, 1))
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
	encoded, err := json.Marshal(releaseTeamBattleContext(s.activeBattle))
	if err != nil {
		t.Fatal(err)
	}
	var active release.TeamBattleActiveState
	if err := json.Unmarshal(encoded, &active); err != nil {
		t.Fatal(err)
	}
	s.activeBattle = teamBattleContextFromRelease(&active)
	if s.activeBattle.Seed != firstSeed {
		t.Fatal("account handler reload lost the active battle seed")
	}
	report := teamBattleContinueReport{PayType: 3, Progress: 1,
		InputCommands: []string{"0,22" + strings.Repeat(",0", 19)}, EnemyDeadBits: []int{0}}
	if _, _, err := s.continueTeamBattle(report, release.State{}, func(release.State) error { return errors.New("disk unavailable") }); err == nil || s.coinFree != 20 || s.coin != 32 || len(s.activeBattle.ContinueReceipts) != 0 {
		t.Fatal("failed persistence did not roll back continuation")
	}
	var saved release.State
	writes := 0
	persist := func(state release.State) error { saved = state; writes++; return nil }
	if _, _, err := s.continueTeamBattle(report, release.State{}, persist); err != nil || s.coinFree != 0 || s.coin != 2 {
		t.Fatalf("published rule changed active continuation: %v", err)
	}
	s.activeBattle = teamBattleContextFromRelease(saved.ActiveTeamBattle)
	if _, _, err := s.continueTeamBattle(report, release.State{}, persist); err != nil || writes != 1 || s.coin != 2 {
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
	s.teamBattleSoloReceipts = map[string]release.TeamBattleSoloResultReceipt{"old-retreat": {BossID: boss}}
	if err := start(); err != nil || s.activeBattle.ContinueAllowed {
		t.Fatalf("next entry did not accept the new no-continue rule: %v", err)
	}
	if s.bp != 0 || len(s.teamBattleSoloReceipts) != 0 || s.activeBattle.FameSeed != startID {
		t.Fatal("new run did not replace the old result scope and debit exactly once")
	}
	if err := start(); err != nil || s.bp != 0 {
		t.Fatal("repeated start was rejected or charged twice", err)
	}
	if _, _, err := s.continueTeamBattle(report, release.State{}, persist); err == nil || writes != 1 || s.coin != 2 {
		t.Fatal("forbidden continuation was charged")
	}
	s.abandonSoloBattle()
	if s.activeBattle != nil || s.bp != 0 {
		t.Fatal("returning home retained a battle or refunded its entry")
	}
}
