package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kairisei.local/server/internal/accounthttp"
	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/testfixture"
)

func TestMaintenanceAdmissionPersistenceAndCleanup(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	catalog, err := accounts.Database().CatalogState()
	if err != nil {
		t.Fatal(err)
	}
	o, err := NewOperations(accounts.Database(), catalog.Gachas)
	if err != nil {
		t.Fatal(err)
	}
	o.content = &contentStore{base: catalog, shops: map[int]exchangeShop{}}
	a := &API{accounts: accounts, operations: o, business: accounthttp.New(accounthttp.Config{}), multiplayerHub: multiplayer.NewHub()}
	call := func(fn http.HandlerFunc, body string, want int) {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/api/maintenance", strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Kairisei-Admin-Action", "apply")
		w := httptest.NewRecorder()
		fn(w, r)
		if w.Code != want {
			t.Fatalf("HTTP %d want %d: %s", w.Code, want, w.Body.String())
		}
	}
	call(a.cleanupOperationalState, `{"mode":"preview"}`, 409)
	done, ok := o.AdmitGameRequest()
	if !ok {
		t.Fatal("normal request rejected")
	}
	call(a.setMaintenance, `{"enabled":true,"expected_revision":0}`, 200)
	if _, ok = o.AdmitGameRequest(); ok {
		t.Fatal("new game request admitted")
	}
	call(a.cleanupOperationalState, `{"mode":"preview"}`, 409)
	done()
	reloaded, err := NewOperations(accounts.Database(), catalog.Gachas)
	if err != nil || !reloaded.MaintenanceState().Enabled {
		t.Fatal("maintenance lost on restart", err)
	}
	finish, err := o.beginCleanup()
	if err != nil {
		t.Fatal(err)
	}
	blocked := a.maintenanceWrites(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("write passed cleanup barrier") }))
	w := httptest.NewRecorder()
	blocked.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/settings", nil))
	if w.Code != 409 {
		t.Fatal("admin mutation not blocked")
	}
	call(a.setMaintenance, `{"enabled":false,"expected_revision":1}`, 409)
	finish()
	// Exercise the asynchronous route without touching a running server or player.
	call(a.cleanupOperationalState, `{"mode":"preview"}`, 202)
	o.maintenance.mu.Lock()
	completed := o.maintenance.done
	o.maintenance.mu.Unlock()
	<-completed
	state := o.MaintenanceState()
	if state.Busy || state.Cleanup.State != "complete" || state.Cleanup.Report.Scanned == 0 {
		t.Fatalf("preview job: %+v", state.Cleanup)
	}
	encoded, _ := json.Marshal(map[string]any{"mode": "apply", "digest": state.Cleanup.Report.Digest})
	call(a.cleanupOperationalState, string(encoded), 202)
	o.maintenance.mu.Lock()
	completed = o.maintenance.done
	o.maintenance.mu.Unlock()
	<-completed
	if state = o.MaintenanceState(); state.Cleanup.State != "complete" || !state.Cleanup.Report.Applied {
		t.Fatalf("apply job: %+v", state.Cleanup)
	}
	call(a.setMaintenance, `{"enabled":false,"expected_revision":1}`, 200)
	done, ok = o.AdmitGameRequest()
	if !ok {
		t.Fatal("maintenance did not end")
	}
	done()
	o.CloseMaintenance()
}
