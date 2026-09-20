package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
)

func TestTeamBattleMultiRoomSearchDoesNotSynthesizeRoom(t *testing.T) {
	api := &API{}

	rooms, err := api.teamBattleMultiRoomWires(multiplayer.RoomSearch{}, 0)
	if err != nil {
		t.Fatalf("list real rooms: %v", err)
	}
	if rooms == nil {
		t.Fatal("room list must be an empty array, not null")
	}
	if len(rooms) != 0 {
		t.Fatalf("expected no rooms without a live multiplayer hub, got %d", len(rooms))
	}
}

func TestExpiredMultiplayerRoomReturnsProtocol(t *testing.T) {
	api := &API{multiplayer: multiplayer.NewHub(), account: &game.Account{}, initialState: gamestate.State{},
		battleSV: multiplayer.Endpoint{Host: "127.0.0.1", Port: 26021}}
	api.initialState.User.UserID = 1001
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		body    string
		code    int
	}{
		{"reserve", api.teamBattleMultiRoomReserve, `{"roomid":6020000001,"deck_arthur_type":1}`, -3208},
		{"enter", api.teamBattleMultiRoomEnter, `{"roomid":6020000001,"deck_arthur_type":1,"deck_arthur_type_idx":0,"use_punished_free_piont":0}`, -3208},
		{"cancel", api.teamBattleMultiRoomReserveCancel, `{"roomid":6020000001,"deck_arthur_type":1}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tc.handler(w, httptest.NewRequest(http.MethodPost, "/"+tc.name, strings.NewReader(tc.body)))
			lines := strings.Split(w.Body.String(), "\n")
			if w.Code != http.StatusOK || len(lines) != 3 {
				t.Fatalf("stale room entered HTTP error path: %d %s", w.Code, w.Body.String())
			}
			var common commonResponse
			if err := json.Unmarshal([]byte(lines[0]), &common); err != nil {
				t.Fatal(err)
			}
			if common.ResultCode != tc.code || common.ResultErrorAction != 0 || common.ResultDeleteSaveData != 0 {
				t.Fatalf("unexpected stale room response: %+v", common)
			}
		})
	}
	for _, tc := range []struct {
		name         string
		err          error
		reserving    bool
		status, code int
	}{
		{"reserved by other", fmt.Errorf("reservation: %w", multiplayer.ErrRoomArthurUnavailable), true, 200, -3208},
		{"profession filled", multiplayer.ErrRoomArthurUnavailable, false, 200, -3202},
		{"account read failed", errors.New("account read failed"), false, 403, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			api.writeMultiplayerAvailabilityError(w, tc.err, tc.reserving, http.StatusForbidden)
			var common commonResponse
			if err := json.Unmarshal([]byte(strings.Split(w.Body.String(), "\n")[0]), &common); err != nil {
				t.Fatal(err)
			}
			if w.Code != tc.status || common.ResultCode != tc.code {
				t.Fatalf("wrong availability classification: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestMultiplayerFriendRoomUsesOwnerRelationship(t *testing.T) {
	room := multiplayer.RoomSnapshot{BossID: 42, RoomType: 1, OwnerMemberType: 2,
		Members: []multiplayer.Member{{MemberType: 1, UserID: 303}, {MemberType: 2, UserID: 202}}}
	relations := &RoomFriendAccounts{}
	api := &API{initialState: gamestate.State{}, friendPointAccounts: relations}
	api.account = testAccount(t, func(state *gamestate.State) {
		state.TeamBattleSolo = json.RawMessage(`{"9":[{"0":1,"1":0,"10":[{"0":42,"1":0,"24":0}]}]}`)
	})
	api.initialState.User.UserID = 101
	for _, tc := range []struct {
		name    string
		state   int8
		loadErr error
		want    bool
	}{
		{"stranger", game.FriendStateOther, nil, false},
		{"friend", game.FriendStateFriend, nil, true},
		{"friend removed", game.FriendStateOther, nil, false},
		{"following only", game.FriendStateFollow, nil, false},
		{"relationship unavailable", game.FriendStateFriend, errors.New("read failed"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			relations.state, relations.err = tc.state, tc.loadErr
			err := api.authorizeMultiplayerRoomSnapshot(room)
			if (err == nil) != tc.want {
				t.Fatalf("friend room access: %v, want allowed=%v", err, tc.want)
			}
			if relations.requester != 101 || len(relations.targets) != 1 || relations.targets[0] != 202 {
				t.Fatalf("queried someone other than the room owner: %d %v", relations.requester, relations.targets)
			}
		})
	}
	room.RoomType = 0
	relations.requester = 0
	if err := api.authorizeMultiplayerRoomSnapshot(room); err != nil || relations.requester != 0 {
		t.Fatalf("public room depends on friend repository: %v", err)
	}
}

func TestTeamBattleMultiRoomSearchEmptyLiveHubStaysEmpty(t *testing.T) {
	api := &API{multiplayer: multiplayer.NewHub()}
	for _, bossID := range []int{0, 10000101, 30010102, 0, 10000101} {
		rooms, err := api.teamBattleMultiRoomWires(multiplayer.RoomSearch{BossID: bossID}, 0)
		if err != nil || rooms == nil || len(rooms) != 0 {
			t.Fatalf("boss=%d rooms=%v err=%v; want nonnil empty list", bossID, rooms, err)
		}
	}
	api.account = testAccount(t, func(state *gamestate.State) {
		state.TeamBattleSolo = json.RawMessage(`{"9":[],"10":[],"11":[],"12":[]}`)
	})
	api.initialState = gamestate.State{}
	api.initialState.User.UserID = 1001
	w := httptest.NewRecorder()
	api.teamBattleMultiRoomSearch(w, httptest.NewRequest(http.MethodPost, "/search", strings.NewReader(
		`{"deck_arthur_type":1,"deck_arthur_type_idx":0,"pass":"","is_rookie":0,"bossid":0,"boss_groupid":0,"rookie_type":-1,"searchid":0,"quest_get_time":0,"is_auto":0,"empty_time":0}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("empty lobby query rejected: %s", w.Body.String())
	}
	var data struct {
		Rooms   []any `json:"rooms"`
		Options []struct {
			ID   int    `json:"searchid"`
			Name string `json:"name"`
		} `json:"search_datas"`
	}
	if err := json.Unmarshal([]byte(strings.Split(w.Body.String(), "\n")[1]), &data); err != nil {
		t.Fatal(err)
	}
	if data.Rooms == nil || len(data.Rooms) != 0 || len(data.Options) == 0 || data.Options[0].ID != 0 || data.Options[0].Name == "" {
		t.Fatalf("empty lobby lost its usable All search option: %+v", data)
	}
}

func TestMultiplayerRoomSelectionUsesActualBoss(t *testing.T) {
	group := map[string]any{"0": 300101, "4": "Boss group", "10": []any{
		map[string]any{"0": 30010101, "4": "Normal", "5": 10},
		map[string]any{"0": 30010102, "4": "Hard", "5": 25},
	}}
	room := multiplayer.RoomSnapshot{RoomID: 1, BossID: 30010102, BossGroup: group, OwnerMemberType: 1,
		Members: []multiplayer.Member{{MemberType: 1, ArthurType: 4, UserID: 1001}}}
	wire, err := multiplayerRoomWire(room, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	// TeamLoListItem and TeamBtMgr.SetRoomIdx both consume bosses[0].
	bosses := wire["boss_group"].(map[string]any)["bosses"].([]any)
	first := bosses[0].(map[string]any)
	if first["bossid"] != float64(room.BossID) || first["difficulty"] != "Hard" || first["bp_use"] != float64(25) {
		t.Fatalf("room selection points at another difficulty: %v", first)
	}
	if len(bosses) != 2 || group["10"].([]any)[0].(map[string]any)["0"] != 30010101 {
		t.Fatal("room projection discarded other difficulties or changed the shared group")
	}
}
