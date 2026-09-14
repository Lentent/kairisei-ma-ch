package multiplayer

import (
	"errors"
	"fmt"
	"maps"
)

// roomBarrierReady checks the current live connection set rather than map
// lengths. A member may disconnect after acknowledging a direction, so stale
// keys must never keep a smaller live quorum blocked or make it appear larger.
func roomBarrierReady[T any](connections map[int]*clientConn, acknowledgements map[int]T) bool {
	if len(connections) == 0 {
		return false
	}
	for memberType := range connections {
		if _, acknowledged := acknowledgements[memberType]; !acknowledged {
			return false
		}
	}
	return true
}

func (s *Server) tryAdvanceGameStart(roomID int64) error {
	hub := s.hub
	hub.mu.Lock()
	current, exists := hub.rooms[roomID]
	if !exists || current.State != RoomStateBattle || !current.gameStarted || current.turnPhaseStarted ||
		!roomBarrierReady(current.connections, current.gameStartFinished) {
		hub.mu.Unlock()
		return nil
	}
	if current.engine == nil {
		hub.mu.Unlock()
		return errors.New("GameStartFinish Go battle engine is unavailable")
	}
	results, err := current.engine.TurnPhase()
	if err != nil {
		hub.mu.Unlock()
		return err
	}
	result, err := encodeBattleResults(results)
	if err != nil {
		hub.mu.Unlock()
		return err
	}
	current.turnPhaseStarted = true
	current.turnNumber = 1
	current.engineBattleEnd = current.engine.EndType()
	battleEnd := current.engineBattleEnd
	connectedMembers := len(current.connections)
	connections := append([]*clientConn(nil), roomConnections(current)...)
	deliveries := reserveRoomFramesLocked(connections, battleFrame{"ApiTurnPhase", result})
	hub.mu.Unlock()

	broadcastRoomFrames(s, roomID, deliveries)
	s.logger.Info(
		"local multiplayer Go turn phase broadcast",
		"room_id", roomID,
		"connected_members", connectedMembers,
		"result_rows", len(results),
		"battle_end", battleEnd,
	)
	return nil
}

func (s *Server) tryAdvanceTurnPhase(roomID int64) error {
	hub := s.hub
	hub.mu.Lock()
	current, exists := hub.rooms[roomID]
	if !exists || current.State != RoomStateBattle || !current.turnPhaseStarted || current.userPhaseStarted ||
		!roomBarrierReady(current.connections, current.turnPhaseFinished) {
		hub.mu.Unlock()
		return nil
	}
	if roomContinuePending(current) {
		return s.continueAtBarrierLocked(hub, current)
	}
	if current.engine == nil {
		hub.mu.Unlock()
		return errors.New("TurnPhaseFinish Go battle engine is unavailable")
	}
	if current.engineBattleEnd != 0 {
		return s.completeGoBattleLocked(hub, current)
	}
	results, err := current.engine.UserPhase()
	if err != nil {
		hub.mu.Unlock()
		return err
	}
	result, err := encodeBattleResults(results)
	if err != nil {
		hub.mu.Unlock()
		return err
	}
	current.userPhaseStarted = true
	submissions, err := automaticRoomCardSubmissionFrames(current)
	if err != nil {
		hub.mu.Unlock()
		return err
	}
	connectedMembers := len(current.connections)
	connections := append([]*clientConn(nil), roomConnections(current)...)
	frames := append([]battleFrame{{"ApiUserPhase", result}}, submissions...)
	deliveries := reserveRoomFramesLocked(connections, frames...)
	hub.mu.Unlock()

	broadcastRoomFrames(s, roomID, deliveries)
	s.logger.Info(
		"local multiplayer Go user phase broadcast",
		"room_id", roomID,
		"connected_members", connectedMembers,
		"result_rows", len(results),
	)
	return nil
}

