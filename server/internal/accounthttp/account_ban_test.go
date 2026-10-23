package accounthttp

import (
	"kairisei.local/server/internal/accountstore"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBanBlocksCachedHandler(t *testing.T) {
	banned := false
	calls := 0
	r := New(Config{IdleLimit: 1, Primary: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }), CheckAccess: func(int) error {
		if banned {
			return accountstore.ErrAccountBanned
		}
		return nil
	}})
	request := func() int {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", "/", nil))
		return w.Code
	}
	if got := request(); got != 200 {
		t.Fatal(got)
	}
	banned = true
	if got := request(); got != 403 || calls != 1 {
		t.Fatalf("cached handler bypass: %d, calls=%d", got, calls)
	}
	banned = false
	if got := request(); got != 200 || calls != 2 {
		t.Fatalf("unban: %d, calls=%d", got, calls)
	}
}
