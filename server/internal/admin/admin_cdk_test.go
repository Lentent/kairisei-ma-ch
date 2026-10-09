package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/accounthttp"
	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/httpapi"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/testfixture"
)

func cdkTestAdmin(t *testing.T) (*API, []int) {
	t.Helper()
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	ids := []int{accountstore.PrimaryUserID}
	for i := 1; i <= 4; i++ {
		a, err := accounts.ResolveLogin(fmt.Sprintf("00000000-0000-4000-8000-%012d", i))
		if err != nil {
			t.Fatal(err)
		}
		if a.UserID != accountstore.PrimaryUserID {
			ids = append(ids, a.UserID)
		}
	}
	policy, err := masterdata.LoadPlayerProgressionRuntimeMaster(filepath.Join("..", "..", "config", "cn602-player-progression-runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &API{accounts: accounts, progression: policy, business: accounthttp.New(accounthttp.Config{}), catalogByKey: map[string]AdminCatalogEntry{"4:0": {Kind: "currency", RewardType: 4, Name: "金币"}, "10:0": {Kind: "currency", RewardType: 10, Name: "水晶"}}}, ids
}

func createTestCDK(t *testing.T, admin *API, code, mode string, max int) CDK {
	t.Helper()
	cdk := CDK{Code: code, Mode: mode, Title: "测试礼包", Message: "领取奖励", Rewards: []AdminMailRequest{{RewardType: 4, Quantity: 100}, {RewardType: 10, Quantity: 50}}, CDKPolicy: accountstore.CDKPolicy{Enabled: true, MaxUses: max}}
	if err := admin.validateCDK(&cdk); err != nil {
		t.Fatal(err)
	}
	if err := admin.accounts.Database().CreateCDKDocuments(map[string]any{code: cdk}); err != nil {
		t.Fatal(err)
	}
	return cdk
}

func TestCDKAtomicRollbackConcurrentRetryAndRestart(t *testing.T) {
	admin, ids := cdkTestAdmin(t)
	cdk := createTestCDK(t, admin, "WELCOME2026", "shared", 0)
	db, err := admin.accounts.Database().Open()
	if err != nil {
		t.Fatal(err)
	}
	before, err := admin.accounts.LoadState(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER fail_cdk_audit BEFORE INSERT ON cn_admin_audit WHEN NEW.operation='cdk-redeem' BEGIN SELECT RAISE(ABORT,'probe'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.redeemCDK(cdk.Code, ids[0]); err == nil {
		t.Fatal("expected injected failure")
	}
	after, err := admin.accounts.LoadState(ids[0])
	if err != nil || len(after.Engagement.Presents) != len(before.Engagement.Presents) {
		t.Fatal("failed redemption partly delivered", err)
	}
	if used, err := admin.accounts.Database().CDKUsedBy(cdk.Code, ids[0]); err != nil || used {
		t.Fatal("failed redemption consumed code", err)
	}
	if _, err := db.Exec(`DROP TRIGGER fail_cdk_audit`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_, err := admin.redeemCDK(" welcome2026 ", id)
			if err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			} else if !errors.Is(err, accountstore.ErrCDKUsed) {
				t.Error(err)
			}
		}(ids[i%len(ids)])
	}
	wg.Wait()
	if successes != len(ids) {
		t.Fatalf("successful redemptions=%d", successes)
	}
	for _, id := range ids {
		state, err := admin.accounts.LoadState(id)
		if err != nil {
			t.Fatal(err)
		}
		presents := state.Engagement.Presents
		if len(presents) < 2 || presents[len(presents)-2].Reward.Num != 100 || presents[len(presents)-1].Reward.Num != 50 {
			t.Fatal("wrong rewards")
		}
		// Receipts must survive collection and explicit deletion of all mail.
		state.Engagement.Presents = nil
		state.Engagement.Histories = nil
		if err := admin.accounts.PersistState(id, state); err != nil {
			t.Fatal(err)
		}
	}
	admin.accounts = testfixture.TestAccountRepository(t, admin.accounts.Database())
	admin.business = accounthttp.New(accounthttp.Config{})
	for _, id := range ids {
		if _, err := admin.redeemCDK(cdk.Code, id); !errors.Is(err, accountstore.ErrCDKUsed) {
			t.Fatal("restart replay", err)
		}
	}
	records, total, err := admin.accounts.Database().CDKRecords(cdk.Code, 2, 0)
	if err != nil || total != len(ids) || len(records) != 2 {
		t.Fatal("records pagination", err, total, len(records))
	}
}

func TestCDKGlobalLimitAndAvailability(t *testing.T) {
	admin, ids := cdkTestAdmin(t)
	for _, mode := range []string{"single", "shared"} {
		code := "LIMIT-" + mode
		code, _ = normalizeCDK(code)
		max := 2
		if mode == "single" {
			max = 1
		}
		createTestCDK(t, admin, code, mode, max)
		var wg sync.WaitGroup
		var mu sync.Mutex
		successes := 0
		for _, id := range ids {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				_, err := admin.redeemCDK(code, id)
				if err == nil {
					mu.Lock()
					successes++
					mu.Unlock()
				} else if !errors.Is(err, accountstore.ErrCDKExhausted) {
					t.Error(err)
				}
			}(id)
		}
		wg.Wait()
		if successes != max {
			t.Fatalf("%s exceeded global limit: %d", mode, successes)
		}
	}
	for _, condition := range []string{"disabled", "expired", "future"} {
		code, _ := normalizeCDK("STATE-" + condition)
		cdk := createTestCDK(t, admin, code, "shared", 0)
		switch condition {
		case "disabled":
			cdk.Enabled = false
		case "expired":
			cdk.ExpiresUnix = time.Now().Unix() - 1
		case "future":
			cdk.StartUnix = time.Now().Unix() + 3600
		}
		if _, err := admin.accounts.Database().WriteDocument(accountstore.CDKPrefix+code, 1, cdk, "cdk-status"); err != nil {
			t.Fatal(err)
		}
		_, redeemErr := admin.redeemCDK(code, ids[1])
		wantErr := accountstore.ErrCDKUnavailable
		if condition == "expired" {
			wantErr = accountstore.ErrCDKExpired
		}
		if !errors.Is(redeemErr, wantErr) {
			t.Fatalf("%s: %v", condition, redeemErr)
		}
		if used, _ := admin.accounts.Database().CDKUsedBy(code, ids[1]); used {
			t.Fatal("unavailable code consumed")
		}
	}
	if _, err := admin.redeemCDK("UNKNOWN", ids[1]); !errors.Is(err, accountstore.ErrCDKUnavailable) {
		t.Fatal(err)
	}
}

func cdkRequest(handler http.Handler, method, path string, payload any) *httptest.ResponseRecorder {
	body, _ := json.Marshal(payload)
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Kairisei-Admin-Action", "apply")
	request.Header.Set("X-Kairisei-CDK", "1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestCDKAdminAndPublicHTTP(t *testing.T) {
	admin, ids := cdkTestAdmin(t)
	router := chi.NewRouter()
	router.Post("/api/cdk", admin.createCDK)
	router.Get("/api/cdk", admin.listCDK)
	router.Put("/api/cdk/{code}/enabled", admin.setCDKEnabled)
	router.Get("/api/cdk/{code}/records", admin.cdkRecords)
	input := map[string]any{"code": "http-test", "mode": "shared", "title": "礼包", "message": "谢谢", "rewards": []AdminMailRequest{{RewardType: 4, Quantity: 42}}}
	if r := cdkRequest(router, "POST", "/api/cdk", input); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := cdkRequest(router, "POST", "/api/cdk", input); r.Code != 409 {
		t.Fatal("duplicate code accepted", r.Code)
	}
	input["code"] = ""
	input["count"] = 3
	input["mode"] = "single"
	if r := cdkRequest(router, "POST", "/api/cdk", input); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := cdkRequest(router, "GET", "/api/cdk?limit=2&offset=0", nil); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	// Obtain the existing UUID only through the test repository, never a public UID.
	db, err := admin.accounts.Database().Open()
	if err != nil {
		t.Fatal(err)
	}
	var uuid string
	if err := db.QueryRow(`SELECT login_uuid FROM cn_local_account WHERE user_id=?`, ids[1]).Scan(&uuid); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.accounts.BindAccount(uuid, "cdk_player", "password123"); err != nil {
		t.Fatal(err)
	}
	public := admin.cdkPublicRouter()
	for _, path := range []string{"/cdk", "/cdk.js"} {
		if r := cdkRequest(public, "GET", path, nil); r.Code != 200 {
			t.Fatal(r.Code)
		}
	}
	payload := map[string]any{"username": "cdk_player", "password": "password123", "code": "http-test"}
	bad := map[string]any{"username": "cdk_player", "password": "wrong-password", "code": "http-test"}
	if r := cdkRequest(public, "POST", "/api/cdk/redeem", bad); r.Code != 401 {
		t.Fatal("bad auth", r.Code)
	}
	payload["user_id"] = ids[2]
	if r := cdkRequest(public, "POST", "/api/cdk/redeem", payload); r.Code != 400 {
		t.Fatal("client supplied UID accepted")
	}
	delete(payload, "user_id")
	if r := cdkRequest(public, "POST", "/api/cdk/redeem", payload); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	} else if bytes.Contains(r.Body.Bytes(), []byte(uuid)) || bytes.Contains(r.Body.Bytes(), []byte("password123")) {
		t.Fatal("public response exposed credentials")
	}
	if r := cdkRequest(public, "POST", "/api/cdk/redeem", payload); r.Code != 400 {
		t.Fatal("duplicate redeem", r.Code)
	}
	if used, _ := admin.accounts.Database().CDKUsedBy("HTTP-TEST", ids[2]); used {
		t.Fatal("awarded client chosen UID")
	}
	if r := cdkRequest(router, "GET", "/api/cdk/HTTP-TEST/records", nil); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := cdkRequest(router, "PUT", "/api/cdk/HTTP-TEST/enabled", map[string]any{"enabled": false, "revision": 1}); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := cdkRequest(router, "PUT", "/api/cdk/HTTP-TEST/enabled", map[string]any{"enabled": true, "revision": 1}); r.Code != 409 {
		t.Fatal("stale edit accepted")
	}
	if r := cdkRequest(router, "PUT", "/api/cdk/HTTP-TEST/enabled", map[string]any{"enabled": true, "revision": 2}); r.Code != 200 {
		t.Fatal(r.Code)
	}
	if _, err := admin.redeemCDK("HTTP-TEST", ids[1]); !errors.Is(err, accountstore.ErrCDKUsed) {
		t.Fatal("reenabling allowed replay", err)
	}
}

func TestCDKPublicRequestBoundaries(t *testing.T) {
	admin, _ := cdkTestAdmin(t)
	handler := admin.cdkPublicRouter()
	for _, test := range []struct {
		name, header, value, path string
		status                    int
	}{
		{"missing header", "X-Kairisei-CDK", "", "/api/cdk/redeem", 400},
		{"cross site", "Sec-Fetch-Site", "cross-site", "/api/cdk/redeem", 403},
		{"foreign origin", "Origin", "http://other.example", "/api/cdk/redeem", 403},
		{"form body", "Content-Type", "application/x-www-form-urlencoded", "/api/cdk/redeem", 400},
		{"query credentials", "X-Kairisei-CDK", "1", "/api/cdk/redeem?password=secret", 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("POST", test.path, bytes.NewBufferString(`{}`))
			request.RemoteAddr = "127.0.0.1:1234"
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Kairisei-CDK", "1")
			request.Header.Set(test.header, test.value)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatal(response.Code, response.Body.String())
			}
		})
	}
	gateway := &cdkGateway{requests: map[string][]time.Time{}}
	for i := 0; i < 12; i++ {
		if !gateway.allow("127.0.0.1") {
			t.Fatal("early throttle")
		}
	}
	if gateway.allow("127.0.0.1") {
		t.Fatal("unbounded requests")
	}
}

