package wirecompression

import (
	"log/slog"
	"sync"
	"time"
)

// One summary per active minute, with no per-packet logs or worker goroutine.
type Metrics struct {
	mu                          sync.Mutex
	start                       time.Time
	messages, raw, wire, packed int64
	writeTime                   time.Duration
}

func (m *Metrics) Add(logger *slog.Logger, channel string, raw, wire int, elapsed time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if m.start.IsZero() {
		m.start = now
	}
	m.messages++
	m.raw += int64(raw)
	m.wire += int64(wire)
	if wire < raw {
		m.packed++
	}
	m.writeTime += elapsed
	if now.Sub(m.start) < time.Minute {
		return
	}
	logger.Info("business wire bytes", "channel", channel, "window_ms", now.Sub(m.start).Milliseconds(),
		"messages", m.messages, "compressed_messages", m.packed, "raw_bytes", m.raw,
		"wire_bytes", m.wire, "write_ms", m.writeTime.Milliseconds())
	m.start, m.messages, m.raw, m.wire, m.packed, m.writeTime = now, 0, 0, 0, 0, 0
}
