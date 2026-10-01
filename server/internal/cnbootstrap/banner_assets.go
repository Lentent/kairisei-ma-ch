package cnbootstrap

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

type bannerAsset struct {
	content   []byte
	versioned string
}
type bannerAssets struct {
	paths   map[string]string
	entries map[string]bannerAsset
}

// Keep a byte snapshot: uploading replacement files must never change the
// bytes served under an already published content hash. Restart to publish.
func loadBannerAssets(files map[string]string) (*bannerAssets, error) {
	result := &bannerAssets{paths: map[string]string{}, entries: map[string]bannerAsset{}}
	for route, file := range files {
		content, err := readBannerSnapshot(file)
		if err != nil {
			return nil, fmt.Errorf("load banner %s: %w", route, err)
		}
		digest := sha256.Sum256(content)
		versioned := strings.TrimSuffix(route, ".png") + fmt.Sprintf("_%x.png", digest)
		result.paths[route] = versioned
		result.entries[route] = bannerAsset{content: content, versioned: versioned}
	}
	return result, nil
}

func readBannerSnapshot(file string) ([]byte, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	const limit = 4 << 20
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > limit {
		return nil, fmt.Errorf("banner must be a non-empty regular file within four MiB")
	}
	content, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if len(content) == 0 || len(content) > limit {
		return nil, fmt.Errorf("invalid banner size")
	}
	return content, nil
}

func (b *bannerAssets) register(router chi.Router) {
	for route, asset := range b.entries {
		serve := func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Cache-Control", "no-store")
			http.ServeContent(w, r, "banner.png", time.Time{}, bytes.NewReader(asset.content))
		}
		// Retain old URLs for clients already on a page during rollout.
		router.Get(route, serve)
		router.Get(asset.versioned, serve)
	}
}
