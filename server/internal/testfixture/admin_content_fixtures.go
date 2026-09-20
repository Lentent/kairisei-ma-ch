package testfixture

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func CallContentAdmin(t *testing.T, h http.Handler, method, path string, body any, want int) []byte {
	t.Helper()
	data, _ := json.Marshal(body)
	r := httptest.NewRequest(method, "http://localhost"+path, bytes.NewReader(data))
	r.RemoteAddr = "127.0.0.1:12345"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Kairisei-Admin-Action", "apply")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != want {
		t.Fatalf("%s %s: status %d: %s", method, path, w.Code, w.Body.String())
	}
	return w.Body.Bytes()
}
