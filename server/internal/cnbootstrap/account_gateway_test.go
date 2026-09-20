package cnbootstrap

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kairisei.local/server/internal/testfixture"
)

func TestAccountGatewayRequiresBoundedPrivateBody(t *testing.T) {
	gateway := newCNAccountGateway(testfixture.NewFriendCapacityTestAccounts(t))
	body := `{"uuid":"d4640000-0000-4000-8000-000000000003","username":"new_guest","password":"new-password"}`
	request := httptest.NewRequest("POST", "/local/account/bind", strings.NewReader(body))
	response := httptest.NewRecorder()
	gateway.ServeHTTP(response, request)
	if response.Code != 400 {
		t.Fatal("unguarded browser bind accepted")
	}
	request = httptest.NewRequest("POST", "/local/account/bind", strings.NewReader(body))
	request.Header.Set("X-Kairisei-Account", "1")
	response = httptest.NewRecorder()
	gateway.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	if strings.Contains(redactCNRequestBody(request.URL.Path, []byte(body)), "password") {
		t.Fatal("credential body recorded")
	}
	logPath := filepath.Join(t.TempDir(), "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	record := &recorder{path: logPath}
	request = httptest.NewRequest("POST", "/local/account/login", strings.NewReader(body))
	record.middleware(slog.New(slog.NewTextHandler(io.Discard, nil)))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded, _ := io.ReadAll(r.Body)
		if string(forwarded) != body {
			t.Fatal("recording changed authentication input")
		}
	})).ServeHTTP(httptest.NewRecorder(), request)
	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	var entry capture
	if err := json.Unmarshal(logged, &entry); err != nil {
		t.Fatal(err)
	}
	if entry.BodySHA != "" || entry.BodyBytes != 0 || strings.Contains(string(logged), "new-password") {
		t.Fatal("credential verifier leaked into request capture")
	}
	for i := 0; i < 15; i++ {
		request = httptest.NewRequest("POST", "/local/account/status", strings.NewReader(body))
		response = httptest.NewRecorder()
		gateway.ServeHTTP(response, request)
	}
	if response.Code != 429 {
		t.Fatal("account rate limit missing")
	}
}
