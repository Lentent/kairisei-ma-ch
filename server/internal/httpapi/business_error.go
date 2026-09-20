package httpapi

import (
	"errors"
	"net/http"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

func (a *API) writeStoreError(writer http.ResponseWriter, err error) {
	if !a.writeBusinessError(writer, err) {
		writeError(writer, http.StatusBadRequest, err.Error())
	}
}

// Returns false for transport, persistence and malformed-input failures. They
// retain their original status at the caller rather than becoming success.
func (a *API) writeBusinessError(writer http.ResponseWriter, err error) bool {
	var expected *game.BusinessError
	if !errors.As(err, &expected) {
		return false
	}
	// Original ProtoMgr.onCommon only displays errors for actions 1 (title)
	// and 2 (home). Action 0 silently invokes the endpoint's default callback;
	// in BuyNavi that callback even treats default res=0 as a navigator ID.
	// Use the supported home return to refresh the page without losing login.
	a.writeProtocolResponse(writer, map[string]any{}, []gamestate.PopupProfile{}, expected.Code, expected.Message, 2)
	return true
}
