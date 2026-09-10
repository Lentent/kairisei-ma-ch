package httpapi

import (
	"errors"
	"net/http"

	"kairisei.local/server/internal/release"
)

func (s *store) unreadPopupState(profile release.PopupProfile) []release.PopupProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if profile.PopupID <= 0 {
		return []release.PopupProfile{}
	}
	if _, read := s.popupReadIDs[profile.PopupID]; read {
		return []release.PopupProfile{}
	}
	return []release.PopupProfile{profile}
}

func (s *store) acknowledgePopup(profile release.PopupProfile, popupID int, isSystem int8) error {
	if profile.PopupID <= 0 || popupID != profile.PopupID || isSystem != profile.IsSystem {
		return errors.New("popup acknowledgement does not match the published local profile")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.popupReadIDs[popupID] = struct{}{}
	return nil
}

func (a *API) popupExec(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		PopupID  int  `json:"popupid"`
		IsSystem int8 `json:"is_system"`
	}
	if err := decodeExact(request, []string{"popupid", "is_system"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if err := a.store.acknowledgePopup(a.release.State.PopupProfile, payload.PopupID, payload.IsSystem); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, struct{}{})
}
