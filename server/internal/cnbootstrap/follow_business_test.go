package cnbootstrap

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func auditCompleteFollowBusiness(t *testing.T, handler http.Handler, savePath, seedPath string, cards cnCardRuntimeMaster) {
	t.Helper()
	storage, err := newCNSaveDatabase(savePath, seedPath, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	attachProbeCardCatalog(t, storage, cards)
	accounts := &cnAccountStore{storage: storage}
	// Leave the preconstructed primary handler untouched; use fresh players.
	if _, err := accounts.resolveLogin("00000000-0000-4000-8478-000000000000"); err != nil {
		t.Fatal(err)
	}
	user := createNamedFriendCapacityAccount(t, accounts, 0x4781)
	other := createNamedFriendCapacityAccount(t, accounts, 0x4782)
	identity, err := accounts.resolveLogin(fmt.Sprintf("00000000-0000-4000-8000-%012x", 0x4781))
	if err != nil {
		t.Fatal(err)
	}
	call := func(route, payload string, code int) map[string]json.RawMessage {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, route, strings.NewReader(identity.SessionKey+payload)))
		lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
		var common struct {
			Code int `json:"res_code"`
		}
		var method map[string]json.RawMessage
		if w.Code != 200 || len(lines) != 3 || json.Unmarshal([]byte(lines[0]), &common) != nil || common.Code != code || json.Unmarshal([]byte(lines[1]), &method) != nil {
			t.Fatalf("%s: expected native result %d, got HTTP %d %s", route, code, w.Code, w.Body.String())
		}
		return method
	}
	for _, target := range []int{other, cnSystemPartnerUserID(2)} {
		payload := fmt.Sprintf(`{"userids":[%d,0,0,0,0,0,0,0,0,0]}`, target)
		call("/FollowAdd", payload, 0)
		call("/FollowAdd", payload, -3411)
	}
	call("/FollowAdd", `{"userids":[0,0,0,0,0,0,0,0,0,0]}`, 0)
	call("/FollowAdd", fmt.Sprintf(`{"userids":[%d]}`, user), -600)
	call("/FollowAdd", `{"userids":[123456789]}`, -601)
	call("/FollowAdd", fmt.Sprintf(`{"userids":[%d,%d]}`, cnSystemPartnerUserID(3), cnSystemPartnerUserID(3)), 0)
	for range 2 {
		call("/FollowUnFollow", fmt.Sprintf(`{"userid":%d}`, other), 0)
	}
	call("/FollowoFollowShow", "", 0)
	t.Log("native FollowAdd: real/system accounts, padded/empty/duplicate selections, business errors, repeat unfollow and subsequent list passed")
}
