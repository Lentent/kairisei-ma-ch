package multiplayer

import (
	"errors"
	"testing"
)

func TestBannedConnectionRejectedBeforeDispatch(t *testing.T) {
	h := NewHub()
	denied := errors.New("banned")
	if err := h.AttachAccountAccess(func(id int) error {
		if id == 1000001 {
			return denied
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s, err := NewServer(h, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := &clientConn{server: s, userID: 1000001}
	for _, method := range []string{"Ping", "CardPlay", "Comeback", "RoomCreateRequest", "RoomEnterRequest"} {
		if err := c.handle(method, ""); !errors.Is(err, denied) {
			t.Fatalf("%s: %v", method, err)
		}
	}
	if err := h.checkAccountAccess(1000002); err != nil {
		t.Fatal(err)
	}
	if err := h.AttachAccountAccess(func(int) error { return nil }); err == nil {
		t.Fatal("replaced access guard")
	}
}
