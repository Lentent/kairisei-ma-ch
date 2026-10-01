package cnbootstrap

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestMissionListAndClaimThroughClientRoutes(t *testing.T) {
	root := t.TempDir()
	log := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(log, nil, 0600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, root, log)
	call := func(route, body string) []string {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("POST", route, strings.NewReader(cn602LocalSession+body)))
		lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
		if w.Code != 200 || len(lines) < 2 {
			t.Fatalf("%s: %s", route, w.Body.String())
		}
		return lines
	}
	lines := call("/MissionShow", `{"is_reward":0}`)
	var list struct {
		Missions []gamestate.MissionInfo `json:"missions"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Missions) != 9 {
		t.Fatalf("mission count: %d", len(list.Missions))
	}
	for _, mission := range list.Missions {
		wantTab := 0
		if mission.MissionID == 910001 || mission.MissionID == 910002 {
			wantTab = 2
		}
		if mission.TabType != wantTab {
			t.Fatalf("MissionShow mission %d: tab_type=%d, want %d", mission.MissionID, mission.TabType, wantTab)
		}
	}
	lines = call("/MissionReward", `{"missionids":[910001]}`)
	var reward struct {
		Missions []gamestate.MissionInfo `json:"reward_missions"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &reward); err != nil {
		t.Fatal(err)
	}
	if len(reward.Missions) != 1 || reward.Missions[0].State != 2 || reward.Missions[0].TabType != 2 {
		t.Fatalf("claim response: %s", lines[1])
	}
	lines = call("/MissionShow", `{"is_reward":0}`)
	if err := json.Unmarshal([]byte(lines[1]), &list); err != nil {
		t.Fatal(err)
	}
	for _, mission := range list.Missions {
		if mission.MissionID == 910001 && mission.State != 2 {
			t.Fatal("claim not retained between requests")
		}
	}
}
