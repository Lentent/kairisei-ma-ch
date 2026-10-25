package admin

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/accounthttp"
	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/testfixture"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAccountBanLifecycle(t *testing.T) {
	a := testfixture.NewFriendCapacityTestAccounts(t)
	uuid := "d4640000-0000-4000-8000-000000000001"
	id, err := a.ResolveLogin(uuid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.BindAccount(uuid, "ban_test", "correct-password"); err != nil {
		t.Fatal(err)
	}
	other, err := a.ResolveLogin("d4640000-0000-4000-8000-000000000002")
	if err != nil {
		t.Fatal(err)
	}
	admin := &API{accounts: a, business: accounthttp.New(accounthttp.Config{})}
	router := chi.NewRouter()
	router.Post("/api/accounts/{userID}/ban", admin.accountBan)
	call := func(body string, guard bool) int {
		req := httptest.NewRequest("POST", fmt.Sprintf("/api/accounts/%d/ban", id.UserID), bytes.NewBufferString(body))
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("Content-Type", "application/json")
		if guard {
			req.Header.Set("X-Kairisei-Admin-Action", "apply")
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w.Code
	}
	if got := call(`{"banned":true,"reason":"违规"}`, false); got != 403 {
		t.Fatalf("guard=%d", got)
	}
	if got := call(`{}`, true); got != 400 {
		t.Fatalf("missing state=%d", got)
	}
	if got := call(`{"banned":true,"reason":"违规"}`, true); got != 200 {
		t.Fatalf("ban=%d", got)
	}
	if _, err = a.ResolveSession(id.SessionKey); !errors.Is(err, accountstore.ErrInvalidSession) {
		t.Fatalf("old session: %v", err)
	}
	if _, err = a.ResolveLogin(uuid); !errors.Is(err, accountstore.ErrAccountBanned) {
		t.Fatalf("UUID bypass: %v", err)
	}
	if _, err = a.LoginAccount("ban_test", "correct-password"); !errors.Is(err, accountstore.ErrAccountBanned) {
		t.Fatalf("password bypass: %v", err)
	}
	if _, err = a.ResolveSession(other.SessionKey); err != nil {
		t.Fatalf("other affected: %v", err)
	}
	reopened, err := accountstore.NewAccounts(a.Database())
	if err != nil {
		t.Fatal(err)
	}
	if err = reopened.CheckAccountAccess(id.UserID); !errors.Is(err, accountstore.ErrAccountBanned) {
		t.Fatalf("restart: %v", err)
	}
	if got := call(`{"banned":false}`, true); got != 200 {
		t.Fatalf("unban=%d", got)
	}
	if _, err = a.ResolveSession(id.SessionKey); !errors.Is(err, accountstore.ErrInvalidSession) {
		t.Fatalf("old session revived: %v", err)
	}
	fresh, err := a.ResolveLogin(uuid)
	if err != nil || fresh.UserID != id.UserID {
		t.Fatalf("unban login: %v", err)
	}
	if _, err = a.LoadState(id.UserID); err != nil {
		t.Fatal(err)
	}
	if err = a.SetAccountBan(id.UserID, accountstore.AccountBan{Banned: true, Reason: "temporary", UntilUnix: time.Now().Unix() + 3600}); err != nil {
		t.Fatal(err)
	}
	db, err := a.Database().Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE cn_account_ban SET until_unix=? WHERE user_id=?`, time.Now().Unix()-1, id.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err = a.ResolveLogin(uuid); err != nil {
		t.Fatalf("expired ban: %v", err)
	}
	if err = a.SetAccountBan(accountstore.SystemPartnerUserIDBase+1, accountstore.AccountBan{Banned: true, Reason: "test"}); err == nil {
		t.Fatal("banned system partner")
	}
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM cn_admin_audit WHERE operation IN ('account-ban','account-unban')`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("audit %d: %v", count, err)
	}
}
