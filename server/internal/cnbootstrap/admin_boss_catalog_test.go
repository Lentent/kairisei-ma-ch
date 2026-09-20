package cnbootstrap

import (
	"encoding/json"
	"net/http"
	"testing"

	adminapi "kairisei.local/server/internal/admin"
	"kairisei.local/server/internal/testfixture"
)

func auditCompletePastAdmin(t *testing.T, handler http.Handler) {
	t.Helper()
	admin := handler.(interface{ AdminHandler() http.Handler }).AdminHandler()
	var past struct {
		Groups []adminapi.AdminBattleGroup `json:"groups"`
	}
	if err := json.Unmarshal(testfixture.CallContentAdmin(t, admin, "GET", "/api/boss-groups?catalog=past", nil, 200), &past); err != nil {
		t.Fatal(err)
	}
	var drops struct {
		Bosses []adminapi.DropBoss `json:"bosses"`
	}
	if err := json.Unmarshal(testfixture.CallContentAdmin(t, admin, "GET", "/api/boss-drops", nil, 200), &drops); err != nil {
		t.Fatal(err)
	}
	byID := map[int]adminapi.DropBoss{}
	for _, b := range drops.Bosses {
		byID[b.BossID] = b
	}
	var rules struct {
		Rules []struct {
			BossID    int  `json:"boss_id"`
			OwnDeckID int  `json:"own_deck_boss_id"`
			OwnDeck   bool `json:"own_deck"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(testfixture.CallContentAdmin(t, admin, "GET", "/api/boss-rules", nil, 200), &rules); err != nil {
		t.Fatal(err)
	}
	kaladins := 0
	for _, rule := range rules.Rules {
		if rule.BossID >= 30100101 && rule.BossID <= 30100103 {
			if rule.OwnDeckID != rule.BossID+100000000 || rule.OwnDeck {
				t.Fatal("Kaladin must offer the optional mode without opening it by default", rule)
			}
			kaladins++
		}
		if rule.OwnDeckID != 0 {
			byID[rule.OwnDeckID] = byID[rule.BossID]
		}
	}
	if kaladins != 3 {
		t.Fatalf("Kaladin configurable difficulties = %d", kaladins)
	}
	count := 0
	if len(past.Groups) == 0 {
		t.Fatal("empty past admin catalog")
	}
	for _, g := range past.Groups {
		if g.PastName == "" || len(g.BossIDs) == 0 || len(g.Bosses) == 0 {
			t.Fatalf("invalid past group %+v", g)
		}
		for _, id := range g.BossIDs {
			if byID[id].Category != "past" {
				t.Fatalf("past boss %d missing drop editor", id)
			}
			count++
		}
	}
	t.Logf("past admin: %d groups / %d difficulties, all connected to shared drop editor", len(past.Groups), count)
}
