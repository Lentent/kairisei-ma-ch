package multiplayer

import (
	"strings"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestCustomBossAppearanceCountdownKeepsCombatParties(t *testing.T) {
	r := &room{battles: []gamestate.TeamBattleReplayBattle{{EnemyPartyID: 101, EnemyType: 1, ClientEnemyPartyID: 900000000}, {EnemyPartyID: 102, EnemyType: 2, ClientEnemyPartyID: 900000001}}}
	got := roomCountdownPayload(r)
	if !strings.HasPrefix(got, "2,900000000,1,0,,0,,900000001,2,0,,0,") {
		t.Fatal("client countdown did not receive appearance parties:", got)
	}
	if r.battles[0].EnemyPartyID != 101 || r.battles[1].EnemyPartyID != 102 {
		t.Fatal("rendering changed combat party identities")
	}
	r.battles[0].ClientEnemyPartyID = 0
	if !strings.HasPrefix(roomCountdownPayload(r), "2,101,1,") {
		t.Fatal("stock party fallback changed")
	}
}
