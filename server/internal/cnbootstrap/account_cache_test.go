package cnbootstrap

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAccountCacheReloadsDurableStateAfterUnsuccessfulMutation(t *testing.T) {
	for _, failure := range []string{"persist", "validation", "panic"} {
		t.Run(failure, func(t *testing.T) {
			durable, builds := 100, 0
			build := func(int) (http.Handler, error) {
				builds++
				cached := durable
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/fail" {
						cached -= 10
						switch failure {
						case "panic":
							panic("failed mutation")
						case "persist":
							http.Error(w, "database unavailable", http.StatusInternalServerError)
						default:
							http.Error(w, "invalid final item", http.StatusBadRequest)
						}
						return
					}
					if r.URL.Path == "/commit" {
						cached--
						durable = cached
					}
					fmt.Fprint(w, cached)
				}), nil
			}
			router := newCNAccountBusinessRouter(nil, build)
			func() {
				defer func() {
					if caught := recover(); (caught != nil) != (failure == "panic") {
						t.Fatalf("unexpected panic: %v", caught)
					}
				}()
				router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/fail", nil))
			}()
			for _, path := range []string{"/commit", "/read"} {
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
				if response.Code != http.StatusOK || response.Body.String() != "99" || durable != 99 {
					t.Fatalf("%s retained failed mutation: status=%d body=%s durable=%d", path, response.Code, response.Body.String(), durable)
				}
			}
			if builds != 2 {
				t.Fatalf("built %d handlers; want one reload, then successful cache reuse", builds)
			}
		})
	}
}

func TestAccountCacheEvictionPreservesActiveRequests(t *testing.T) {
	router := newCNAccountBusinessRouter(nil, func(int) (http.Handler, error) {
		return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), nil
	})
	t.Cleanup(func() {
		for id := range router.handlers {
			router.invalidate(id)
		}
	})
	firstID := cnPrimaryUserID
	lock := router.accountLock(firstID)
	active, err := router.acquireHandler(firstID)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 20; i++ {
		id := firstID + i
		entry, err := router.acquireHandler(id)
		if err != nil {
			t.Fatal(err)
		}
		router.releaseHandler(id, entry, false)
	}
	if len(router.handlers) != cnIdleAccountCacheLimit+1 || router.handlers[firstID] != active || router.handlers[firstID+1] != nil {
		t.Fatal("cache did not bound idle entries while retaining the active request")
	}
	active.lastUsed = time.Now().Add(-2 * cnIdleAccountCacheTTL)
	router.expireHandler(firstID, active)
	if router.handlers[firstID] != active {
		t.Fatal("expiry evicted active request")
	}
	router.releaseHandler(firstID, active, false)
	router.expireHandler(firstID, active)
	if router.handlers[firstID] != active {
		t.Fatal("old timer evicted refreshed entry")
	}
	active.lastUsed = time.Now().Add(-2 * cnIdleAccountCacheTTL)
	router.expireHandler(firstID, active)
	if router.handlers[firstID] != nil {
		t.Fatal("idle entry did not expire")
	}
	replacement, err := router.acquireHandler(firstID)
	if err != nil {
		t.Fatal(err)
	}
	router.releaseHandler(firstID, replacement, false)
	router.expireHandler(firstID, active)
	if router.handlers[firstID] != replacement || router.accountLock(firstID) != lock {
		t.Fatal("old timer or eviction changed replacement identity/transaction lock")
	}
}
