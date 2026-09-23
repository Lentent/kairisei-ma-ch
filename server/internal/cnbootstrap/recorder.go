package cnbootstrap

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"kairisei.local/server/internal/accounthttp"
)

const maxCapturedBody = 1024 * 1024

type capture struct {
	Timestamp string              `json:"timestamp"`
	Method    string              `json:"method"`
	Path      string              `json:"path"`
	Query     string              `json:"query,omitempty"`
	Remote    string              `json:"remote"`
	Headers   map[string][]string `json:"headers"`
	BodyBytes int                 `json:"body_bytes"`
	BodySHA   string              `json:"body_sha256"`
	Body      string              `json:"body"`
}

type recorder struct {
	path string
	mu   sync.Mutex
}

func (r *recorder) middleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path != "/healthz" && !isBulkResourceRequest(request) {
				body, err := io.ReadAll(io.LimitReader(request.Body, maxCapturedBody+1))
				if err != nil {
					http.Error(writer, "request body read failed", http.StatusBadRequest)
					return
				}
				if len(body) > maxCapturedBody {
					http.Error(writer, "request body too large", http.StatusRequestEntityTooLarge)
					return
				}
				request.Body = io.NopCloser(bytes.NewReader(body))
				digest := sha256.Sum256(body)
				headers := request.Header.Clone()
				for _, name := range []string{"Authorization", "Cookie", "Proxy-Authorization", accounthttp.AccountUserHeader} {
					headers.Del(name)
				}
				entry := capture{
					Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
					Method:    request.Method,
					Path:      request.URL.Path,
					Query:     request.URL.RawQuery,
					Remote:    request.RemoteAddr,
					Headers:   headers,
					BodyBytes: len(body),
					BodySHA:   hex.EncodeToString(digest[:]),
					Body:      redactCNRequestBody(request.URL.Path, body),
				}
				if strings.TrimLeft(request.URL.Path, "/") == "disabled/web/auto" {
					entry.Query = "" // Do not capture notice acknowledgement tokens.
				}
				if strings.HasPrefix(request.URL.Path, "/local/account/") {
					// Even a raw SHA-256/length would expose a fast password
					// guessing oracle. Account captures contain routing only.
					entry.BodySHA, entry.Query, entry.BodyBytes = "", "", 0
					entry.Headers = nil
				}
				if err := r.append(entry); err != nil {
					logger.Warn("skip failed request capture", "error", err)
				} else {
					logger.Info("captured CN request", "method", request.Method, "path", request.URL.Path)
				}
			}
			next.ServeHTTP(writer, request)
		})
	}
}

func redactCNSessionBody(body []byte) string {
	_, payload, ok := splitCNSessionPayload(body)
	if !ok {
		return string(body)
	}
	return "<session-redacted>" + string(payload)
}

func redactCNRequestBody(path string, body []byte) string {
	path = "/" + strings.TrimLeft(path, "/")
	if path == "/loginSDK.php" || strings.HasPrefix(path, "/local/account/") {
		return "<login-redacted>"
	}
	return redactCNSessionBody(body)
}

func (r *recorder) append(entry capture) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	file, err := os.OpenFile(r.path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	// The capture is diagnostic runtime evidence, not transactional state. Closing
	// the handle preserves normal-process durability without blocking every client
	// request on a physical-disk flush.
	return file.Close()
}

func isBulkResourceRequest(request *http.Request) bool {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		return false
	}
	path := "/" + strings.TrimLeft(request.URL.Path, "/")
	return strings.HasPrefix(path, "/local/resources/") ||
		strings.HasPrefix(path, "/local/version/")
}
