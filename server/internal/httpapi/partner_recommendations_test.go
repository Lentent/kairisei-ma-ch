package httpapi

import (
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

type recommendationTestAccounts struct {
	game.FriendPointAccountRepository
	rows     []game.FriendPointAccountRelation
	selected []int
}

func (r *recommendationTestAccounts) ListFriendPointPartnerRecommendations(int) ([]game.FriendPointAccountRelation, error) {
	return r.rows, nil
}

func (r *recommendationTestAccounts) LoadFriendPointAccountRelations(_ int, ids []int) ([]game.FriendPointAccountRelation, error) {
	r.selected = append([]int(nil), ids...)
	var result []game.FriendPointAccountRelation
	for _, row := range r.rows {
		for _, id := range ids {
			if row.State.User.UserID == id {
				result = append(result, row)
				break
			}
		}
	}
	return result, nil
}

func TestPartnerRecommendationsUseSystemOnlyForEmptyProfessions(t *testing.T) {
	account := testAccount(t, func(*gamestate.State) {})
	repository := &recommendationTestAccounts{}
	add := func(id int, profession int8, system bool, friend int8) {
		state := gamestate.State{User: gamestate.User{UserID: id, Name: "partner", ActiveArthurType: int(profession)},
			Avatars: make([]gamestate.Avatar, 4), SupportDeck: gamestate.SupportDeckState{UnlockSlotNums: make([]int8, 4)}}
		deck := gamestate.Deck{ArthurType: profession, Name: "rental", IsRental: 1}
		for i := 0; i < 10; i++ {
			uid := int64(i + 1)
			state.Cards = append(state.Cards, gamestate.Card{UniqueID: uid, CardID: 100 + i, SkillLevels: []int16{1}})
			deck.CardUniqueIDs = append(deck.CardUniqueIDs, uid)
		}
		state.Decks = []gamestate.Deck{deck}
		repository.rows = append(repository.rows, game.FriendPointAccountRelation{State: state, System: system, FriendState: friend})
	}
	for profession := int8(1); profession <= 4; profession++ {
		add(1900000000+int(profession), profession, true, game.FriendStateOther)
	}
	add(2000001, 1, false, game.FriendStateFollow)
	add(2000002, 1, false, game.FriendStateOther)
	add(2000003, 3, false, game.FriendStateOther)
	api := &API{account: account, initialState: gamestate.State{User: gamestate.User{UserID: 1000001}}, friendPointAccounts: repository}
	assertBuckets := func(want [][]int) {
		t.Helper()
		arthurs, err := api.localTeamBattlePartnerArthurs()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := teamBattlePartnerArthursNumeric(arthurs); err != nil {
			t.Fatal(err)
		}
		for index, value := range arthurs {
			var ids []int
			for _, p := range value.(map[string]any)["partners"].([]any) {
				ids = append(ids, p.(map[string]any)["userid"].(int))
			}
			if !reflect.DeepEqual(ids, want[index]) {
				t.Fatalf("profession %d: %v, want %v", index+1, ids, want[index])
			}
		}
	}
	assertBuckets([][]int{{2000001, 2000002}, {1900000002}, {2000003}, {1900000004}})
	response := httptest.NewRecorder()
	api.teamBattleSoloPartnerRentalDeck(response, httptest.NewRequest("POST", "/TeamBattleSoloPartnerRentalDeck", strings.NewReader(`{"userid":2000001}`)))
	if response.Code != 200 || !reflect.DeepEqual(repository.selected, []int{2000001}) {
		t.Fatalf("targeted rental detail: %d %v %s", response.Code, repository.selected, response.Body.String())
	}
	repository.rows = repository.rows[:4]
	assertBuckets([][]int{{1900000001}, {1900000002}, {1900000003}, {1900000004}})
}
