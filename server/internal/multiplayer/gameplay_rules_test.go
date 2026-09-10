package multiplayer

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestGameplayRoomThresholds(t *testing.T) {
	h := NewHub()
	h.rooms[1] = &room{RoomSnapshot: RoomSnapshot{RoomID: 1, State: RoomStateOpen,
		NeedHP: 99999, NeedDeckRank: 10, NeedFame: 100,
		Members: []Member{{MemberType: 1, UserID: 1, ArthurType: 1}}}, reservations: map[int]roomReservation{}}
	member := Member{MemberType: 2, UserID: 2, ArthurType: 2, Level: 1, HP: 100,
		LeaderCardID: 10165084, LeaderLevel: 1, LeaderFame: 1, DeckRank: 0,
		PartsIDs: make([]int, 7), DeckHonorIDs: make([]int, 4)}
	for i := 1; i <= 10; i++ {
		member.DeckCards = append(member.DeckCards, BattleCard{CardType: i, CardID: 10165084, Level: 1})
	}
	if _, err := h.Reserve(1, 2, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := h.IssueEnter(1, member); err == nil || errors.Is(err, ErrRoomUnavailable) || errors.Is(err, ErrRoomArthurUnavailable) {
		t.Fatalf("underqualified deck must retain its threshold rejection, got %v", err)
	}
	h.rooms[1].NeedHP, h.rooms[1].NeedDeckRank, h.rooms[1].NeedFame = 0, 0, 0
	if err := h.CancelReservation(1, member.UserID, member.ArthurType); err != nil {
		t.Fatal(err)
	}
	if _, err := h.IssueEnter(1, member); !errors.Is(err, ErrRoomUnavailable) {
		t.Fatalf("lost reservation did not return room availability rejection: %v", err)
	}
	h.rooms[1].Members = append(h.rooms[1].Members, Member{UserID: 3, ArthurType: member.ArthurType})
	if _, err := h.IssueEnter(1, member); !errors.Is(err, ErrRoomArthurUnavailable) {
		t.Fatalf("occupied profession did not return entry rejection: %v", err)
	}
	delete(h.rooms, 1)
	if err := h.CancelReservation(1, member.UserID, member.ArthurType); err != nil {
		t.Fatalf("room removal did not complete cancellation: %v", err)
	}
}

func TestGameplayDistinctActorChain(t *testing.T) {
	root := filepath.FromSlash("../../../_local/control/server/") + string(os.PathSeparator)
	if _, err := os.Stat(root + "cn602-card-master/card.csv"); os.IsNotExist(err) {
		t.Skip("official local master is not present in source-only checkout")
	}
	catalog, err := LoadCombatCatalog(root+"cn602-card-master/card.csv", root+"cn602-battle-master")
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]int, 0, len(catalog.Cards))
	for id := range catalog.Cards {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	chosen := []int{}
	attribute := ""
	base := 0
	for _, id := range ids {
		skill, roles, err := catalog.CardSkill(id, 1)
		if err != nil || skill.Cost > 5 || skill.Cost <= 0 || skill.Target != "ENEMY_ONE" || len(splitCombatAttributes(skill.Attribute)) != 1 || skill.Attribute == "NULL" || len(roles) != 1 || roles[0].Function != "ATTACK_AA" {
			continue
		}
		if len(catalog.PlayerSkills[skill.ID]) != 1 {
			continue
		}
		if len(chosen) == 0 {
			chosen = append(chosen, id)
			attribute = skill.Attribute
			base = catalog.Cards[id].BaseCardID
		} else if skill.Attribute == attribute && catalog.Cards[id].BaseCardID != base {
			chosen = append(chosen, id)
			break
		}
	}
	if len(chosen) != 2 {
		t.Fatal("could not select two distinct official same-element cards")
	}
	members := make([]Member, 4)
	for i := range members {
		members[i] = Member{MemberType: i + 1, ArthurType: i + 1, HP: 100000, Attack: 1, Magic: 1, Mind: 1}
		for slot := 1; slot <= 10; slot++ {
			id := chosen[(slot-1)%2]
			members[i].DeckCards = append(members[i].DeckCards, BattleCard{CardType: slot, CardID: id, Level: catalog.Cards[id].MaxLevel})
		}
	}
	engine, err := newBattleEngine(catalog, RoomSpec{EnemyPartyID: 10000101, Seed: 602, CostInitial: 3, HoldMax: 5}, members)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = engine.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err = engine.TurnPhase(); err != nil {
		t.Fatal(err)
	}
	if _, err = engine.UserPhase(); err != nil {
		t.Fatal(err)
	}
	// Isolate chain accounting with ample cost and HP, independent of battle balance.
	engine.players[0].Hand = [5]int{1, 2}
	engine.players[0].Cost = 10
	engine.enemies[0].HP = 1000000
	engine.enemies[0].MaxHP = 1000000
	// A malformed duplicate selection must not consume cost or commit a play.
	if _, err := engine.Submit(1, cardPlaySubmission{CardTypes: [5]int{1, 1}, Targets: [5]int{5, 5}}); err == nil || engine.players[0].Cost != 10 || len(engine.selectedPlays) != 0 {
		t.Fatal("duplicate card submission was accepted or mutated the turn")
	}
	current := &room{engine: engine, cardPlaySubmissions: make(map[int]cardPlaySubmission)}
	for actor := 1; actor <= 4; actor++ {
		submission := cardPlaySubmission{}
		if actor == 1 {
			submission.CardTypes = [5]int{1, 2}
			submission.Targets = [5]int{5, 5}
		}
		current.cardPlaySubmissions[actor] = submission
	}
	current.cardPlaySubmissions[2] = cardPlaySubmission{CardTypes: [5]int{99}, Targets: [5]int{5}}
	if _, err := commitRoomCardPlays(current); err == nil || engine.players[0].Cost != 10 || len(engine.selectedPlays) != 0 {
		t.Fatal("invalid later member partially committed the room's turn")
	}
	current.cardPlaySubmissions[2] = cardPlaySubmission{}
	plays, err := commitRoomCardPlays(current)
	if err != nil {
		t.Fatal(err)
	}
	passes := 0
	for _, result := range plays {
		if result.Command == resultCardPass {
			passes++
		}
	}
	if passes != 3 {
		t.Fatalf("manual empty selections must pass, not auto-play: got %d passes", passes)
	}
	results, err := engine.UserAttack()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, result := range results {
		if result.Command != resultCardSkill {
			continue
		}
		count++
		t.Logf("cards=%v attribute=%s, actor=%d slot=%d chain=%d", chosen, attribute, result.Args[0], result.Args[1], result.Args[7])
		if result.Args[7] != 0 {
			t.Errorf("only one actor contributed, but native chain=%d; want 0", result.Args[7])
		}
	}
	if count != 2 {
		t.Fatalf("expected two executed cards, got %d", count)
	}
}

