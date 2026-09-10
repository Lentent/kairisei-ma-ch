package multiplayer

import (
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
)

type battleFrame struct {
	method  string
	payload string
}

// Reserve while holding hub.mu, in the same order as the state changes. Sending
// waits only after releasing hub.mu; neither a socket nor another writer may
// block the global room lock. Each reservation must be sent exactly once.
type frameDelivery struct {
	connection *clientConn
	frames     []battleFrame
	previous   <-chan struct{}
	done       chan struct{}
}

func (c *clientConn) reserveFramesLocked(frames []battleFrame) frameDelivery {
	delivery := frameDelivery{connection: c, frames: frames, previous: c.lastDelivery, done: make(chan struct{})}
	c.lastDelivery = delivery.done
	return delivery
}

func (delivery frameDelivery) send() error {
	if delivery.previous != nil {
		<-delivery.previous
	}
	defer close(delivery.done)
	c := delivery.connection
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	for _, frame := range delivery.frames {
		if err := c.writeFrameLocked(frame.method, frame.payload); err != nil {
			return err
		}
	}
	return nil
}

func reserveRoomFramesLocked(connections []*clientConn, frames ...battleFrame) []frameDelivery {
	if len(frames) == 0 {
		return nil
	}
	deliveries := make([]frameDelivery, 0, len(connections))
	for _, connection := range connections {
		deliveries = append(deliveries, connection.reserveFramesLocked(frames))
	}
	return deliveries
}

// Called only for a newly joined connection while holding hub.mu.
func (c *clientConn) writeInitialRoomFramesAndUnlock(hub *Hub, frames []battleFrame) error {
	delivery := c.reserveFramesLocked(frames)
	hub.mu.Unlock()
	return delivery.send()
}

// Original Start emits recovery HP rows before 3af95's CARD identities.
// Keep the same assembly for first start, next wave and API comparisons.
func roomStartResult(current *room, results []BattleResult) (string, error) {
	deck, err := roomDeckRows(current)
	if err != nil {
		return "", err
	}
	var rows []string
	for len(results) > 0 && results[0].Command == resultHP {
		row, err := results[0].CSV()
		if err != nil {
			return "", err
		}
		rows = append(rows, row)
		results = results[1:]
	}
	rows = append(rows, deck...)
	tail, err := encodeOptionalBattleResults(results)
	if err != nil {
		return "", err
	}
	if tail != "" {
		rows = append(rows, tail)
	}
	return strings.Join(rows, "\n"), nil
}

func roomDeckRows(current *room) ([]string, error) {
	if current == nil || len(current.Members) != maxRoomMembers {
		return nil, errors.New("battle room party is incomplete")
	}
	rows := make([]string, 0, maxRoomMembers*10)
	for _, member := range current.Members {
		if len(member.DeckCards) != 10 {
			return nil, errors.New("battle game start member deck is incomplete")
		}
		for index, card := range member.DeckCards {
			if card.CardType != index+1 || card.CardID <= 0 || card.Level <= 0 {
				return nil, errors.New("battle game start member card is invalid")
			}
			row, err := cardIdentityResult(member.MemberType, card).CSV()
			if err != nil {
				return nil, err
			}
			rows = append(rows, row)
		}
		if err := validateSupportCards(member.SupportCards); err != nil {
			return nil, err
		}
		for _, card := range member.SupportCards {
			row, err := cardIdentityResult(member.MemberType, card).CSV()
			if err != nil {
				return nil, err
			}
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func broadcastRoomFrames(server *Server, roomID int64, deliveries []frameDelivery) {
	for index, err := range sendRoomFrames(deliveries) {
		if err != nil {
			server.logger.Warn("broadcast room frames failed", "room_id", roomID, "method", deliveries[index].frames[0].method, "error", err)
		}
	}
}

func sendRoomFrames(deliveries []frameDelivery) []error {
	// At most four writers, with independent deadlines. A slow peer must not
	// delay directions to healthy peers. No persistent workers or output queue.
	failures := make([]error, len(deliveries))
	var pending sync.WaitGroup
	for index, delivery := range deliveries {
		pending.Add(1)
		go func() {
			defer pending.Done()
			failures[index] = delivery.send()
		}()
	}
	pending.Wait()
	return failures
}

// completeGoBattleLocked consumes hub.mu and always releases it.
func (s *Server) completeGoBattleLocked(hub *Hub, current *room) error {
	if current.nextBattlePending {
		hub.mu.Unlock()
		return nil
	}
	endType := current.engineBattleEnd
	connections := append([]*clientConn(nil), roomConnections(current)...)
	roomID := current.RoomID
	connectedMembers := len(connections)
	if next, ok := roomNextBattle(current); ok {
		recordRoomWaveDrops(current)
		current.nextBattlePending, current.nextBattleIndex = true, next
		current.gameNextFinished = make(map[int]bool)
		payload := joinCSV(strconv.Itoa(next), strconv.Itoa(roomBattleProgress(current, next)))
		deliveries := reserveRoomFramesLocked(connections, battleFrame{"GameNextStart", payload})
		hub.mu.Unlock()
		broadcastRoomFrames(s, roomID, deliveries)
		return nil
	}
	if err := completeBattleLocked(hub, current, time.Now()); err != nil {
		hub.mu.Unlock()
		return err
	}
	deliveries := reserveRoomFramesLocked(connections,
		battleFrame{"ApiGameEnd", joinCSV("2", strconv.Itoa(endType))}, battleFrame{"GameClose", ""})
	hub.mu.Unlock()
	broadcastRoomFrames(s, roomID, deliveries)
	s.logger.Info(
		"local multiplayer Go battle completed",
		"room_id", roomID,
		"connected_members", connectedMembers,
		"battle_end", endType,
	)
	return nil
}