func TestCDKNativeMailClaimAndCacheInvalidation(t *testing.T) {
	admin, ids := cdkTestAdmin(t)
	id := ids[1]
	state := testfixture.RuntimeState(t)
	state.User.UserID = id
	original, err := admin.accounts.LoadState(id)
	if err != nil {
		t.Fatal(err)
	}
	state.Onboarding = original.Onboarding
	admin.accounts.Database().SetCatalog(state)
	state.Onboarding.Step = masterdata.OnboardingStepCount
	state.Engagement.Presents = nil
	state.Engagement.Histories = nil
	if err := admin.accounts.PersistState(id, state); err != nil {
		t.Fatal(err)
	}
	runtime := accounthttp.New(accounthttp.Config{IdleLimit: 4, Build: func(userID int) (http.Handler, error) {
		state, err := admin.accounts.LoadState(userID)
		if err != nil {
			return nil, err
		}
		handler, err := httpapi.New(httpapi.Config{InitialState: state, BaseURL: "http://127.0.0.1:26020", PersistState: func(state gamestate.State) error { return admin.accounts.PersistState(userID, state) }})
		if err != nil {
			t.Logf("build native handler: %v", err)
		}
		return handler, err
	}})
	admin.business = runtime
	call := func(path string, payload any) {
		t.Helper()
		body, _ := json.Marshal(payload)
		request := httptest.NewRequest("POST", path, bytes.NewReader(body))
		request.Header.Set(accounthttp.AccountUserHeader, strconv.Itoa(id))
		response := httptest.NewRecorder()
		runtime.ServeHTTP(response, request)
		lines := strings.Split(strings.TrimSpace(response.Body.String()), "\n")
		var common struct {
			Code int `json:"res_code"`
		}
		if response.Code != 200 || len(lines) != 3 || json.Unmarshal([]byte(lines[0]), &common) != nil || common.Code != 0 {
			t.Fatalf("%s: %d %s", path, response.Code, response.Body.String())
		}
	}
	call("/PresentBoxShow", nil)
	before, err := admin.accounts.LoadState(id)
	if err != nil {
		t.Fatal(err)
	}
	createTestCDK(t, admin, "NATIVE-GIFT", "shared", 0)
	result, err := admin.redeemCDK("NATIVE-GIFT", id)
	if err != nil {
		t.Fatal(err)
	}
	for _, presentID := range result["present_ids"].([]int64) {
		call("/PresentBoxRecv", map[string]int64{"presentid": presentID})
		call("/PresentBoxRecv", map[string]int64{"presentid": presentID})
		call("/PresentBoxDelete", map[string]int64{"presentid": presentID})
	}
	after, err := admin.accounts.LoadState(id)
	if err != nil {
		t.Fatal(err)
	}
	if after.User.Gold != before.User.Gold+100 || after.User.CoinFree != before.User.CoinFree+50 {
		t.Fatalf("native rewards differ: gold %d→%d, crystals %d→%d", before.User.Gold, after.User.Gold, before.User.CoinFree, after.User.CoinFree)
	}
	if _, err := admin.redeemCDK("NATIVE-GIFT", id); !errors.Is(err, accountstore.ErrCDKUsed) {
		t.Fatal("deleted mail allowed CDK replay", err)
	}
}

