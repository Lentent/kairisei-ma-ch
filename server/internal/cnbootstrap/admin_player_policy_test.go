package cnbootstrap

import (
	"encoding/json"
	"net/http"
	"testing"

	adminapi "kairisei.local/server/internal/admin"
	"kairisei.local/server/internal/testfixture"
)

func auditCompletePlayerPolicy(t *testing.T, h http.Handler) {
	t.Helper()
	a := h.(interface{ AdminHandler() http.Handler }).AdminHandler()
	var data struct {
		Config   adminapi.PlayerPolicy `json:"config"`
		Revision int                   `json:"revision"`
	}
	if err := json.Unmarshal(testfixture.CallContentAdmin(t, a, "GET", "/api/player-policy", nil, 200), &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Config.Navigators) == 0 || len(data.Config.Login.Cycle) == 0 || len(data.Config.Login.Beginner) == 0 || len(data.Config.Login.Total) == 0 {
		t.Fatal("missing public policies")
	}
	testfixture.CallContentAdmin(t, a, "PUT", "/api/player-policy", map[string]any{"expected_revision": data.Revision, "config": data.Config}, 200)
	t.Logf("player policy: %d cycle / %d beginner / %d total rewards, %d purchasable navigators", len(data.Config.Login.Cycle), len(data.Config.Login.Beginner), len(data.Config.Login.Total), len(data.Config.Navigators))
}
