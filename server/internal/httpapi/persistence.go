package httpapi

import (
	"net/http"
)

func (a *API) persist() error {
	state := a.account.Snapshot(a.initialState)
	if a.persistState == nil {
		return nil
	}
	return a.persistState(state)
}

func (a *API) persistOrError(writer http.ResponseWriter) bool {
	if err := a.persist(); err != nil {
		a.logger.Error("persist local state", "error", err)
		writeError(writer, http.StatusInternalServerError, "persist local state")
		return false
	}
	return true
}
