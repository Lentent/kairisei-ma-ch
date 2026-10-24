package multiplayer

import "time"

// Called under the session at creation. Lobby activity and cancelled countdowns
// keep the original deadline; automatic AI rooms skip this lobby altogether.
func (s *Server) scheduleWaitingRoomExpiry(current *room) {
	current.waitingExpiresAt = time.Now().Add(waitingRoomLifetime)
	roomID := current.RoomID
	current.waitingTimer = time.AfterFunc(waitingRoomLifetime, func() {
		if !s.beginWork() {
			return
		}
		defer s.workers.Done()
		s.expireWaitingRoom(roomID)
	})
}

func stopWaitingRoomExpiryLocked(current *room) {
	if current.waitingTimer != nil {
		current.waitingTimer.Stop()
		current.waitingTimer = nil
	}
	current.waitingExpiresAt = time.Time{}
}

func (s *Server) expireWaitingRoom(roomID int64) {
	hub := s.hub
	session := hub.lockRoomSession(roomID)
	defer func() { session.Unlock() }()
	for {
		current := session.room
		if current == nil || (current.State != RoomStateOpen && current.State != RoomStateCountdown) ||
			current.waitingExpiresAt.IsZero() || time.Now().Before(current.waitingExpiresAt) {
			return
		}
		// Account callbacks release the session. Let an in-flight start finish
		// before deciding whether this is still a lobby; never discard a debit.
		if done := current.startDone; done != nil {
			session.Unlock()
			<-done
			session = hub.lockRoomSession(roomID)
			continue
		}
		connections := roomConnections(current)
		hub.removeRoom(current)
		deliveries := reserveRoomFramesLocked(connections, battleFrame{"RoomDelete", ""})
		session.Unlock()
		broadcastRoomFrames(s, roomID, deliveries)
		for _, connection := range connections {
			_ = connection.close(false)
		}
		s.logger.Info("local multiplayer waiting room expired", "room_id", roomID)
		return
	}
}
