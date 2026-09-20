package multiplayer

import (
	"fmt"
	"maps"
)

// CPU members submit as soon as input opens. Connected humans, including KO
// members, use the original client's manual or timeout submission. Use the
// same atomic engine update, including cost and target RNG consumption.
func automaticRoomCardSubmissionFrames(current *room) ([]battleFrame, error) {
	engine := roomCardPlayPreview(current.engine)
	submissions := maps.Clone(current.cardPlaySubmissions)
	if submissions == nil {
		submissions = make(map[int]cardPlaySubmission)
	}
	var results []BattleResult
	for memberType := 1; memberType <= maxRoomMembers; memberType++ {
		submission, submitted := current.cardPlaySubmissions[memberType]
		if _, committed := engine.selectedPlays[memberType]; committed {
			continue
		}
		if (!submitted && current.connections[memberType] != nil) ||
			(submitted && (submission.Automatic || !submission.TimedOut || selectedActionCount(submission) != 0)) {
			continue
		}
		selection, err := engine.AutoSubmission(memberType)
		if err != nil {
			return nil, fmt.Errorf("select automatic cards for member %d: %w", memberType, err)
		}
		rows, err := submitRoomCardPlay(&engine, memberType, selection)
		if err != nil {
			return nil, fmt.Errorf("submit automatic cards for member %d: %w", memberType, err)
		}
		submissions[memberType] = selection
		results = append(results, rows...)
	}
	payload, err := encodeOptionalBattleResults(results)
	if err != nil {
		return nil, err
	}
	current.engine.players = engine.players
	current.engine.rng = engine.rng
	current.engine.selectedPlays = engine.selectedPlays
	current.engine.turnActions = engine.turnActions
	current.cardPlaySubmissions = submissions
	if len(results) == 0 {
		return nil, nil
	}
	return []battleFrame{{"ApiCardPlayR", payload}}, nil
}

func (s *Server) submitAutomaticRoomCards(roomID int64) error {
	hub := s.hub
	hub.mu.Lock()
	current, exists := hub.rooms[roomID]
	if !exists || current.State != RoomStateBattle || !current.userPhaseStarted ||
		current.userAttackStarted || len(current.connections) == 0 {
		hub.mu.Unlock()
		return nil
	}
	if current.engine == nil {
		hub.mu.Unlock()
		return fmt.Errorf("automatic card submission has no battle engine")
	}
	frames, err := automaticRoomCardSubmissionFrames(current)
	if err != nil {
		hub.mu.Unlock()
		return err
	}
	deliveries := reserveRoomFramesLocked(roomConnections(current), frames...)
	hub.mu.Unlock()
	broadcastRoomFrames(s, roomID, deliveries)
	return nil
}
