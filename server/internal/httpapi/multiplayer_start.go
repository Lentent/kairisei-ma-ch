package httpapi

import (
	"errors"
	"net/http"

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

func (h *accountBusinessHandler) ChargeMultiplayerStart(start multiplayer.BattleStart) error {
	a := h.api
	if start.OwnerUserID != a.initialState.User.UserID || start.RoomID <= 0 || start.BossID <= 0 || start.BPUse < 0 {
		return errors.New("multiplayer start identity is invalid")
	}
	return a.account.ChargeMultiplayerStart(start, a.initialState, a.persistState)
}
