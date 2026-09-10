package cnbootstrap

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
)

type cnAuthenticatedUserKey struct{}

func authenticateCNSessions(accounts *cnAccountStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			request.Header.Del(cnAccountUserHeader)
			if request.Method != http.MethodPost || isUnauthenticatedCNPost(request.URL.Path) {
				next.ServeHTTP(writer, request)
				return
			}
			body, err := io.ReadAll(io.LimitReader(request.Body, maxCapturedBody+1))
			if err != nil {
				http.Error(writer, "read CN session", http.StatusBadRequest)
				return
			}
			if len(body) > maxCapturedBody {
				http.Error(writer, "CN session payload too large", http.StatusRequestEntityTooLarge)
				return
			}
			request.Body = io.NopCloser(bytes.NewReader(body))
			sessionKey, _, ok := splitCNSessionPayload(body)
			if !ok {
				// Let the concrete route preserve its own protocol error. This also
				// keeps unknown JSON endpoints observable as 501 instead of turning
				// the authentication layer into an endpoint oracle.
				next.ServeHTTP(writer, request)
				return
			}
			userID, err := accounts.resolveSession(sessionKey)
			if err != nil {
				http.Error(writer, "invalid CN account session", http.StatusUnauthorized)
				return
			}
			request = request.WithContext(
				context.WithValue(request.Context(), cnAuthenticatedUserKey{}, userID),
			)
			next.ServeHTTP(writer, request)
		})
	}
}

func isUnauthenticatedCNPost(path string) bool {
	switch path {
	case "/loginSDK.php", "/mods_switch.php", "/Ping", "/log.php", "/subcribe_push.php":
		return true
	}
	return strings.HasPrefix(path, "/disabled/envsdk/") || strings.HasPrefix(path, "/d/")
}
