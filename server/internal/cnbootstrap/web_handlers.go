package cnbootstrap

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

var cn602IntroPlaceholderPNG = func() []byte {
	content, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		panic("decode embedded CN intro placeholder: " + err.Error())
	}
	return content
}()

func cnBootstrapProducts(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"code":         200,
		"product_list": cnLocalShopProducts(),
	})
}

func cnBootstrapIntroPlaceholder(writer http.ResponseWriter, request *http.Request) {
	index, err := strconv.Atoi(chi.URLParam(request, "index"))
	if err != nil || index < 0 || index > 4 {
		http.NotFound(writer, request)
		return
	}
	writer.Header().Set("Content-Type", "image/png")
	writer.Header().Set("Cache-Control", "public, max-age=86400")
	writer.Header().Set("X-Kairisei-Source-State", "PLACEHOLDER")
	_, _ = writer.Write(cn602IntroPlaceholderPNG)
}
