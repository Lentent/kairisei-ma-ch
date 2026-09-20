package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestAdminJSONRejectsBodyBeyondLimit(t *testing.T) {
	const limit = 64 * 1024
	prefix := `{}` + strings.Repeat(" ", limit-2)
	for _, body := range []string{prefix + " ", prefix + `{}`} {
		request := httptest.NewRequest(http.MethodPost, "http://localhost/api/accounts", strings.NewReader(body))
		var payload struct{}
		if err := decodeAdminJSON(request, &payload); err == nil {
			t.Fatal("oversized body accepted, hiding bytes after the JSON boundary")
		}
	}
	request := httptest.NewRequest(http.MethodPost, "http://localhost/api/accounts", strings.NewReader(prefix))
	var payload struct{}
	if err := decodeAdminJSON(request, &payload); err != nil {
		t.Fatalf("valid body at the limit rejected: %v", err)
	}
}

func TestAdminRejectsReboundHostBeforeServingAccounts(t *testing.T) {
	for _, test := range []struct {
		host    string
		allowed bool
	}{
		{"127.0.0.1:18002", true}, {"localhost:18002", true}, {"[::1]:18002", true},
		{"attacker.example:18002", false}, {"127.0.0.1.attacker.example:18002", false}, {"192.168.1.2:18002", false},
	} {
		t.Run(test.host, func(t *testing.T) {
			reached := false
			handler := adminSecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				reached = true
				w.WriteHeader(http.StatusOK)
			}))
			request := httptest.NewRequest(http.MethodGet, "http://"+test.host+"/api/accounts", nil)
			request.RemoteAddr = "127.0.0.1:12345"
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if reached != test.allowed || (!test.allowed && response.Code != http.StatusForbidden) {
				t.Fatalf("host boundary reached=%v status=%d", reached, response.Code)
			}
		})
	}
}

func TestAdminMailIdempotencyComparesStoredContent(t *testing.T) {
	mail := AdminMailRequest{Title: "cards", Message: "reward"}
	reward := gamestate.Reward{Type: 6, Num: 1, RewardTypeID: 1001, CardLevel: 1, CardFame: 1, CardSkillLevels: []int16{1}}
	existing := gamestate.Present{Title: mail.Title, Comment: mail.Message, Reward: reward}
	if !adminMailPayloadMatches(existing, mail, reward) {
		t.Fatal("identical retry rejected")
	}
	changed := reward
	changed.Num = 100
	if adminMailPayloadMatches(existing, mail, changed) {
		t.Fatal("reused key silently accepted a different quantity")
	}
	mail.Message = "different"
	if adminMailPayloadMatches(existing, mail, reward) {
		t.Fatal("reused key silently accepted different mail content")
	}
}
