package httpapi

import (
	"context"
	"net/http"
)

type cardAcquisitionGroupsKey struct{}

// WithCardAcquisitionGroups uses the same current publication as the battle
// list adapter. nil means all activities; an empty map means none. Normal
// quests retain their account-scoped unlocks independently of operations.
func WithCardAcquisitionGroups(request *http.Request, groups map[int]struct{}) *http.Request {
	return request.WithContext(context.WithValue(request.Context(), cardAcquisitionGroupsKey{}, groups))
}