func (s *Server) tryAdvanceCardPlay(roomID int64) error {
	if err := s.submitAutomaticRoomCards(roomID); err != nil {
		return err
	}
	hub := s.hub
	hub.mu.Lock()
	current, exists := hub.rooms[roomID]
	if !exists || current.State != RoomStateBattle || !current.userPhaseStarted || current.userAttackStarted ||
		!roomBarrierReady(current.connections, current.cardPlaySubmissions) {
		hub.mu.Unlock()
		return nil
	}
	if current.engine == nil {
		hub.mu.Unlock()
		return errors.New("CardPlay Go battle engine is unavailable")
	}
	cardPlayResults, err := commitRoomCardPlays(current)
	if err != nil {
		var inputErr *roomCardPlayError
		if errors.As(err, &inputErr) {
			owner := current.connections[inputErr.memberType]
			if owner != nil {
				hub.mu.Unlock()
				// A queued choice can become invalid before the last teammate
				// submits. Detach its owner, then let the existing disconnect
				// path fill that slot and retry the barrier outside the lock.
				s.logger.Warn("local multiplayer queued card play rejected",
					"room_id", roomID, "member_type", inputErr.memberType, "error", err)
				if closeErr := owner.close(true); closeErr != nil {
					s.logger.Warn("local multiplayer rejected input close failed", "room_id", roomID, "error", closeErr)
				}
				return nil
			}
		}
		hub.mu.Unlock()
		return err
	}
	cardPlayResult, err := encodeOptionalBattleResults(cardPlayResults)
	if err != nil {
		hub.mu.Unlock()
		return err
	}
	attackResults, err := current.engine.UserAttack()
	if err != nil {
		hub.mu.Unlock()
		return fmt.Errorf("execute Go user attack: %w", err)
	}
	attackResult, err := encodeBattleResults(attackResults)
	if err != nil {
		hub.mu.Unlock()
		return err
	}
	current.userAttackStarted = true
	current.engineBattleEnd = current.engine.EndType()
	battleEnd := current.engineBattleEnd
	connectedMembers := len(current.connections)
	connections := append([]*clientConn(nil), roomConnections(current)...)
	// Earlier submissions already reserved their confirmation frames. Any
	// remaining automatic input must also precede the shared attack exactly once.
	var frames []battleFrame
	if cardPlayResult != "" {
		frames = append(frames, battleFrame{"ApiCardPlayR", cardPlayResult})
	}
	frames = append(frames, battleFrame{"ApiUserAttack", attackResult})
	deliveries := reserveRoomFramesLocked(connections, frames...)
	hub.mu.Unlock()

	broadcastRoomFrames(s, roomID, deliveries)
	s.logger.Info("local multiplayer Go user attack broadcast",
		"room_id", roomID, "connected_members", connectedMembers,
		"evaluated_members", maxRoomMembers,
		"card_play_result_rows", len(cardPlayResults),
		"card_play_result_rows_csv", battleResultRows(cardPlayResults),
		"attack_result_rows", len(attackResults),
		"attack_result_rows_csv", battleResultRows(attackResults),
		"contract_rows", battleContractRows(attackResults), "battle_end", battleEnd)
	return nil
}

type roomCardPlayError struct {
	memberType int
	err        error
}

func (e *roomCardPlayError) Error() string {
	return fmt.Sprintf("evaluate cards for member %d: %v", e.memberType, e.err)
}

func (e *roomCardPlayError) Unwrap() error { return e.err }

func roomCardPlayPreview(engine *BattleEngine) BattleEngine {
	preview := *engine
	preview.selectedPlays = maps.Clone(engine.selectedPlays)
	if preview.selectedPlays == nil {
		preview.selectedPlays = make(map[int]cardPlaySubmission)
	}
	return preview
}

func submitRoomCardPlay(preview *BattleEngine, memberType int, submission cardPlaySubmission) ([]BattleResult, error) {
	results, err := preview.Submit(memberType, submission)
	if err != nil {
		return nil, err
	}
	if submission.SphereSlot != 0 {
		// Managed HandsData.getUseCost/isCostOver counts both kinds of
		// selected hand slot. Native Submit retains its own accounting;
		// enforce the client's input budget before committing the room.
		sphere := preview.players[memberType-1].Spheres[submission.SphereSlot-1]
		skill, _, err := preview.catalog.SphereSkill(sphere.SphereID)
		if err != nil {
			return nil, err
		}
		if skill.Cost > preview.players[memberType-1].Cost {
			return nil, fmt.Errorf("member %d card and sphere cost exceeds remaining cost", memberType)
		}
	}
	return results, nil
}

func commitRoomCardPlays(current *room) ([]BattleResult, error) {
	// Accepted human and CPU submissions are already committed and broadcast.
	// Fill only remaining inputs before starting the shared UserAttack.
	preview := roomCardPlayPreview(current.engine)
	cardPlayResults := make([]BattleResult, 0, 24)
	for memberType := 1; memberType <= maxRoomMembers; memberType++ {
		if _, committed := preview.selectedPlays[memberType]; committed {
			continue
		}
		submission, submitted := current.cardPlaySubmissions[memberType]
		automaticInput := !submitted || (submission.TimedOut && selectedActionCount(submission) == 0)
		if automaticInput {
			automatic, err := preview.AutoSubmission(memberType)
			if err != nil {
				return nil, fmt.Errorf("select automatic cards for member %d: %w", memberType, err)
			}
			submission = automatic
		}
		cardResults, err := submitRoomCardPlay(&preview, memberType, submission)
		if err != nil {
			if !automaticInput {
				return nil, &roomCardPlayError{memberType: memberType, err: err}
			}
			return nil, fmt.Errorf("evaluate cards for member %d: %w", memberType, err)
		}
		cardPlayResults = append(cardPlayResults, cardResults...)
	}
	current.engine.players = preview.players
	current.engine.rng = preview.rng
	current.engine.selectedPlays = preview.selectedPlays
	current.engine.turnActions = preview.turnActions
	current.cardPlaySubmissions = maps.Clone(preview.selectedPlays)
	return cardPlayResults, nil
}

