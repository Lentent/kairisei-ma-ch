package cnbootstrap

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"kairisei.local/server/internal/accounthttp"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/httpapi"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/testfixture"
)

// Exercise device takeover, cache eviction, durable recovery and retries through
// the real CN authentication, routing and exploration handlers with isolated SQLite.
func TestExploreRecoveryOnRelogin(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	accounts.Database().SetCatalog(testfixture.RuntimeState(t))
	exploreMaster, err := masterdata.LoadExploreRuntimeMaster(testfixture.WriteTestExploreMaster(t, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	const uuidA = "00000000-0000-4000-8000-000000009001"
	const uuidB = "00000000-0000-4000-8000-000000009002"
	first, err := accounts.ResolveLogin(uuidA)
	if err != nil {
		t.Fatal(err)
	}
	second, err := accounts.ResolveLogin(uuidB)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{first.UserID, second.UserID} {
		state := testfixture.RuntimeState(t)
		state.User.UserID = id
		state.User.AP = 3
		if err := accounts.PersistState(id, state); err != nil {
			t.Fatal(err)
		}
	}
	failPersist := false
	newHandler := func() (http.Handler, *accounthttp.Router) {
		router := accounthttp.New(accounthttp.Config{IdleLimit: 1, Build: func(id int) (http.Handler, error) {
			state, err := accounts.LoadState(id)
			if err != nil {
				t.Errorf("load account: %v", err)
				return nil, err
			}
			if err := masterdata.ApplyExploreRuntimeMaster(&state, exploreMaster); err != nil {
				return nil, err
			}
			handler, err := httpapi.New(httpapi.Config{InitialState: state, BaseURL: "http://local", PersistState: func(s gamestate.State) error {
				if failPersist {
					return errors.New("injected Explore persistence failure")
				}
				return accounts.PersistState(id, s)
			}})
			if err != nil {
				t.Errorf("build account: %v", err)
			}
			return handler, err
		}})
		mux := http.NewServeMux()
		mux.HandleFunc("/Connect", cnBootstrapConnect(router))
		mux.HandleFunc("/ExploreStart", cnBootstrapExploreStart(router))
		mux.HandleFunc("/ExploreEnd", cnBootstrapExploreEnd(router))
		return authenticateCNSessions(accounts)(mux), router
	}
	handler, router := newHandler()
	defer router.InvalidateAll()
	call := func(session, path string, status int) *httptest.ResponseRecorder {
		t.Helper()
		body := session + `{}`
		if path == "/ExploreStart" {
			body = session + `{"arthur_type":1,"deck_idx":0}`
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(body)))
		if w.Code != status {
			t.Fatalf("%s: status=%d want=%d body=%s", path, w.Code, status, w.Body.String())
		}
		return w
	}
	load := func(id int) gamestate.State {
		t.Helper()
		state, err := accounts.LoadPersistentState(id)
		if err != nil {
			t.Fatal(err)
		}
		return state
	}
	call(first.SessionKey, "/ExploreStart", 200)
	second, err = accounts.ResolveLogin(uuidB)
	if err != nil {
		t.Fatal(err)
	}
	call(second.SessionKey, "/Connect", 200)
	if !load(first.UserID).Explore.Active {
		t.Fatal("another account's login settled the active exploration")
	}
	call(second.SessionKey, "/ExploreStart", 200)
	call(first.SessionKey, "/ExploreEnd", 200)
	call(second.SessionKey, "/ExploreEnd", 200)
	otherBefore := load(second.UserID)
	t.Log("Different UID login during exploration: both accounts start and settle successfully, even with cache limit 1")
	call(first.SessionKey, "/ExploreStart", 200)
	oldSession := first.SessionKey
	first, err = accounts.ResolveLogin(uuidA)
	if err != nil {
		t.Fatal(err)
	}
	w := call(oldSession, "/ExploreEnd", 200)
	if !strings.Contains(w.Body.String(), "-3208") {
		t.Fatalf("old login was not rejected: %s", w.Body.String())
	}
	w = call(first.SessionKey, "/ExploreStart", 400)
	t.Logf("Same UID relogin: previous settlement rejected with -3208; next start returns %s", strings.TrimSpace(w.Body.String()))
	before := load(first.UserID)
	if !before.Explore.Active || before.User.AP <= 0 {
		t.Fatal("diagnosis confounded by empty AP")
	}
	call(oldSession, "/Connect", 200)
	if !reflect.DeepEqual(before, load(first.UserID)) {
		t.Fatal("unauthenticated Connect changed the pending exploration")
	}
	// Operator edits after entry must not change the frozen reward on recovery.
	exploreMaster.Events[0] = bytes.ReplaceAll(exploreMaster.Events[0], []byte(`"num":10`), []byte(`"num":900`))
	router.InvalidateAll()
	failPersist = true
	call(first.SessionKey, "/Connect", 500)
	if !reflect.DeepEqual(before, load(first.UserID)) {
		t.Fatal("failed recovery partially committed rewards or cleared the pending run")
	}
	failPersist = false
	call(first.SessionKey, "/Connect", 200)
	after := load(first.UserID)
	if after.Explore.Active || after.Explore.ActiveRewards != nil || after.Explore.StartedAtUnix != 0 ||
		after.User.AP != before.User.AP || after.User.Gold != before.User.Gold+10 || after.ExploreResultReceipt == nil {
		t.Fatal("recovery did not settle the frozen rewards once without spending AP")
	}
	call(first.SessionKey, "/Connect", 200)
	firstResult := call(first.SessionKey, "/ExploreEnd", 200)
	secondResult := call(first.SessionKey, "/ExploreEnd", 200)
	if !bytes.Equal(firstResult.Body.Bytes(), secondResult.Body.Bytes()) || !reflect.DeepEqual(after, load(first.UserID)) {
		t.Fatal("repeated login/result changed the receipt, rewards or mission progress")
	}
	call(first.SessionKey, "/ExploreStart", 200)
	before = load(first.UserID)
	if before.User.AP != 0 || !before.Explore.Active {
		t.Fatal("expected last AP to be spent on the next exploration")
	}
	router.InvalidateAll()
	accounts = testfixture.TestAccountRepository(t, accounts.Database())
	if err := accounts.InvalidateSessions(); err != nil {
		t.Fatal(err)
	}
	handler, router = newHandler()
	defer router.InvalidateAll()
	first, err = accounts.ResolveLogin(uuidA)
	if err != nil {
		t.Fatal(err)
	}
	call(first.SessionKey, "/Connect", 200)
	after = load(first.UserID)
	if after.Explore.Active || after.User.AP != 0 || after.User.Gold != before.User.Gold+900 || after.ExploreResultReceipt == nil {
		t.Fatal("restart/login failed to recover a persisted exploration with zero AP")
	}
	call(first.SessionKey, "/ExploreEnd", 200)
	if !reflect.DeepEqual(after, load(first.UserID)) || !reflect.DeepEqual(otherBefore, load(second.UserID)) {
		t.Fatal("recovery replay changed settled state or another account")
	}
	t.Log("Login recovery survives eviction/restart and persistence failure; frozen rewards, AP and replay remain correct")
}
