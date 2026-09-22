package multiplayer

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"kairisei.local/server/internal/wirecompression"
)

const gzipFrameMethod = "LocalGzip1"
const gzipCapability = "gzip-v1"

// Acknowledgement and mode change share writeMu, so no compressed frame can
// overtake the acknowledgement.
func (c *clientConn) handlePing(payload string) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if payload == gzipCapability && !c.gzipFrames {
		if err := c.writeFrameLocked("LocalCompression", gzipCapability); err != nil {
			return err
		}
		c.gzipFrames = true
	}
	return c.writeFrameLocked("Pong", strconv.FormatInt(time.Now().Unix(), 10))
}

// One immutable message per broadcast, compressed at most once outside state locks.
// Fanout to raw and compressed peers preserves the exact same original frame.
type preparedBattleFrame struct {
	frame             battleFrame
	rawOnce, gzipOnce sync.Once
	raw, gzip         string
	err               error
}

func prepareBattleFrames(frames []battleFrame) []*preparedBattleFrame {
	prepared := make([]*preparedBattleFrame, len(frames))
	for i, frame := range frames {
		prepared[i] = &preparedBattleFrame{frame: frame}
	}
	return prepared
}
func (p *preparedBattleFrame) bytes(compress bool) (string, string, error) {
	p.rawOnce.Do(func() {
		if p.frame.method == "" || strings.ContainsAny(p.frame.method, "{}\r\n") {
			p.err = errors.New("BattleSv response method is invalid")
			return
		}
		p.raw = p.frame.method + "{\n"
		if p.frame.payload != "" {
			p.raw += p.frame.payload + "\n"
		}
		p.raw += "}\n"
		if len(p.raw) > maxFrameBytes || !utf8.ValidString(p.raw) {
			p.err = errors.New("BattleSv response frame is invalid or too large")
		}
	})
	if p.err != nil {
		return "", "", p.err
	}
	if !compress || len(p.raw) < wirecompression.Threshold {
		return p.raw, p.raw, nil
	}
	p.gzipOnce.Do(func() {
		p.gzip = p.raw
		packed, err := wirecompression.Compress([]byte(p.raw))
		if err != nil {
			return
		} // Raw is always valid if compression fails.
		candidate := gzipFrameMethod + "{\n" + strconv.Itoa(len(p.raw)) + "," + base64.StdEncoding.EncodeToString(packed) + "\n}\n"
		if len(candidate)+64 < len(p.raw) {
			p.gzip = candidate
		}
	})
	return p.raw, p.gzip, nil
}

func (c *clientConn) writePreparedFrameLocked(frame *preparedBattleFrame) error {
	if c.closed {
		return net.ErrClosed
	}
	raw, wire, err := frame.bytes(c.gzipFrames)
	if err != nil {
		return err
	}
	start := time.Now()
	_ = c.conn.SetWriteDeadline(start.Add(10 * time.Second))
	n, err := io.WriteString(c.conn, wire)
	if err == nil && n != len(wire) {
		err = io.ErrShortWrite
	}
	if c.server != nil {
		c.server.downloadBytes.Add(c.server.logger, "battlesv_download", len(raw), n, time.Since(start))
	}
	if err != nil {
		_ = c.conn.Close()
	}
	return err
}

func decodeGZIPFrame(payload string) (string, string, error) {
	count, encoded, ok := strings.Cut(payload, ",")
	size, err := strconv.Atoi(count)
	if !ok || err != nil || size < 4 || size > maxFrameBytes || strconv.Itoa(size) != count {
		return "", "", errors.New("invalid compressed frame size")
	}
	packed, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || base64.StdEncoding.EncodeToString(packed) != encoded {
		return "", "", errors.New("invalid compressed frame encoding")
	}
	raw, err := wirecompression.Decompress(packed, size)
	if err != nil || len(raw) != size || !utf8.Valid(raw) {
		return "", "", errors.New("invalid compressed frame contents")
	}
	reader := bufio.NewReader(bytes.NewReader(raw))
	method, body, err := readFrame(reader)
	if err != nil {
		return "", "", err
	}
	if _, err = reader.ReadByte(); err != io.EOF || method == gzipFrameMethod || method == "LocalCompression" {
		return "", "", errors.New("nested or trailing compressed frame")
	}
	return method, body, nil
}
