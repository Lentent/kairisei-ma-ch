package httpapi

import (
	"errors"
	"net/http"
	"slices"

	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
)

type accountBusinessHandler struct {
	http.Handler
	api             *API
	contentRevision uint64
}

func (h *accountBusinessHandler) HasUnsettledSoloBattle() bool {
	return h.api.account.HasUnsettledSoloBattle()
}

func (h *accountBusinessHandler) ChargeMultiplayerStart(start multiplayer.BattleStart, persist func(gamestate.State) error) error {
	a := h.api
	if start.RoomID <= 0 || start.BossID <= 0 || start.BPUse < 0 || persist == nil {
		return errors.New("multiplayer start identity is invalid")
	}
	if start.OwnerUserID == a.initialState.User.UserID {
		return a.account.ChargeMultiplayerStart(start, a.initialState, persist)
	}
	if !slices.Contains(start.GuestUserIDs, a.initialState.User.UserID) {
		return errors.New("multiplayer start participant is invalid")
	}
	return a.account.ChargeMultiplayerEntry(start, a.initialState, persist)
}