func (s *Server) tryAdvanceUserAttack(roomID int64) error {
	hub := s.hub
	hub.mu.Lock()
	current, exists := hub.rooms[roomID]
	if !exists || current.State != RoomStateBattle || !current.userAttackStarted || current.chaliceUserStarted ||
		!roomBarrierReady(current.connections, current.userAttackFinished) {
		hub.mu.Unlock()
		return nil
	}
	if roomContinuePending(current) {
		return s.continueAtBarrierLocked(hub, current)
	}
	if current.engineBattleEnd != 0 {
		return s.completeGoBattleLocked(hub, current)
	}
	if current.engine == nil {
		hub.mu.Unlock()
		return errors.New("UserAttackFinish Go battle engine is unavailable")
	}
	results, err := current.engine.ExecuteChaliceUserPhase()
	if err != nil {
		hub.mu.Unlock()
		return fmt.Errorf("execute Go chalice user phase: %w", err)
	}
	result, err := encodeOptionalBattleResults(results)
	if err != nil {
		hub.mu.Unlock()
		return err
	}
	current.chaliceUserStarted = true
	current.engineBattleEnd = current.engine.EndType()
	battleEnd := current.engineBattleEnd
	connections := append([]*clientConn(nil), roomConnections(current)...)
	deliveries := reserveRoomFramesLocked(connections, battleFrame{"ApiChaliceSphrExecUserPhase", result})
	hub.mu.Unlock()

	broadcastRoomFrames(s, roomID, deliveries)
	s.logger.Info(
		"local multiplayer Go chalice user phase broadcast",
		"room_id", roomID,
		"result_rows", len(results),
		"battle_end", battleEnd,
	)
	return nil
}

func (s *Server) tryAdvanceChaliceUser(roomID int64) error {
	hub := s.hub
	hub.mu.Lock()
	current, exists := hub.rooms[roomID]
	if !exists || current.State != RoomStateBattle || !current.chaliceUserStarted || current.enemyPhaseStarted ||
		!roomBarrierReady(current.connections, current.chaliceUserFinished) {
		hub.mu.Unlock()
		return nil
	}
	if roomContinuePending(current) {
		return s.continueAtBarrierLocked(hub, current)
	}
	if current.engineBattleEnd != 0 {
		return s.completeGoBattleLocked(hub, current)
	}
	if current.engine == nil {
		hub.mu.Unlock()
		return errors.New("ChaliceSphrExecUserPhaseFinish Go battle engine is unavailable")
	}
	results, err := current.engine.EnemyPhase()
	if err != nil {
		hub.mu.Unlock()
		return fmt.Errorf("execute Go enemy phase: %w", err)
	}
	result, err := encodeBattleResults(results)
	if err != nil {
		hub.mu.Unlock()
		return err
	}
	current.enemyPhaseStarted = true
	current.engineBattleEnd = current.engine.EndType()
	battleEnd := current.engineBattleEnd
	connections := append([]*clientConn(nil), roomConnections(current)...)
	deliveries := reserveRoomFramesLocked(connections, battleFrame{"ApiEnemyPhase", result})
	hub.mu.Unlock()

	broadcastRoomFrames(s, roomID, deliveries)
	s.logger.Info(
		"local multiplayer Go enemy phase broadcast",
		"room_id", roomID,
		"result_rows", len(results),
		"contract_rows", battleContractRows(results),
		"battle_end", battleEnd,
	)
	return nil
}

