package multiplayer

import "errors"

// AttachAccountAccess is configured before the hub starts accepting traffic.
func (h *Hub) AttachAccountAccess(check func(int) error) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.configFrozen || h.accountAccess != nil {
		return errors.New("account access already configured")
	}
	h.accountAccess = check
	return nil
}
func (h *Hub) checkAccountAccess(id int) error {
	h.mu.Lock()
	check := h.accountAccess
	h.mu.Unlock()
	if check != nil {
		return check(id)
	}
	return nil
}
