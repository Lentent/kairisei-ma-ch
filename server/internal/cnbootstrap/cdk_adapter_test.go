package cnbootstrap

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/accounthttp"
	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/testfixture"
)

type nativeCDKFunc func(string, int) error

func (f nativeCDKFunc) RedeemCDK(code string, userID int) error { return f(code, userID) }

func nativeCDKResponse(t *testing.T, handler http.Handler, body string) (int, int) {
	t.Helper()
	r := httptest.NewRequest("POST", "/GiftCodeAd", strings.NewReader(body))
	// The account header cannot override the authenticated session identity.
	r.Header.Set(accounthttp.AccountUserHeader, fmt.Sprint(accountstore.PrimaryUserID))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	parts := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
	if w.Code != 200 || len(parts) != 3 {
		t.Fatalf("invalid native response: HTTP %d %s", w.Code, w.Body.String())
	}
	var common struct {
		Code   int `json:"res_code"`
		Delete int `json:"res_is_del_savedata"`
	}
	var result struct {
		Code *int `json:"code"`
	}
	var popup struct {
		Popup []any `json:"popup"`
	}
	if err := json.Unmarshal([]byte(parts[0]), &common); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(parts[1]), &result); err != nil || result.Code == nil {
		t.Fatal("missing native code", err)
	}
	if err := json.Unmarshal([]byte(parts[2]), &popup); err != nil || popup.Popup == nil {
		t.Fatal("missing native popup", err)
	}
	if common.Delete != 0 {
		t.Fatal("redemption tried to delete client save")
	}
	return common.Code, *result.Code
}

func TestNativeCDKProtocolAndSessionIdentity(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	if _, err := accounts.ResolveLogin("00000000-0000-4000-8000-00000000c001"); err != nil {
		t.Fatal(err)
	}
	identity, err := accounts.ResolveLogin("00000000-0000-4000-8000-00000000c002")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	service := nativeCDKFunc(func(code string, userID int) error {
		calls++
		if userID != identity.UserID {
			t.Fatalf("wrong authenticated user %d", userID)
		}
		switch code {
		case "WELCOME":
			return nil
		case "USED":
			return accountstore.ErrCDKUsed
		case "EXPIRED":
			return accountstore.ErrCDKExpired
		case "LIMITED":
			return accountstore.ErrCDKExhausted
		case "DISABLED":
			return accountstore.ErrCDKUnavailable
		default:
			return errors.New("database failure")
		}
	})
	router := chi.NewRouter()
	router.Use(authenticateCNSessions(accounts))
	router.Post("/GiftCodeAd", cnBootstrapGiftCodeAd(service))
	for _, test := range []struct {
		code string
		want int
	}{{"WELCOME", 0}, {"USED", 3}, {"EXPIRED", 2}, {"LIMITED", 3}, {"DISABLED", 1}, {"INTERNAL", 5}} {
		common, code := nativeCDKResponse(t, router, identity.SessionKey+fmt.Sprintf(`{"channel":"netease","codenumber":%q}`, test.code))
		if common != 0 || code != test.want {
			t.Fatalf("%s: common=%d code=%d", test.code, common, code)
		}
	}
	if calls != 6 {
		t.Fatal("native service calls", calls)
	}
	for _, body := range []string{`{"channel":"netease","codenumber":"WELCOME"}`, "local-cn-" + strings.Repeat("a", 64) + `{"channel":"netease","codenumber":"WELCOME"}`} {
		common, code := nativeCDKResponse(t, router, body)
		if common != 0 || code != 8 {
			t.Fatalf("unauthenticated response appears successful: %d %d", common, code)
		}
	}
	for _, body := range []string{`{"channel":"netease","codenumber":"WELCOME","userid":1000001}`, `{"channel":"netease"}`, `{"channel":7,"codenumber":"WELCOME"}`, `{"channel":"netease","codenumber":7}`, `{"channel":"netease","codenumber":"WELCOME"} {}`, `{broken`} {
		common, code := nativeCDKResponse(t, router, identity.SessionKey+body)
		if common != 0 || code != 1 {
			t.Fatalf("invalid request response: %d %d", common, code)
		}
	}
	if calls != 6 {
		t.Fatal("unauthenticated or malformed request reached service")
	}
}

func TestNativeCDKLimitAndBodyBound(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	identity, err := accounts.ResolveLogin("00000000-0000-4000-8000-00000000c101")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	router := chi.NewRouter()
	router.Use(authenticateCNSessions(accounts))
	router.Post("/GiftCodeAd", cnBootstrapGiftCodeAd(nativeCDKFunc(func(string, int) error { calls++; return nil })))
	for i := 0; i < 13; i++ {
		common, code := nativeCDKResponse(t, router, identity.SessionKey+`{"channel":"netease","codenumber":"WELCOME"}`)
		want := 0
		if i == 12 {
			want = 9
		}
		if common != 0 || code != want {
			t.Fatalf("request %d: %d %d", i, common, code)
		}
	}
	if calls != 12 {
		t.Fatal("rate limit did not bound calls")
	}
	other, err := accounts.ResolveLogin("00000000-0000-4000-8000-00000000c102")
	if err != nil {
		t.Fatal(err)
	}
	_, code := nativeCDKResponse(t, router, other.SessionKey+`{"channel":"netease","codenumber":"WELCOME"}`)
	if code != 0 {
		t.Fatal("one player's limit throttled another")
	}
	_, code = nativeCDKResponse(t, router, other.SessionKey+fmt.Sprintf(`{"channel":"netease","codenumber":%q}`, strings.Repeat("A", 2048)))
	if code != 1 || calls != 13 {
		t.Fatal("oversized request reached service")
	}
}
