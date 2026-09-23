package cnbootstrap

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/go-chi/chi/v5"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBannerVersionsAndSnapshot(t *testing.T) {
	dir := t.TempDir()
	home, gacha := filepath.Join(dir, "home.png"), filepath.Join(dir, "gacha.png")
	write := func(p, s string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(s), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(home, "home-v1")
	write(gacha, "gacha-v1")
	files := map[string]string{"/local/home/banner.png": home, "/local/gacha/local_standard.png": gacha}
	load := func() *bannerAssets {
		t.Helper()
		b, e := loadBannerAssets(files)
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	first := load()
	same := load()
	for k, v := range first.paths {
		if same.paths[k] != v {
			t.Fatal("unchanged content changed URL")
		}
	}
	write(home, "home-v2")
	second := load()
	if first.paths["/local/home/banner.png"] == second.paths["/local/home/banner.png"] {
		t.Fatal("changed image reused URL")
	}
	if first.paths["/local/gacha/local_standard.png"] != second.paths["/local/gacha/local_standard.png"] {
		t.Fatal("unrelated image changed URL")
	}
	for _, tc := range []struct {
		b    *bannerAssets
		want string
	}{{first, "home-v1"}, {second, "home-v2"}} {
		r := chi.NewRouter()
		tc.b.register(r)
		for _, path := range []string{"/local/home/banner.png", tc.b.paths["/local/home/banner.png"]} {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			if w.Code != 200 || w.Body.String() != tc.want || w.Header().Get("Content-Type") != "image/png" {
				t.Fatalf("%s: %d %q", path, w.Code, w.Body.String())
			}
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/local/home/banner_unknown.png", nil))
		if w.Code != 404 {
			t.Fatal("unknown hash accepted")
		}
	}
	// Reverting the image restores its original address.
	write(home, "home-v1")
	if load().paths["/local/home/banner.png"] != first.paths["/local/home/banner.png"] {
		t.Fatal("revert changed address")
	}
}
func TestBannerSnapshotRejectsInvalidFiles(t *testing.T) {
	dir := t.TempDir()
	for _, size := range []int{0, (4 << 20) + 1} {
		p := filepath.Join(dir, "bad.png")
		if err := os.WriteFile(p, bytes.Repeat([]byte{1}, size), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadBannerAssets(map[string]string{"/banner.png": p}); err == nil {
			t.Fatalf("accepted %d bytes", size)
		}
	}
	for _, p := range []string{dir, filepath.Join(dir, "missing")} {
		if _, err := loadBannerAssets(map[string]string{"/banner.png": p}); err == nil {
			t.Fatal("accepted invalid file")
		}
	}
}

// Exercise the real account factory, wire payload and public resource router.
func TestBannerWireURLsDownloadMatchingContent(t *testing.T) {
	root := t.TempDir()
	log := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(log, nil, 0600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, root, log)
	for _, tc := range []struct {
		route, body, list string
		fields            []string
	}{
		{"/HomeShow", "", "banners", []string{"image_url"}},
		{"/GachaShow", `{"0":0}`, "1", []string{"22", "23"}},
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("POST", tc.route, strings.NewReader(cn602LocalSession+tc.body)))
		lines := bytes.Split(w.Body.Bytes(), []byte{'\n'})
		if w.Code != 200 || len(lines) < 2 {
			t.Fatalf("%s: %s", tc.route, w.Body.String())
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(lines[1], &payload); err != nil {
			t.Fatal(err)
		}
		var entries []map[string]any
		if err := json.Unmarshal(payload[tc.list], &entries); err != nil {
			t.Fatal(err)
		}
		if len(entries) == 0 {
			t.Fatalf("no %s: %s", tc.list, w.Body.String())
		}
		for _, entry := range entries {
			for _, field := range tc.fields {
				u, err := url.Parse(entry[field].(string))
				if err != nil {
					t.Fatal(err)
				}
				image := httptest.NewRecorder()
				handler.ServeHTTP(image, httptest.NewRequest("GET", u.String(), nil))
				if image.Code != 200 {
					t.Fatalf("%s: %d", u, image.Code)
				}
				digest := sha256.Sum256(image.Body.Bytes())
				if !strings.HasSuffix(u.Path, fmt.Sprintf("_%x.png", digest)) || u.RawQuery != "" {
					t.Fatalf("URL does not describe downloaded bytes: %s", u)
				}
			}
		}
	}
}
