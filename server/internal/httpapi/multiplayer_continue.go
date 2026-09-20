package httpapi

import (
	"errors"

	"kairisei.local/server/internal/multiplayer"
)

func (h *accountBusinessHandler) ChargeMultiplayerContinue(request multiplayer.BattleContinue) (multiplayer.ContinueBalance, error) {
	a := h.api
	if request.UserID != a.initialState.User.UserID || request.RoomID <= 0 || request.BossID <= 0 || request.Sequence <= 0 {
		return multiplayer.ContinueBalance{}, errors.New("multiplayer continue identity is invalid")
	}
	return a.account.ChargeMultiplayerContinue(request, a.initialState, a.persistState)
}
