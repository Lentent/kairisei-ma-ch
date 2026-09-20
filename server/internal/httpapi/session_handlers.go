package httpapi

import (
	"net/http"
	"strings"
)

func (a *API) connect(writer http.ResponseWriter, request *http.Request) {
	body, err := readBody(request)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		writeError(writer, http.StatusBadRequest, "session body is empty")
		return
	}
	gameOptionFlag, pushOptionFlag := a.account.OptionState()
	naviUnlockFlag, naviUnlockIDs := a.account.NaviUnlockState()
	isUser := 0
	if a.account.UserCreated() {
		isUser = 1
	}
	a.writeProtocol(writer, map[string]any{
		"is_user":              isUser,
		"push_option":          map[string]int{"enable_flag": pushOptionFlag},
		"game_option":          map[string]int{"enable_flag": gameOptionFlag},
		"revision":             []any{},
		"navi_unlock_flag":     naviUnlockFlag,
		"navi_unlock_ids":      naviUnlockIDs,
		"cl_behavior_flag":     0,
		"tutorial_flag":        a.account.TutorialState(),
		"is_multidevice_share": 0,
		"bonus": map[string]any{
			"bonus_end_time":   0,
			"present_end_time": 0,
			"info_url":         "",
		},
	})
}
