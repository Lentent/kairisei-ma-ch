package multiplayer

import (
	"maps"
	"sync"
	"sync/atomic"
)

// A session owns one room through lobby, battle and terminal recovery. Its
// mutex serializes members, engine/RNG, phase barriers and outbound ordering.
// Hub.mu owns only registry/credential entries. Never wait for a session while
// holding Hub.mu; a session may briefly acquire Hub.mu to publish a transition.
type roomSession struct {
	mu   sync.Mutex
	view atomic.Pointer[roomReadModel]
}

// A published read model is immutable. Searching/observing other rooms never
// waits for an engine phase or a completion write. Entry still checks the live
// room under its session lock; a search result is not an entry authorization.
type roomReadModel struct {
	snapshot     RoomSnapshot
	reservations map[int]roomReservation
	password     string
	activity     RoomActivity
}

type lockedRoomSession struct {
	hub       *Hub
	owner     *roomSession
	room      *room
	completed *completedBattle
	released  bool
}

// Unlock can be called before I/O as well as deferred for error paths. The
// lease is local to one goroutine and must not be copied or shared.
func (session *lockedRoomSession) Unlock() {
	if session.released {
		return
	}
	session.released = true
	if session.owner != nil {
		if session.room != nil {
			session.owner.publish(session.room)
		}
		session.owner.mu.Unlock()
	}
}

// A reserved account transaction releases the room before calling account
// code, then returns to this exact owner to finish its barrier. Keep owning the
// old session even if the registry entry disappeared; callers recheck identity.
func (session *lockedRoomSession) relock() *lockedRoomSession {
	session.owner.mu.Lock()
	h := session.hub
	h.mu.RLock()
	current, completed := h.rooms[session.room.RoomID], h.completed[session.room.RoomID]
	if current != nil && current.session != session.owner {
		current = nil
	}
	if completed != nil && completed.session != session.owner {
		completed = nil
	}
	h.mu.RUnlock()
	return &lockedRoomSession{hub: h, owner: session.owner, room: current, completed: completed}
}

// Establish the owner once under the registry lock. This method only touches
// identity pointers, never mutable gameplay fields or another session's lock.
func (h *Hub) roomSessionLocked(id int64) *roomSession {
	if current := h.rooms[id]; current != nil {
		h.configFrozen = true
		if current.session == nil {
			current.session = &roomSession{}
		}
		return current.session
	}
	if completed := h.completed[id]; completed != nil {
		h.configFrozen = true
		if completed.session == nil {
			completed.session = &roomSession{}
		}
		return completed.session
	}
	return nil
}

func (h *Hub) lockRoomSession(id int64) *lockedRoomSession {
	for {
		h.mu.Lock()
		owner := h.roomSessionLocked(id)
		h.mu.Unlock()
		if owner == nil {
			return &lockedRoomSession{hub: h}
		}
		owner.mu.Lock()
		h.mu.Lock()
		currentOwner := h.roomSessionLocked(id)
		current, completed := h.rooms[id], h.completed[id]
		h.mu.Unlock()
		if currentOwner != owner {
			owner.mu.Unlock()
			continue // Removed/expired while waiting; do not act on a stale room.
		}
		if current != nil && owner.view.Load() == nil {
			owner.publish(current)
		}
		return &lockedRoomSession{hub: h, owner: owner, room: current, completed: completed}
	}
}

func (session *roomSession) publish(current *room) {
	row := RoomActivity{RoomID: current.RoomID, BossID: current.BossID, State: current.State,
		GameSpeed: current.GameSpeed, Turn: current.turnNumber, Wave: current.battleIndex + 1, Players: []int{}}
	for _, member := range current.Members {
		if current.connections[member.MemberType] != nil && member.UserID > 0 && member.UserID < 1900000000 {
			row.Players = append(row.Players, member.UserID)
		} else if _, pending := current.disconnectedUntil[member.MemberType]; pending {
			row.Disconnected++
		} else {
			row.AI++
		}
	}
	session.view.Store(&roomReadModel{snapshot: cloneRoomSnapshot(current.RoomSnapshot),
		reservations: maps.Clone(current.reservations), password: current.password(), activity: row})
}

func (h *Hub) roomViews() map[int64]*roomReadModel {
	h.mu.Lock()
	owners := make(map[int64]*roomSession, len(h.rooms))
	for id := range h.rooms {
		owners[id] = h.roomSessionLocked(id)
	}
	h.mu.Unlock()
	views := make(map[int64]*roomReadModel, len(owners))
	for id, owner := range owners {
		view := owner.view.Load()
		if view == nil {
			session := h.lockRoomSession(id)
			session.Unlock()
			view = owner.view.Load()
		}
		if view != nil && view.snapshot.State != RoomStateClosed {
			views[id] = view
		}
	}
	return views
}

func (h *Hub) roomView(id int64) *roomReadModel {
	h.mu.Lock()
	var owner *roomSession
	if h.rooms[id] != nil {
		owner = h.roomSessionLocked(id)
	}
	h.mu.Unlock()
	if owner == nil {
		return nil
	}
	if owner.view.Load() == nil {
		session := h.lockRoomSession(id)
		session.Unlock()
	}
	return owner.view.Load()
}

// Caller owns the session. Closing the registry entry does not acquire another
// room's lock, and any waiter must recheck this entry before using its pointer.
func (h *Hub) removeRoom(current *room) {
	current.State = RoomStateClosed
	h.mu.Lock()
	if h.rooms[current.RoomID] == current {
		delete(h.rooms, current.RoomID)
	}
	h.mu.Unlock()
}

func (h *Hub) reserveRoomID() (int64, error) {
	h.mu.Lock()
	minimum, repository := h.nextRoomID, h.repository
	if repository == nil {
		h.nextRoomID++
		h.mu.Unlock()
		return minimum, nil
	}
	h.mu.Unlock()
	id, err := repository.NextRoomID(minimum)
	if err != nil {
		return 0, err
	}
	h.mu.Lock()
	h.nextRoomID = max(h.nextRoomID, id+1)
	h.mu.Unlock()
	return id, nil
}