func TestPlayedCardsReturnThroughDiscardPool(t *testing.T) {
	engine := &BattleEngine{rng: newXorShift128(602)}
	player := &engine.players[0]
	player.MemberType, player.DrawCount = 1, 10
	for index := range player.Deck {
		player.Deck[index] = BattleCard{CardType: index + 1, CardID: index + 1, Level: 1}
		player.DeckOrder[index] = index
	}
	for index := range player.Hand {
		player.Hand[index] = engine.drawCard(player)
	}
	seenAgain := false
	for turn := 0; turn < 20; turn++ {
		cardType := player.Deck[player.Hand[0]-1].CardType
		engine.removeCardFromHand(1, cardType)
		before := len(player.Discard)
		engine.removeCardFromHand(1, cardType)
		if len(player.Discard) != before {
			t.Fatal("same card discarded twice")
		}
		next := engine.drawCard(player)
		if next == 0 {
			t.Fatal("played card disappeared from the rotation pool")
		}
		if next == 1 {
			seenAgain = true
		}
		player.Hand[0] = next
		locations := make(map[int]int)
		for _, slot := range player.Hand {
			if slot != 0 {
				locations[slot]++
			}
		}
		for _, slot := range player.DeckOrder[player.DrawIndex:player.DrawCount] {
			if slot >= 0 {
				locations[slot+1]++
			}
		}
		for _, slot := range player.Discard {
			locations[slot+1]++
		}
		if len(locations) != 10 {
			t.Fatalf("turn %d lost a deck card: %v", turn, locations)
		}
		for _, count := range locations {
			if count != 1 {
				t.Fatalf("turn %d duplicated a deck card: %v", turn, locations)
			}
		}
	}
	if !seenAgain {
		t.Fatal("first played card was never recycled")
	}
}
