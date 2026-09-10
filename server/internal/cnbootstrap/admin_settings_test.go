package cnbootstrap

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kairisei.local/server/internal/httpapi"
)

func TestAdminItemShopSettingsPersistOutsidePlayerSave(t *testing.T) {
	accounts := newFriendCapacityTestAccounts(t)
	operations, err := newCNOperationStore(accounts.storage, nil)
	if err != nil {
		t.Fatal(err)
	}
	admin := &cnAdmin{operations: operations}
	call := func(body string, want int) {
		t.Helper()
		r := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:12345"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Kairisei-Admin-Action", "apply")
		w := httptest.NewRecorder()
		admin.setRuntimeSettings(w, r)
		if w.Code != want {
			t.Fatalf("settings HTTP %d: %s", w.Code, w.Body.String())
		}
	}
	call(`{"crystal_purchase_enabled":false,"expected_revision":0,"item_shop":[{"lineup_id":602003,"enabled":true,"price":2000},{"lineup_id":602004,"enabled":false,"price":12000}]}`, 200)
	call(`{"crystal_purchase_enabled":true,"expected_revision":0}`, 409)
	call(`{"crystal_purchase_enabled":false,"expected_revision":1,"item_shop":[{"lineup_id":992001,"enabled":false,"price":2}]}`, 400)
	restarted, err := newCNOperationStore(accounts.storage, nil)
	if err != nil {
		t.Fatal(err)
	}
	admin.operations = restarted
	w := httptest.NewRecorder()
	admin.runtimeSettings(w, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	var response struct {
		Settings httpapi.RuntimeSettings `json:"settings"`
		Revision int                     `json:"revision"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Revision != 1 {
		t.Fatalf("settings reload: %s %v", w.Body.String(), err)
	}
	got := make(map[int]httpapi.ItemShopSetting)
	for _, setting := range response.Settings.ItemShop {
		got[setting.LineupID] = setting
	}
	if !got[602003].Enabled || got[602003].Price != 2000 || got[602004].Enabled || got[602004].Price != 12000 {
		t.Fatalf("operator settings lost after restart: %+v", got)
	}
}
