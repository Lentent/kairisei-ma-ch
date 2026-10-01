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
)

func TestCDKRecorderRedactsCredentialsAndVerifier(t *testing.T) {
	for _, path := range []string{"/api/cdk/redeem", "///api/cdk/redeem", "/GiftCodeAd", "///GiftCodeAd"} {
		marker := "<login-redacted>"
		if strings.HasSuffix(path, "/GiftCodeAd") {
			marker = "<cdk-redacted>"
		}
		body := `{"username":"player","password":"private-password","code":"PRIVATE-CODE"}`
		if got := redactCNRequestBody(path, []byte(body)); got != marker {
			t.Fatal(path, got)
		}
		logPath := filepath.Join(t.TempDir(), "requests.jsonl")
		if err := os.WriteFile(logPath, nil, 0600); err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest("POST", path+"?password=private-password", strings.NewReader(body))
		request.Header.Set("X-Probe", "private-header")
		response := httptest.NewRecorder()
		(&recorder{path: logPath}).middleware(slog.New(slog.NewTextHandler(io.Discard, nil)))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			forwarded, _ := io.ReadAll(r.Body)
			if string(forwarded) != body {
				t.Fatal("recorder changed credentials")
			}
			w.WriteHeader(204)
		})).ServeHTTP(response, request)
		content, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatal(err)
		}
		var entry capture
		if err := json.Unmarshal(content, &entry); err != nil {
			t.Fatal(err)
		}
		if response.Code != 204 || entry.BodySHA != "" || entry.BodyBytes != 0 || entry.Query != "" || len(entry.Headers) != 0 || entry.Body != marker {
			t.Fatalf("credential capture leaked: %+v", entry)
		}
	}
	if !isUnauthenticatedCNPost("/api/cdk/redeem") {
		t.Fatal("web JSON passed to game session authentication")
	}
}
