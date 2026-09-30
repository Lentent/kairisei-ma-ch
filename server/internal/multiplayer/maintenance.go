package multiplayer

import "errors"

// Switching to maintenance is allowed only between rooms. Pending credentials
// are revoked so a token obtained before the switch cannot create a room later.
func (h *Hub) SetMaintenance(enabled bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if enabled && (len(h.rooms) != 0 || h.creatingRooms != 0) {
		return errors.New("仍有组队房间或建房请求，请结束房间后再开启维护")
	}
	h.maintenance = enabled
	if enabled {
		clear(h.pending)
	}
	return nil
}