func (s *Server) tryAdvanceEnemyPhase(roomID int64) error {
	hub := s.hub
	hub.mu.Lock()
	current, exists := hub.rooms[roomID]
	if !exists || current.State != RoomStateBattle || !current.enemyPhaseStarted || current.chaliceEnemyStarted ||
		!roomBarrierReady(current.connections, current.enemyPhaseFinished) {
		hub.mu.Unlock()
		return nil
	}
	if roomContinuePending(current) {
		return s.continueAtBarrierLocked(hub, current)
	}
	if current.engineBattleEnd != 0 {
		return s.completeGoBattleLocked(hub, current)
	}
	if current.engine == nil {
		hub.mu.Unlock()
		return errors.New("EnemyPhaseFinish Go battle engine is unavailable")
	}
	results, err := current.engine.ExecuteChaliceEnemyPhase()
	if err != nil {
		hub.mu.Unlock()
		return fmt.Errorf("execute Go chalice enemy phase: %w", err)
	}
	result, err := encodeOptionalBattleResults(results)
	if err != nil {
		hub.mu.Unlock()
		return err
	}
	current.chaliceEnemyStarted = true
	current.engineBattleEnd = current.engine.EndType()
	connections := append([]*clientConn(nil), roomConnections(current)...)
	deliveries := reserveRoomFramesLocked(connections, battleFrame{"ApiChaliceSphrExecEnemyPhase", result})
	hub.mu.Unlock()

	broadcastRoomFrames(s, roomID, deliveries)
	s.logger.Info(
		"local multiplayer Go chalice enemy phase broadcast",
		"room_id", roomID,
		"result_rows", len(results),
	)
	return nil
}

func (s *Server) tryAdvanceChaliceEnemy(roomID int64) error {
	hub := s.hub
	hub.mu.Lock()
	current, exists := hub.rooms[roomID]
	if !exists || current.State != RoomStateBattle || !current.chaliceEnemyStarted ||
		!roomBarrierReady(current.connections, current.chaliceEnemyFinished) {
		hub.mu.Unlock()
		return nil
	}
	if roomContinuePending(current) {
		return s.continueAtBarrierLocked(hub, current)
	}
	if current.engineBattleEnd != 0 {
		return s.completeGoBattleLocked(hub, current)
	}
	if current.engine == nil {
		hub.mu.Unlock()
		return errors.New("ChaliceSphrExecEnemyPhaseFinish Go battle engine is unavailable")
	}
	results, err := current.engine.TurnPhase()
	if err != nil {
		hub.mu.Unlock()
		return err
	}
	result, err := encodeBattleResults(results)
	if err != nil {
		hub.mu.Unlock()
		return err
	}
	current.engineBattleEnd = current.engine.EndType()
	battleEnd := current.engineBattleEnd
	current.turnNumber++
	current.turnPhaseFinished = make(map[int]bool, len(current.connections))
	current.userPhaseStarted = false
	current.cardPlayPlans = make(map[int]cardPlaySubmission, len(current.connections))
	current.cardPlaySubmissions = make(map[int]cardPlaySubmission, len(current.connections))
	current.userAttackStarted = false
	current.userAttackFinished = make(map[int]bool, len(current.connections))
	current.chaliceUserStarted = false
	current.chaliceUserFinished = make(map[int]bool, len(current.connections))
	current.enemyPhaseStarted = false
	current.enemyPhaseFinished = make(map[int]bool, len(current.connections))
	current.chaliceEnemyStarted = false
	current.chaliceEnemyFinished = make(map[int]bool, len(current.connections))
	turnNumber := current.turnNumber
	connections := append([]*clientConn(nil), roomConnections(current)...)
	deliveries := reserveRoomFramesLocked(connections, battleFrame{"ApiTurnPhase", result})
	hub.mu.Unlock()

	broadcastRoomFrames(s, roomID, deliveries)
	s.logger.Info(
		"local multiplayer next Go turn phase broadcast",
		"room_id", roomID,
		"turn", turnNumber,
		"result_rows", len(results),
		"battle_end", battleEnd,
	)
	return nil
}

// advanceBattleAfterDisconnect re-evaluates exactly the phase whose live
// quorum may have become complete. It never emits the client's Disconnect
// error callback; the disconnected character remains an engine-controlled CPU
// and may still reclaim its slot through the bounded comeback token.
func (s *Server) advanceBattleAfterDisconnect(roomID int64) error {
	hub := s.hub
	hub.mu.RLock()
	current, exists := hub.rooms[roomID]
	if !exists || current.State != RoomStateBattle || !current.gameStarted || len(current.connections) == 0 {
		hub.mu.RUnlock()
		return nil
	}
	var advance func(int64) error
	switch {
	case current.continuation != nil:
		advance = s.advanceContinue
	case current.nextBattlePending:
		advance = s.tryAdvanceNextBattle
	case !current.turnPhaseStarted:
		advance = s.tryAdvanceGameStart
	case !current.userPhaseStarted:
		advance = s.tryAdvanceTurnPhase
	case !current.userAttackStarted:
		advance = s.tryAdvanceCardPlay
	case !current.chaliceUserStarted:
		advance = s.tryAdvanceUserAttack
	case !current.enemyPhaseStarted:
		advance = s.tryAdvanceChaliceUser
	case !current.chaliceEnemyStarted:
		advance = s.tryAdvanceEnemyPhase
	default:
		advance = s.tryAdvanceChaliceEnemy
	}
	hub.mu.RUnlock()
	return advance(roomID)
}
