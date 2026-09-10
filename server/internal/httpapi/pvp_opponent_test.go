package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"kairisei.local/server/internal/release"
)

type pvpOpponentStates []release.State

func (states pvpOpponentStates) ListPVPOpponents(int) ([]release.State, error) {
	return append([]release.State(nil), states...), nil
}

func TestPVPDailyChallengesPersistOnceAtCNMidnight(t *testing.T) {
	beforeMidnight := time.Date(2026, 9, 7, 15, 59, 59, 0, time.UTC)
	afterMidnight := beforeMidnight.Add(time.Second)
	config := PVPConfig{ChallengeMax: 5}
	s := &store{pvp: release.PVPPlayerState{Challenge: 0, ChallengeDay: pvpChallengeDay(beforeMidnight)}}
	writes := 0
	var saved []byte
	persist := func(state release.State) error {
		writes++
		var err error
		saved, err = json.Marshal(state.PVP)
		return err
	}
	if err := s.refreshPVPChallenges(config, beforeMidnight, release.State{}, persist); err != nil || writes != 0 || s.pvp.Challenge != 0 {
		t.Fatalf("allowance replenished before midnight: writes=%d state=%+v error=%v", writes, s.pvp, err)
	}
	failure := errors.New("save unavailable")
	if err := s.refreshPVPChallenges(config, afterMidnight, release.State{}, func(release.State) error { return failure }); !errors.Is(err, failure) || s.pvp.Challenge != 0 || s.pvp.ChallengeDay != pvpChallengeDay(beforeMidnight) {
		t.Fatalf("failed refill was not rolled back: state=%+v error=%v", s.pvp, err)
	}
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			if err := s.refreshPVPChallenges(config, afterMidnight, release.State{}, persist); err != nil {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	if writes != 1 || s.pvp.Challenge != 5 {
		t.Fatalf("concurrent midnight refresh: writes=%d state=%+v", writes, s.pvp)
	}
	reopened := &store{}
	if err := json.Unmarshal(saved, &reopened.pvp); err != nil {
		t.Fatal(err)
	}
	reopened.pvp.Challenge = 4 // one challenge consumed after the refill
	for _, now := range []time.Time{afterMidnight.Add(time.Hour), beforeMidnight} {
		if err := reopened.refreshPVPChallenges(config, now, release.State{}, persist); err != nil || writes != 1 || reopened.pvp.Challenge != 4 {
			t.Fatalf("reopen or clock rollback refilled twice: writes=%d state=%+v error=%v", writes, reopened.pvp, err)
		}
	}
	// A retained save without the day field can recover after its last ranked
	// debit, even if later free battles were played on the current day.
	unstamped := &store{pvp: release.PVPPlayerState{Challenge: 0, History: []release.PVPMatch{
		{BattleType: 1, StartedAtUnix: beforeMidnight.Unix()},
		{BattleType: 0, StartedAtUnix: afterMidnight.Unix()},
	}}}
	if err := unstamped.refreshPVPChallenges(config, afterMidnight, release.State{}, nil); err != nil || unstamped.pvp.Challenge != 5 {
		t.Fatalf("prior ranked day did not restore allowance: state=%+v error=%v", unstamped.pvp, err)
	}
}

func TestPVPEnemyDeckIncludesLeaderAndSupportParameters(t *testing.T) {
	state := release.State{Cards: []release.Card{
		{UniqueID: 1, CardID: 11, HP: 100, Attack: 100, Magic: 100, Mind: 100},
		{UniqueID: 2, CardID: 12, HP: 100, Attack: 100, Magic: 100, Mind: 100},
		{UniqueID: 3, CardID: 13, HP: 100, Attack: 100, Magic: 100, Mind: 100, LoveMax: 100},
	}}
	state.User.Jobs = make([]release.JobParameter, 5)
	for arthur := int8(1); arthur <= 4; arthur++ {
		state.User.Jobs[arthur] = release.JobParameter{HP: 10, Attack: 20, Magic: 30, Mind: 40}
		state.PVP.DefenseDecks = append(state.PVP.DefenseDecks, release.PVPDeckSelection{
			ArthurType: arthur, JobType: arthur, LeaderCardIndex: 1,
			CardUniqueIDs: []int64{1, 2}, SupportCardUniqueIDs: []int64{3},
		})
	}
	decks, err := buildPVPEnemyDecks(state)
	if err != nil {
		t.Fatal(err)
	}
	for _, deck := range decks {
		if deck.HP != 266 || deck.Attack != 290 || deck.Magic != 300 || deck.Mind != 300 {
			t.Fatalf("PvP enemy Arthur %d ignores leader/support parameters: %d/%d/%d/%d", deck.ArthurType, deck.HP, deck.Attack, deck.Magic, deck.Mind)
		}
	}
}

func TestPVPStartSkipsUnusableDefenseBeforeCharging(t *testing.T) {
	decks := make([]release.PVPDeckSelection, 4)
	for index := range decks {
		decks[index] = release.PVPDeckSelection{
			ArthurType: int8(index + 1), JobType: int8(index + 1), CardUniqueIDs: []int64{1},
		}
	}
	stale := release.State{PVP: release.PVPPlayerState{DefenseDecks: decks}}
	stale.User.UserID = 2
	valid := stale
	valid.User.UserID = 3
	valid.Cards = []release.Card{{UniqueID: 1, CardID: 9, HP: 100}}
	for _, scenario := range []struct {
		name      string
		states    pvpOpponentStates
		challenge int
		started   bool
	}{
		{"skip stale defense", pvpOpponentStates{stale, valid}, 2, true},
		{"none usable", pvpOpponentStates{stale}, 2, false},
		{"no other accounts", pvpOpponentStates{}, 2, false},
		{"ranked allowance exhausted", pvpOpponentStates{valid}, 0, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			s := &store{cards: []cardInfo{{UniqueID: 1}}, deckSlots: 1, supportUnlockedSlots: []int8{0, 0, 0, 0},
				pvp: release.PVPPlayerState{NextBattleID: 1, Challenge: scenario.challenge,
					DefenseDecks: []release.PVPDeckSelection{}, DefenseUpdatedAtUnix: 1, History: []release.PVPMatch{}},
			}
			before := clonePVPState(s.pvp)
			api := &API{store: s, release: &release.Release{}, pvpAccounts: scenario.states,
				pvpConfig: PVPConfig{EngineMode: pvpEngineClientNativeLocal, Fields: []PVPFieldConfig{{FieldID: 1}}, ReplayResult: 1, RankWinPoint: 10},
			}
			body, err := json.Marshal(map[string]any{"type": 1, "select_arthur_type": 1, "pvp_my_deck": decks})
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			api.pvpStart(response, httptest.NewRequest(http.MethodPost, "/PvpStart2", bytes.NewReader(body)))
			if scenario.started {
				if response.Code != http.StatusOK || s.pvp.Challenge != 1 || len(s.pvp.History) != 1 || s.pvp.History[0].OpponentUserID != 3 {
					t.Fatalf("usable opponent blocked or charged incorrectly: status=%d challenge=%d history=%+v body=%s", response.Code, s.pvp.Challenge, s.pvp.History, response.Body.String())
				}
			} else {
				var common struct {
					Code int    `json:"res_code"`
					Text string `json:"res_str"`
				}
				if err := json.Unmarshal(bytes.Split(response.Body.Bytes(), []byte{'\n'})[0], &common); err != nil {
					t.Fatal(err)
				}
				if response.Code != http.StatusOK || common.Code != -1200 || common.Text == "" {
					t.Fatalf("expected stock arena error callback: status=%d body=%s", response.Code, response.Body.String())
				}
				if !reflect.DeepEqual(s.pvp, before) || s.pvpPoint != 0 {
					t.Fatalf("rejected match changed defense or currency: before=%+v after=%+v points=%d", before, s.pvp, s.pvpPoint)
				}
			}
		})
	}
}
