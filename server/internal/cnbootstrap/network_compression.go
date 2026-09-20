package cnbootstrap

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"kairisei.local/server/internal/wirecompression"
)

// Native HttpMgr business routes are single path components. Downloads,
// resources, SDK callbacks, admin and account/password endpoints are excluded.
func businessCompressionRoute(r *http.Request) bool {
	path := strings.TrimLeft(r.URL.Path, "/")
	return r.Method == http.MethodPost && path != "" && !strings.ContainsAny(path, "/.")
}

func acceptsGZIP(value string) bool {
	wildcard := false
	explicit, enabled := false, false
	for _, item := range strings.Split(value, ",") {
		parts := strings.Split(item, ";")
		name, quality := strings.ToLower(strings.TrimSpace(parts[0])), 1.0
		for _, parameter := range parts[1:] {
			key, val, ok := strings.Cut(strings.TrimSpace(parameter), "=")
			if ok && strings.EqualFold(strings.TrimSpace(key), "q") {
				q, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
				if err != nil || q < 0 || q > 1 {
					quality = 0
				} else {
					quality = q
				}
			}
		}
		if name == "gzip" {
			explicit, enabled = true, quality > 0
		}
		if name == "*" {
			wildcard = quality > 0
		}
	}
	if explicit {
		return enabled
	}
	return wildcard
}

func cnNetworkCompression(logger *slog.Logger) func(http.Handler) http.Handler {
	var upload, download wirecompression.Metrics
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !businessCompressionRoute(r) {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("X-Kairi-Request-Encoding", "gzip")
			w.Header().Add("Vary", "Accept-Encoding")
			encoding := strings.TrimSpace(r.Header.Get("Content-Encoding"))
			if encoding != "" && !strings.EqualFold(encoding, "identity") {
				if !strings.EqualFold(encoding, "gzip") {
					http.Error(w, "unsupported request encoding", http.StatusUnsupportedMediaType)
					return
				}
				packed, err := io.ReadAll(io.LimitReader(r.Body, maxCapturedBody+1))
				_ = r.Body.Close()
				if len(packed) > maxCapturedBody {
					http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
					return
				}
				if err != nil {
					http.Error(w, "request body read failed", http.StatusBadRequest)
					return
				}
				body, err := wirecompression.Decompress(packed, maxCapturedBody)
				if err != nil {
					http.Error(w, "invalid compressed request", http.StatusBadRequest)
					return
				}
				// capture/authentication consume exactly the original bytes.
				r = r.Clone(r.Context())
				r.Body, r.ContentLength = io.NopCloser(bytes.NewReader(body)), int64(len(body))
				r.Header.Del("Content-Encoding")
				r.Header.Del("Content-Length")
				upload.Add(logger, "http_upload", len(body), len(packed), 0)
			} else if r.ContentLength >= 0 {
				upload.Add(logger, "http_upload", int(r.ContentLength), int(r.ContentLength), 0)
			}
			capture := &compressionWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(capture, r)
			start := time.Now()
			wire := capture.finish(acceptsGZIP(r.Header.Get("Accept-Encoding")))
			download.Add(logger, "http_download", capture.rawBytes, wire, time.Since(start))
		})
	}
}

const maxBufferedBusinessResponse = 8 * 1024 * 1024

type compressionWriter struct {
	http.ResponseWriter
	body                bytes.Buffer
	status              int
	wroteHeader, sent   bool
	rawBytes, wireBytes int
}

func (w *compressionWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	if code >= 100 && code < 200 {
		w.ResponseWriter.WriteHeader(code)
		return
	}
	w.status, w.wroteHeader = code, true
}

func (w *compressionWriter) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	w.rawBytes += len(body)
	if !w.sent && w.body.Len()+len(body) <= maxBufferedBusinessResponse {
		return w.body.Write(body)
	}
	w.flushIdentity()
	n, err := w.ResponseWriter.Write(body)
	w.wireBytes += n
	return n, err
}

func (w *compressionWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *compressionWriter) flushIdentity() {
	if w.sent {
		return
	}
	w.sent = true
	w.ResponseWriter.WriteHeader(w.status)
	n, _ := w.ResponseWriter.Write(w.body.Bytes())
	w.wireBytes += n
	w.body.Reset()
}

func (w *compressionWriter) Flush() {
	w.flushIdentity()
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *compressionWriter) finish(accept bool) int {
	if w.sent {
		return w.wireBytes
	}
	raw := w.body.Bytes()
	h := w.Header()
	kind := strings.ToLower(h.Get("Content-Type"))
	eligible := strings.HasPrefix(kind, "application/json") || strings.HasPrefix(kind, "text/")
	if accept && eligible && len(raw) >= wirecompression.Threshold && w.status != 204 && w.status != 304 &&
		h.Get("Content-Encoding") == "" && h.Get("Content-Range") == "" &&
		!strings.Contains(strings.ToLower(h.Get("Cache-Control")), "no-transform") {
		if packed, err := wirecompression.Compress(raw); err == nil && len(packed)+64 < len(raw) {
			h.Set("Content-Encoding", "gzip")
			h.Del("Content-Length")
			w.ResponseWriter.WriteHeader(w.status)
			n, _ := w.ResponseWriter.Write(packed)
			return n
		}
	}
	w.flushIdentity()
	return w.wireBytes
}
