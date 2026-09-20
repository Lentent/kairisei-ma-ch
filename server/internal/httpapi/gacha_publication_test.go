package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

func TestGachaGuidanceAndSelectionFollowPublication(t *testing.T) {
	a := &API{initialState: gamestate.State{}, account: testAccount(t, func(state *gamestate.State) {
		state.Gachas = []gamestate.GachaProfile{
			{GachaID: 101, GroupID: 1, PayType: 3, Price: 1, CardNum: 1, CardNumMax: 10, CardIDs: []int{11000045, 11000046}, CardWeights: []int{1, 1}, UserSelectMax: 1},
			{GachaID: 202, GroupID: 2, PayType: 3, Price: 1, CardNum: 1, CardNumMax: 10, CardIDs: []int{11000045, 11000047}, CardWeights: []int{1, 1}, UserSelectMax: 1},
		}
		state.GachaSelections = nil
		state.GachaDailyClaims = nil
		state.SupportDeck.CardCollectionIDs = []int{11000047}
		state.SupportDeck.CardCollectionLoveMaxIDs = nil
	})}
	published := func(id int) bool { return id == 202 }
	t.Run("guidance", func(t *testing.T) {
		request := WithGachaPublication(httptest.NewRequest(http.MethodPost, "/", nil), published)
		response := httptest.NewRecorder()
		a.getURCardNoGetFromCurrentGaCha(response, request)
		var method struct {
			Cards []game.UrCardIDFromGacha `json:"URCardList"`
		}
		if err := json.Unmarshal(bytes.Split(response.Body.Bytes(), []byte{'\n'})[1], &method); err != nil {
			t.Fatal(err)
		}
		if len(method.Cards) != 1 || method.Cards[0].CardID != 11000045 || method.Cards[0].GachaID != 202 {
			t.Fatalf("recommendations ignore publication or lose a shared card: %+v", method.Cards)
		}
	})
	for name, handler := range map[string]http.HandlerFunc{"selection": a.gachaSelectLineupShow, "selected": a.gachaSelectedListShow} {
		t.Run(name, func(t *testing.T) {
			for _, input := range []struct {
				body   string
				result int
			}{{`{"gachaid":101}`, -3100}, {`{"gachaid":202}`, 0}} {
				request := WithGachaPublication(httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(input.body)), published)
				response := httptest.NewRecorder()
				handler(response, request)
				lines := bytes.Split(bytes.TrimSpace(response.Body.Bytes()), []byte{'\n'})
				var common commonResponse
				if response.Code != http.StatusOK || len(lines) != 3 || json.Unmarshal(lines[0], &common) != nil || common.ResultCode != input.result || common.ResultDeleteSaveData != 0 {
					t.Fatalf("%s unexpected publication response: %s", input.body, response.Body.String())
				}
			}
		})
	}
}