func TestCDKBatchCreationRollbackAndStaleClaim(t *testing.T) {
	admin, ids := cdkTestAdmin(t)
	cdk := createTestCDK(t, admin, "ATOMIC-GIFT", "shared", 0)
	if err := admin.accounts.Database().CreateCDKDocuments(map[string]any{cdk.Code: cdk, "NEW-GIFT": cdk}); !errors.Is(err, accountstore.ErrDocumentConflict) {
		t.Fatal("expected collision", err)
	}
	if doc, err := admin.accounts.Database().ReadDocument(accountstore.CDKPrefix + "NEW-GIFT"); err != nil || doc.Revision != 0 {
		t.Fatal("batch partly created", err)
	}
	_, oldDoc, err := admin.readCDK(cdk.Code)
	if err != nil {
		t.Fatal(err)
	}
	cdk.Enabled = false
	if _, err := admin.accounts.Database().WriteDocument(accountstore.CDKPrefix+cdk.Code, oldDoc.Revision, cdk, "cdk-status"); err != nil {
		t.Fatal(err)
	}
	state, err := admin.accounts.LoadState(ids[1])
	if err != nil {
		t.Fatal(err)
	}
	key := accountstore.CDKPrefix + cdk.Code
	err = admin.accounts.PersistStateWithAudit(ids[1], state, &accountstore.AdminAudit{Operation: "cdk-redeem", Target: "probe", Payload: map[string]any{}, CDKClaim: &accountstore.CDKClaim{Key: key, SHA256: oldDoc.SHA256}, Receipt: &accountstore.AdminActionReceipt{OperationKey: key, UserID: ids[1], RequestSHA256: oldDoc.SHA256, Result: map[string]any{}}})
	if !errors.Is(err, accountstore.ErrDocumentConflict) {
		t.Fatal("stale claim bypassed disable", err)
	}
	if used, err := admin.accounts.Database().CDKUsedBy(cdk.Code, ids[1]); err != nil || used {
		t.Fatal("stale claim wrote receipt", err)
	}
}

func TestNativeAndWebCDKShareClaims(t *testing.T) {
	admin, ids := cdkTestAdmin(t)
	createTestCDK(t, admin, "SHARED-ENTRY", "shared", 0)
	handler := &cdkAdminHandler{service: admin}
	if err := handler.RedeemCDK("SHARED-ENTRY", ids[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.redeemCDK("SHARED-ENTRY", ids[1]); !errors.Is(err, accountstore.ErrCDKUsed) {
		t.Fatal("native claim allowed web replay", err)
	}
	if _, err := admin.redeemCDK("SHARED-ENTRY", ids[2]); err != nil {
		t.Fatal(err)
	}
	if err := handler.RedeemCDK("SHARED-ENTRY", ids[2]); !errors.Is(err, accountstore.ErrCDKUsed) {
		t.Fatal("web claim allowed native replay", err)
	}
}
