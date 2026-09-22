package multiplayer

import (
	"fmt"
	"maps"
)

// CPU and KO members submit as soon as input opens. Living connected humans
// retain manual/timeout selection. KO Submit intentionally emits no native
// rows; their transport acknowledgement must still release the input barrier.
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
		if (!submitted && current.connections[memberType] != nil && engine.players[memberType-1].HP > 0) ||
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
	session := hub.lockRoomSession(roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if !exists || current.State != RoomStateBattle || !current.userPhaseStarted ||
		current.userAttackStarted || len(current.connections) == 0 {
		session.Unlock()
		return nil
	}
	if current.engine == nil {
		session.Unlock()
		return fmt.Errorf("automatic card submission has no battle engine")
	}
	frames, err := automaticRoomCardSubmissionFrames(current)
	if err != nil {
		session.Unlock()
		return err
	}
	deliveries := reserveRoomFramesLocked(roomConnections(current), frames...)
	session.Unlock()
	broadcastRoomFrames(s, roomID, deliveries)
	return nil
}
