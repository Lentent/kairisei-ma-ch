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

// Reserve under the room session, in state-transition order. Send only after
// releasing the session; each reservation must be sent exactly once.
type frameDelivery struct {
	connection *clientConn
	frames     []battleFrame
	prepared   []*preparedBattleFrame
	previous   <-chan struct{}
	done       chan struct{}
}

func (c *clientConn) reserveFramesLocked(frames []battleFrame) frameDelivery {
	return c.reservePreparedFramesLocked(frames, prepareBattleFrames(frames))
}

func (c *clientConn) reservePreparedFramesLocked(frames []battleFrame, prepared []*preparedBattleFrame) frameDelivery {
	delivery := frameDelivery{connection: c, frames: frames, prepared: prepared, previous: c.lastDelivery, done: make(chan struct{})}
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
	for _, frame := range delivery.prepared {
		if err := c.writePreparedFrameLocked(frame); err != nil {
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
	prepared := prepareBattleFrames(frames)
	for _, connection := range connections {
		deliveries = append(deliveries, connection.reservePreparedFramesLocked(frames, prepared))
	}
	return deliveries
}

// Called only for a newly joined connection while holding its session.
func (c *clientConn) writeInitialRoomFramesAndUnlock(session *lockedRoomSession, frames []battleFrame) error {
	delivery := c.reserveFramesLocked(frames)
	session.Unlock()
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

// completeGoBattleLocked consumes the room lease and always releases it.
func (s *Server) completeGoBattleLocked(session *lockedRoomSession, current *room) error {
	current.chaliceInput = roomChaliceInput{}
	if current.nextBattlePending {
		session.Unlock()
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
		session.Unlock()
		broadcastRoomFrames(s, roomID, deliveries)
		return nil
	}
	if err := completeBattleLocked(session.hub, current, time.Now()); err != nil {
		session.Unlock()
		return err
	}
	deliveries := reserveRoomFramesLocked(connections,
		battleFrame{"ApiGameEnd", joinCSV("2", strconv.Itoa(endType))}, battleFrame{"GameClose", ""})
	session.Unlock()
	broadcastRoomFrames(s, roomID, deliveries)
	s.logger.Info(
		"local multiplayer Go battle completed",
		"room_id", roomID,
		"connected_members", connectedMembers,
		"battle_end", endType,
	)
	return nil
}
