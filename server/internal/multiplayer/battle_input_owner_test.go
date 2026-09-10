package multiplayer

import (
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"
)

func TestAnimationSkipRequiresReadyCurrentOwner(t *testing.T) {
	for _, method := range []string{"ChaliceSphrSkip", "AwakeSkip"} {
		for _, state := range []string{"guest", "stale_owner", "recovering_owner", "ready_owner"} {
			t.Run(method+"/"+state, func(t *testing.T) {
				hub := NewHub()
				server := &Server{hub: hub, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
				left, peer := net.Pipe()
				defer left.Close()
				defer peer.Close()
				output := &hubCheckingConn{Conn: left, hub: hub}
				ownerOutput := &hubCheckingConn{Conn: left, hub: hub}
				owner := &clientConn{server: server, conn: ownerOutput, roomID: 1, memberType: 1}
				guest := &clientConn{server: server, conn: output, roomID: 1, memberType: 2}
				current := &room{RoomSnapshot: RoomSnapshot{RoomID: 1, State: RoomStateBattle, OwnerMemberType: 1},
					connections: map[int]*clientConn{1: owner, 2: guest}}
				hub.rooms[1] = current
				sender := owner
				switch state {
				case "guest":
					sender = guest
				case "stale_owner":
					sender = &clientConn{server: server, conn: output, roomID: 1, memberType: 1}
				case "recovering_owner":
					owner.comebackPending = true
				}
				err := sender.handle(method, "")
				if state != "ready_owner" {
					if err == nil || output.output.Len() != 0 || ownerOutput.output.Len() != 0 {
						t.Fatalf("unavailable animation controller reached peers: err=%v frames=%q", err, output.output.String())
					}
					return
				}
				if err != nil || strings.Count(output.output.String(), method+"Exec{") != 1 || output.locked ||
					strings.Count(ownerOutput.output.String(), method+"Exec{") != 1 || ownerOutput.locked {
					t.Fatalf("current owner's presentation control failed: err=%v frames=%q locked=%v", err, output.output.String(), output.locked)
				}
			})
		}
	}
}

func TestCardPlayConfirmationPrecedesTeamAttack(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(map[bool]string{false: "invalid_sender", true: "accepted_before_teammate"}[late], func(t *testing.T) {
			hub := NewHub()
			server := &Server{hub: hub, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			skill := CombatSkillDefinition{ID: 1, FunctionID: 1, Cost: 1, Target: "ENEMY_ONE"}
			engine := newSphereContractEngine(&CombatCatalog{
				Cards:            map[int]CombatCardDefinition{1: {ID: 1, NormalSkillID: 1}},
				Spheres:          map[int]CombatSphereDefinition{2: {ID: 2, SkillID: 2}},
				PlayerSkills:     map[int][]CombatSkillDefinition{1: {skill}, 2: {{ID: 2, FunctionID: 2, Cost: 2, Target: "ENEMY_ONE"}}},
				PlayerSkillRoles: map[int][]CombatSkillRole{1: {{Function: "ATTACK_AA"}}, 2: {{Function: "ATTACK_AA"}}},
			})
			engine.phase = battlePhaseUser
			engine.players[0].Cost = 4
			engine.players[0].Deck[0] = BattleCard{CardType: 1, CardID: 1, Level: 1}
			engine.players[0].Deck[1] = BattleCard{CardType: 2, CardID: 1, Level: 1}
			engine.players[0].Hand = [5]int{1, 2}
			engine.players[0].Spheres[0] = battleSphere{Slot: 1, SphereID: 2, Level: 1, Type: sphereTypeNormal, Count: 1, Playable: true}
			left, peer := net.Pipe()
			defer left.Close()
			defer peer.Close()
			other, otherPeer := net.Pipe()
			defer other.Close()
			defer otherPeer.Close()
			output := &hubCheckingConn{Conn: other, hub: hub}
			first := &clientConn{server: server, conn: &hubCheckingConn{Conn: left, hub: hub}, roomID: 1, memberType: 1}
			last := &clientConn{server: server, conn: output, roomID: 1, memberType: 2}
			current := &room{RoomSnapshot: RoomSnapshot{RoomID: 1, State: RoomStateBattle}, engine: engine,
				gameStarted: true, turnPhaseStarted: true, userPhaseStarted: true,
				connections: map[int]*clientConn{1: first, 2: last}, cardPlaySubmissions: map[int]cardPlaySubmission{},
				disconnectedUntil: map[int]time.Time{}, comebackTokens: map[int]string{1: "first"}}
			hub.rooms[1] = current
			if err := server.submitAutomaticRoomCards(1); err != nil {
				t.Fatal(err)
			}
			output.output.Reset()
			before := engine.rng
			if !late {
				if err := first.handleCardPlay("3,5,0,0,0,0,0,0,0,0,0,0", false); err == nil {
					lastErr := last.handleCardPlay(strings.Repeat("0,", 11)+"0", false)
					t.Fatalf("unavailable first-player card entered queue; last legal sender received: %v", lastErr)
				}
				if _, accepted := current.cardPlaySubmissions[1]; accepted {
					t.Fatal("rejected input remained queued")
				}
				if engine.players[0].Cost != 4 || engine.rng != before || output.output.Len() != 0 {
					t.Fatal("rejected input changed cost, RNG or peer display")
				}
			} else {
				if err := first.handleCardPlay("1,5,2,5,0,0,0,0,0,0,1,5", false); err != nil {
					t.Fatal(err)
				}
				confirmed := output.output.String()
				for _, row := range []string{"22,1,1,5,0,0", "22,1,2,5,0,0", "304,1,1,5"} {
					if strings.Count(confirmed, row) != 1 {
						t.Fatal("teammate did not immediately receive the confirmed cards and sphere", confirmed)
					}
				}
				if current.userAttackStarted || strings.Contains(confirmed, "ApiUserAttack{") || engine.players[0].Cost != 2 || engine.rng != before {
					t.Fatal("submission resolved combat early or failed to retain native cost/RNG")
				}
				selection := engine.selectedPlays[1]
				// Duplicate submits and a late plan cannot replace the visible choice.
				if err := first.handleCardPlay(strings.Repeat("0,", 11)+"0", false); err != nil {
					t.Fatal(err)
				}
				if err := first.handleCardPlayPlan(strings.Repeat("0,", 11) + "0"); err != nil {
					t.Fatal(err)
				}
				if output.output.String() != confirmed || engine.selectedPlays[1] != selection || engine.players[0].Cost != 2 {
					t.Fatal("confirmed submission was replaced, rebroadcast or charged twice")
				}
			}
			if !late {
				// serve() closes the offending sender after its handler returns an error.
				if err := first.close(true); err != nil {
					t.Fatal(err)
				}
			}
			if err := last.handleCardPlay(strings.Repeat("0,", 11)+"0", false); err != nil {
				t.Fatal("valid last sender was blamed for another member's selection", err)
			}
			if (current.connections[1] == nil) == late || current.connections[2] != last || last.closed || !current.userAttackStarted {
				t.Fatal("wrong member detached or surviving room did not advance")
			}
			if !late {
				_ = peer.SetReadDeadline(time.Now().Add(time.Second))
				if _, err := peer.Read(make([]byte, 1)); err != io.EOF {
					t.Fatal("offending socket not closed", err)
				}
			}
			frames := output.output.String()
			if output.locked || strings.Count(frames, "ApiCardPlayR{") < 1 || strings.Count(frames, "ApiUserAttack{") != 1 ||
				strings.LastIndex(frames, "ApiCardPlayR{") > strings.Index(frames, "ApiUserAttack{") {
				t.Fatal("surviving peer lost ordered submissions or single attack delivery", frames)
			}
			if strings.Count(frames, "28,2\n") != 1 || (late && strings.Count(frames, "22,1,1,5,0,0") != 1) {
				t.Fatal("pass confirmation missing or an accepted card was repeated at attack", frames)
			}
			if late {
				if err := first.close(true); err != nil {
					t.Fatal(err)
				}
				if hub.rooms[1] == nil || len(current.connections) != 1 {
					t.Fatal("remaining human lost the room when a teammate disconnected")
				}
				if err := last.handleRetire(""); err != nil {
					t.Fatal(err)
				}
				if hub.rooms[1] != nil {
					t.Fatal("AI and an old comeback reservation kept a room with no humans alive")
				}
			}
		})
	}
}
