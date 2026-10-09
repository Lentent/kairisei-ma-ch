package cnbootstrap

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutoNoticeNativeHomeURLAndReadReceipt(t *testing.T) {
	root := t.TempDir()
	log := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(log, nil, 0600); err != nil {
		t.Fatal(err)
	}
	h := newTestHandler(t, root, log)
	home := func() string {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/HomeShow", strings.NewReader(cn602LocalSession)))
		parts := bytes.Split(w.Body.Bytes(), []byte{'\n'})
		if w.Code != 200 || len(parts) != 3 {
			t.Fatalf("home: %s", w.Body.String())
		}
		var body struct {
			URL string `json:"update_info_url"`
		}
		if err := json.Unmarshal(parts[1], &body); err != nil {
			t.Fatal(err)
		}
		return body.URL
	}
	page := func(path string) {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/disabled/web/"+path, nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), "游戏公告") || strings.Contains(w.Body.String(), "<nav") {
			t.Fatalf("notice page: %s", w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("notice receipt was cacheable")
		}
	}
	first := home()
	if !strings.HasPrefix(first, "auto?") {
		t.Fatalf("native relative URL missing: %q", first)
	}
	if home() != first {
		t.Fatal("unopened notice consumed")
	}
	u, _ := url.Parse(first)
	q := u.Query()
	q.Set("user", "1000002")
	u.RawQuery = q.Encode()
	page(u.String())
	if home() != first {
		t.Fatal("tampered token marked account read")
	}
	page(first)
	if home() != "" {
		t.Fatal("notice page did not persist read state")
	}
	captured, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(captured, []byte(q.Get("token"))) {
		t.Fatal("notice token leaked into request log")
	}
}
