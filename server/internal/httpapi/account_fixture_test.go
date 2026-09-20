package httpapi

import (
	"testing"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

func testAccount(t *testing.T, configure func(*gamestate.State)) *game.Account {
	t.Helper()
	state := testfixture.RuntimeState(t)
	configure(&state)
	account, err := game.New(state)
	if err != nil {
		t.Fatal(err)
	}
	return account
}
