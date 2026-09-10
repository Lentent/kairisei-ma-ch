package httpapi

import (
	"context"
	"net/http"
)

type gachaPublicationKey struct{}

// WithGachaPublication attaches the adapter's immutable publication snapshot to
// one request. It is transport metadata, never a player-supplied field.
func WithGachaPublication(request *http.Request, published func(int) bool) *http.Request {
	return request.WithContext(context.WithValue(request.Context(), gachaPublicationKey{}, published))
}

func gachaPublished(request *http.Request, gachaID int) bool {
	published, _ := request.Context().Value(gachaPublicationKey{}).(func(int) bool)
	return published == nil || published(gachaID)
}
