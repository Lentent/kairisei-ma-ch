package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

type historyFixture struct {
	saves   int
	records []gamestate.BattleClearRecord
}

func (h *historyFixture) PersistSoloClear(_ gamestate.State, r gamestate.BattleClearRecord, _ string) error {
	h.saves++
	r.Count = 1
	h.records = []gamestate.BattleClearRecord{r}
	return nil
}
func (h *historyFixture) RecentBattleClears(int, time.Time) ([]gamestate.BattleClearRecord, error) {
	return append([]gamestate.BattleClearRecord(nil), h.records...), nil
}
func (h *historyFixture) YesterdayBattleRanks(int, int, time.Time) ([]gamestate.BattleClearRecord, error) {
	return h.records, nil
}

func TestClearHistoryUsesStartedPartyAndNativeRankAlignment(t *testing.T) {
	const boss = 30010102
	var initial gamestate.State
	s := testAccount(t, func(state *gamestate.State) {
		state.User.Name = "开战昵称"
		state.User.BP = 100
		helper := &state.PlayerProgressionPolicy.Friends.HelperReward
		helper.OtherPerPartner, helper.FriendPerPartner, helper.MaximumPartners = 5, 10, 3
		state.TeamBattleSolo = json.RawMessage(`{"9":[],"10":[{"0":300101,"4":"活动","9":0,"10":[{"0":30010102,"1":1,"4":"超级","5":1,"10":0,"16":0}]}],"11":[],"12":[]}`)
		for i := range state.Decks {
			state.Decks[i].Name = fmt.Sprintf("原卡组%d", i)
		}
		initial = *state
	})
	state := s.Snapshot(initial)
	state.TeamBattleReplays = []gamestate.TeamBattleReplay{{BossID: boss, EnemyPartyID: 1}}
	state.TeamBattleRewards = []gamestate.TeamBattleRewardProfile{{BossID: boss}}
	history := &historyFixture{}
	a := &API{initialState: state, account: s, battleHistory: history, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	call := func(fn http.HandlerFunc, payload string) (map[string]json.RawMessage, int) {
		t.Helper()
		w := httptest.NewRecorder()
		fn(w, httptest.NewRequest("POST", "/", strings.NewReader(payload)))
		parts := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
		if w.Code != 200 || len(parts) != 3 {
			t.Fatalf("HTTP %d %s", w.Code, w.Body.String())
		}
		var common struct {
			Code int `json:"res_code"`
		}
		var method map[string]json.RawMessage
		if json.Unmarshal([]byte(parts[0]), &common) != nil || json.Unmarshal([]byte(parts[1]), &method) != nil {
			t.Fatal("invalid response", w.Body.String())
		}
		return method, common.Code
	}
	if _, code := call(a.teamBattleClearDeckShow, `{"bossid":30010102}`); code != -1 {
		t.Fatal("empty history fabricated a cleared deck")
	}
	start := fmt.Sprintf(`{"bossid":30010102,"deck_arthur_type":3,"deck_arthur_type_idx":0,"partner_deck_selects":[{"userid":%d,"arthur_type":1,"deck_idx":0},{"userid":%d,"arthur_type":2,"deck_idx":0},{"userid":%d,"arthur_type":4,"deck_idx":0}],"starttime":1,"flag":0}`, state.User.UserID, state.User.UserID, state.User.UserID)
	method, code := call(a.teamBattleSoloStart, start)
	if code != 0 {
		t.Fatal("start failed", code)
	}
	var partners []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(method["partner_deck"], &partners); err != nil || len(partners) != 3 || partners[0].Name != "开战昵称" {
		t.Fatal("own partner nickname projection", err)
	}
	// An account-handler reload must retain the runtime snapshot, and later
	// nickname changes must not rewrite the identity recorded at battle start.
	s.SetUserName("后来昵称")
	input := `{"bossid":30010102,"progress":1,"is_clear":1,"input_cmd":["0,22` + strings.Repeat(",0", 19) + `"],"enemy_dead_bit":[0]}`
	if _, code := call(a.teamBattleSoloEnd, input); code != 0 {
		t.Fatal("clear failed", code)
	}
	if _, code := call(a.teamBattleSoloEnd, input); code != 0 || history.saves != 1 {
		t.Fatal("repeated result recorded a second clear")
	}
	if len(history.records) != 1 || history.records[0].Decks[0].ArthurType != 3 || history.records[0].Decks[0].Name != "开战昵称" {
		t.Fatal("controlled profession / frozen nickname lost")
	}
	method, code = call(a.dailyClearRankShow, `{"bossid":30010102,"is_multi":0}`)
	var lists []struct {
		Ranks []struct {
			Rank int `json:"rank"`
			Deck struct {
				Arthur int    `json:"arthur_type"`
				Name   string `json:"name"`
			} `json:"partner_deck"`
		} `json:"ranks"`
	}
	if code != 0 || json.Unmarshal(method["deck"], &lists) != nil || len(lists) != 4 {
		t.Fatal("invalid native rank lists")
	}
	for i, want := range []int{3, 1, 2, 4} {
		if len(lists[i].Ranks) != 1 || lists[i].Ranks[0].Deck.Arthur != want || !strings.HasPrefix(lists[i].Ranks[0].Deck.Name, "原卡组") {
			t.Fatal("solo four-list alignment or original deck name lost", lists)
		}
	}
	method, code = call(a.teamBattleClearDeckShow, `{"bossid":30010102}`)
	var sample []json.RawMessage
	if code != 0 || json.Unmarshal(method["partner_deck"], &sample) != nil || len(sample) != 4 {
		t.Fatal("recent clear sample missing professions")
	}
	if _, code := call(a.teamBattleSoloStart, strings.Replace(start, `"starttime":1`, `"starttime":2`, 1)); code != 0 {
		t.Fatal("next start failed")
	}
	if _, code := call(a.teamBattleSoloEnd, `{"bossid":30010102,"progress":0,"is_clear":0,"input_cmd":[""],"enemy_dead_bit":[0]}`); code != 0 || history.saves != 1 {
		t.Fatal("retreat entered clear statistics")
	}
}
