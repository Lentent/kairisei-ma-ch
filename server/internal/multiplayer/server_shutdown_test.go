package multiplayer

import (
	"testing"
	"time"
)

func TestServerCloseDrainsAdmittedWork(t *testing.T) {
	s, err := NewServer(NewHub(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !s.beginWork() {
		t.Fatal("work rejected before shutdown")
	}
	done := make(chan struct{})
	go func() { _ = s.Close(); close(done) }()
	// Wait for the close admission barrier, without permitting more work.
	deadline := time.After(time.Second)
	for {
		s.mu.Lock()
		closed := s.closed
		s.mu.Unlock()
		if closed {
			break
		}
		select {
		case <-deadline:
			s.workers.Done()
			t.Fatal("close did not begin")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if s.beginWork() {
		s.workers.Done()
		s.workers.Done()
		t.Fatal("work admitted after shutdown")
	}
	select {
	case <-done:
		s.workers.Done()
		t.Fatal("close returned during an active operation")
	default:
	}
	s.workers.Done()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("close did not finish after drain")
	}
}
