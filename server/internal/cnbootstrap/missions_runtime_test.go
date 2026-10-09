package cnbootstrap

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
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

func auditCompleteManagedMissions(t *testing.T, handler http.Handler) {
	t.Helper()
	admin := handler.(interface{ AdminHandler() http.Handler }).AdminHandler()
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, httptest.NewRequest("POST", "/loginSDK.php", strings.NewReader(`{"uuid":"00000000-0000-4000-8800-000000000001","clver":"`+cnMinimumClientVersion+`"}`)))
	var login struct {
		Session string `json:"sess_key"`
	}
	if loginResponse.Code != 200 || json.Unmarshal(loginResponse.Body.Bytes(), &login) != nil || login.Session == "" {
		t.Fatalf("mission account login: %s", loginResponse.Body.String())
	}
	call := func(route, body string, wantCode int) map[string]json.RawMessage {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("POST", route, strings.NewReader(login.Session+body)))
		lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
		var common struct {
			Code int `json:"res_code"`
		}
		var result map[string]json.RawMessage
		if wantCode != 0 {
			if w.Code != wantCode || json.Unmarshal(w.Body.Bytes(), &common) != nil || common.Code != wantCode {
				t.Fatalf("%s: expected rejection %d, HTTP %d %s", route, wantCode, w.Code, w.Body.String())
			}
			return result
		}
		if w.Code != 200 || len(lines) != 3 || json.Unmarshal([]byte(lines[0]), &common) != nil || common.Code != wantCode || json.Unmarshal([]byte(lines[1]), &result) != nil {
			t.Fatalf("%s: HTTP %d %s", route, w.Code, w.Body.String())
		}
		return result
	}
	list := func() []gamestate.MissionInfo {
		t.Helper()
		var result []gamestate.MissionInfo
		if err := json.Unmarshal(call("/MissionShow", `{"is_reward":0}`, 0)["missions"], &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	if got := list(); len(got) != 9 {
		t.Fatalf("initial catalog: %+v", got)
	}
	def := game.MissionDefinition{Title: "后台新任务", Description: "新任务说明", Kind: "login", Daily: true, Enabled: true, Target: 1,
		Rewards: []gamestate.Reward{{Type: 10, Num: 7, CardSkillLevels: []int16{}}, {Type: 4, Num: 100, CardSkillLevels: []int16{}}}}
	save := func(revision int, missions []game.MissionDefinition) int {
		t.Helper()
		body := testfixture.CallContentAdmin(t, admin, "PUT", "/api/missions", map[string]any{"expected_revision": revision, "config": map[string]any{"missions": missions}}, 200)
		var response struct {
			Config struct {
				Missions []game.MissionDefinition `json:"missions"`
			} `json:"config"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			t.Fatal(err)
		}
		if len(response.Config.Missions) == 0 {
			return 0
		}
		return response.Config.Missions[0].ID
	}
	id := save(1, []game.MissionDefinition{def})
	got := list()
	if len(got) != 1 || got[0].MissionID != id || got[0].Title != def.Title || got[0].Description != def.Description || got[0].State != 1 || got[0].TabType != 2 || len(got[0].Rewards) != 2 {
		t.Fatalf("admin policy did not reach the cached business handler: %+v", got)
	}
	claimBody, _ := json.Marshal(map[string]any{"missionids": []int{id}})
	call("/MissionReward", string(claimBody), 0)
	call("/MissionReward", string(claimBody), 400)
	call("/MissionReward", `{"missionids":[910001]}`, 400)
	var presents []gamestate.Present
	if err := json.Unmarshal(call("/PresentBoxShow", "", 0)["presents"], &presents); err != nil {
		t.Fatal(err)
	}
	var issued []gamestate.Reward
	for _, p := range presents {
		if p.Comment == def.Title {
			issued = append(issued, p.Reward)
		}
	}
	if len(issued) != 2 || issued[0].Type != 10 || issued[0].Num != 7 || issued[1].Type != 4 || issued[1].Num != 100 {
		t.Fatalf("native gift delivery: %+v", issued)
	}
	save(2, []game.MissionDefinition{})
	if got := list(); len(got) != 0 {
		t.Fatalf("empty policy did not reach cached account: %+v", got)
	}
	newID := save(3, []game.MissionDefinition{def})
	if newID <= id {
		t.Fatal("removed identity was reused")
	}
	if got := list(); len(got) != 1 || got[0].MissionID != newID {
		t.Fatalf("added task not published: %+v", got)
	}
	call("/MissionReward", string(claimBody), 400)
}
