package multiplayer

import (
	"bufio"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// Parallel peer writers must not mistake another probe's brief lock for a
// production write performed under hub.mu.
var hubWriteProbe sync.Mutex

type hubCheckingConn struct {
	net.Conn
	hub     *Hub
	session *roomSession
	locked  bool
	output  strings.Builder
}

func (connection *hubCheckingConn) Write(p []byte) (int, error) {
	hubWriteProbe.Lock()
	if connection.hub.mu.TryLock() {
		connection.hub.mu.Unlock()
	} else {
		connection.locked = true
	}
	if connection.session != nil {
		if connection.session.mu.TryLock() {
			connection.session.mu.Unlock()
		} else {
			connection.locked = true
		}
	}
	hubWriteProbe.Unlock()
	return connection.output.Write(p)
}

func TestRoomInitialFramesDoNotWriteUnderStateLocks(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	hub := NewHub()
	connection := &hubCheckingConn{Conn: left, hub: hub}
	client := &clientConn{conn: connection}
	hub.rooms[1] = &room{RoomSnapshot: RoomSnapshot{RoomID: 1, State: RoomStateOpen}}
	session := hub.lockRoomSession(1)
	connection.session = session.owner
	if err := client.writeInitialRoomFramesAndUnlock(session, []battleFrame{{"RoomCreateRequestResult", "0"}, {"RoomMember", "1"}}); err != nil {
		t.Fatal(err)
	}
	if connection.locked || connection.output.String() != "RoomCreateRequestResult{\n0\n}\nRoomMember{\n1\n}\n" {
		t.Fatalf("initial frames blocked other rooms or lost ordering: locked=%v output=%q", connection.locked, connection.output.String())
	}
}

func TestBattleBroadcastDoesNotWaitForSlowPeer(t *testing.T) {
	hub := NewHub()
	server := &Server{hub: hub, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	slow, slowPeer := net.Pipe()
	fast, fastPeer := net.Pipe()
	defer slow.Close()
	defer slowPeer.Close()
	defer fast.Close()
	defer fastPeer.Close()
	connections := []*clientConn{{conn: slow}, {conn: fast}}
	hub.mu.Lock()
	first := reserveRoomFramesLocked(connections,
		battleFrame{"ApiCardPlayR", "first"}, battleFrame{"ApiUserAttack", "second"})
	second := reserveRoomFramesLocked(connections, battleFrame{"ApiEnemyPhase", "third"})
	hub.mu.Unlock()
	done := make(chan struct{}, 2)
	// Deliver the later state first to exercise concurrent handler scheduling.
	go func() {
		broadcastRoomFrames(server, 123, second)
		done <- struct{}{}
	}()
	go func() {
		broadcastRoomFrames(server, 123, first)
		done <- struct{}{}
	}()
	defer func() {
		_ = slowPeer.Close()
		_ = fastPeer.Close()
		<-done
		<-done
	}()
	// The second peer receives both ordered batches while the first reads nothing.
	for _, peer := range []net.Conn{fastPeer, slowPeer} {
		_ = peer.SetReadDeadline(time.Now().Add(time.Second))
		reader := bufio.NewReader(peer)
		for _, want := range [][2]string{{"ApiCardPlayR", "first"}, {"ApiUserAttack", "second"}, {"ApiEnemyPhase", "third"}} {
			method, payload, err := readFrame(reader)
			if err != nil || method != want[0] || payload != want[1] {
				t.Fatalf("peer blocked or state order changed: %s %s %v; want %v", method, payload, err, want)
			}
		}
	}
}

type endlessFrameLine struct{ read int }

func (stream *endlessFrameLine) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	stream.read += len(p)
	return len(p), nil
}

func TestFrameLimitStopsUnterminatedHeaderAndPayload(t *testing.T) {
	for _, prefix := range []string{"", "CardPlay{\n"} {
		stream := &endlessFrameLine{}
		reader := bufio.NewReaderSize(io.MultiReader(strings.NewReader(prefix), stream), 4096)
		if _, _, err := readFrame(reader); err == nil || !strings.Contains(err.Error(), "too large") {
			t.Fatalf("oversized line not rejected: %v", err)
		}
		if stream.read > maxFrameBytes+4096 {
			t.Fatalf("read %d bytes before enforcing the %d-byte limit", stream.read, maxFrameBytes)
		}
	}
	reader := bufio.NewReader(strings.NewReader("Ping{\n}\nChat{\n123\n}\n"))
	for _, want := range [][2]string{{"Ping", ""}, {"Chat", "123"}} {
		method, payload, err := readFrame(reader)
		if err != nil || method != want[0] || payload != want[1] {
			t.Fatalf("ordinary frame failed: %s %s %v", method, payload, err)
		}
	}
}
