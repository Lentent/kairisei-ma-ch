package admin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image/png"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/accountstore"
)

func (o *Operations) readCustomGachaBanner(key string) ([]byte, error) {
	if !strings.HasPrefix(key, "custom_") || len(key) != 71 {
		return nil, errors.New("无效横幅标识")
	}
	if _, err := hex.DecodeString(key[7:]); err != nil {
		return nil, errors.New("无效横幅标识")
	}
	doc, err := o.storage.ReadDocument("gacha-banner:" + key)
	if err != nil {
		return nil, err
	}
	if doc.Revision == 0 {
		return nil, errors.New("横幅不存在")
	}
	var content []byte
	if err := json.Unmarshal(doc.Payload, &content); err != nil {
		return nil, err
	}
	digest := sha256.Sum256(content)
	if hex.EncodeToString(digest[:]) != key[7:] {
		return nil, errors.New("横幅摘要不匹配")
	}
	return content, nil
}

func (a *API) gachaBannerUpload(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	const limit = 4 << 20
	var body struct {
		Content []byte `json:"content"`
	}
	err := DecodeAdminJSONLimit(r, &body, 6<<20)
	content := body.Content
	if err != nil || len(content) > limit {
		WriteAdminError(w, 400, "横幅须为不超过4 MiB的PNG图片")
		return
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(content))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 || int64(cfg.Width)*int64(cfg.Height) > 8000000 {
		WriteAdminError(w, 400, "请选择有效PNG图片，边长不超过4096，总像素不超过800万")
		return
	}
	if _, err := png.Decode(bytes.NewReader(content)); err != nil {
		WriteAdminError(w, 400, "PNG图片损坏")
		return
	}
	digest := sha256.Sum256(content)
	key := "custom_" + hex.EncodeToString(digest[:])
	_, err = a.operations.storage.WriteDocument("gacha-banner:"+key, 0, content, "gacha-banner")
	if err != nil && !errors.Is(err, accountstore.ErrDocumentConflict) {
		WriteAdminError(w, 500, err.Error())
		return
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "banner_key": key, "image_url": "/gacha-assets/" + key + ".png"})
}

// CustomGachaBanner is public, read-only and content-addressed. Both the game
// listener and the local admin preview read the same persisted image bytes.
func (o *Operations) CustomGachaBanner(w http.ResponseWriter, r *http.Request) {
	file := chi.URLParam(r, "file")
	if !strings.HasSuffix(file, ".png") {
		http.NotFound(w, r)
		return
	}
	content, err := o.readCustomGachaBanner(strings.TrimSuffix(file, ".png"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, file, time.Time{}, bytes.NewReader(content))
}
