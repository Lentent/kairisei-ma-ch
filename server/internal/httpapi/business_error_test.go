package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

func TestBusinessErrorsPreserveSessionAndProtocolErrorsRemainFailures(t *testing.T) {
	a := &API{account: &game.Account{}, initialState: gamestate.State{}}
	for _, expected := range []*game.BusinessError{game.ErrInsufficientGold, game.ErrInsufficientCrystals, game.ErrInsufficientMaterials, game.ErrCardCapacity, game.ErrSphereCapacity, game.ErrBuddyCapacity, game.ErrItemExpired, game.ErrGachaUnavailable} {
		response := httptest.NewRecorder()
		a.writeStoreError(response, fmt.Errorf("operation: %w", expected))
		lines := strings.Split(strings.TrimSpace(response.Body.String()), "\n")
		var common commonResponse
		if response.Code != 200 || len(lines) != 3 || json.Unmarshal([]byte(lines[0]), &common) != nil || common.ResultCode != expected.Code || common.ResultString != expected.Message || common.ResultErrorAction != 2 || common.ResultDeleteSaveData != 0 {
			t.Fatalf("business failure lost native envelope: %s", response.Body.String())
		}
	}
	response := httptest.NewRecorder()
	if a.writeBusinessError(response, errors.New("disk failure")) || response.Body.Len() != 0 {
		t.Fatal("storage error was disguised as a business rejection")
	}
	a.writeStoreError(response, errors.New("invalid payload"))
	if response.Code != 400 {
		t.Fatal("malformed input was accepted")
	}
}

func TestClosedGachaRejectsStalePlayWithoutDisconnection(t *testing.T) {
	a := &API{account: &game.Account{}, initialState: gamestate.State{}}
	r := httptest.NewRequest("POST", "/GachaPlay2", strings.NewReader(`{"gachaid":60200011,"pay_type":3,"gacha_hash":"stale","select_lineup_list":[],"popupid":0}`))
	r = WithGachaPublication(r, func(int) bool { return false })
	w := httptest.NewRecorder()
	a.gachaPlay(w, r)
	var common commonResponse
	lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
	if w.Code != 200 || len(lines) != 3 || json.Unmarshal([]byte(lines[0]), &common) != nil || common.ResultCode != -3100 || common.ResultDeleteSaveData != 0 || common.ResultErrorAction != 2 {
		t.Fatalf("stale gacha: %s", w.Body.String())
	}
}
