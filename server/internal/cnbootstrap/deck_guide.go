package cnbootstrap

import (
	_ "embed"
	"net/http"
)

//go:embed deck_guide.html
var cnDeckGuide []byte

func cnBootstrapDeckGuide(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'")
	_, _ = writer.Write(cnDeckGuide)
}
