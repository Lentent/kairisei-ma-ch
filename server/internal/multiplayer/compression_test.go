package multiplayer

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/internal/wirecompression"
)

func TestCompressedBattleFrames(t *testing.T) {
	frames := []battleFrame{{"Chat", "繁體／日本語／🙂"}, {"Empty", ""}, {"ApiUserAttack", strings.Repeat("601001,0,1,2345\n", 500) + "601001,0,0,0"}}
	for _, frame := range frames {
		p := &preparedBattleFrame{frame: frame}
		raw, wire, err := p.bytes(true)
		if err != nil {
			t.Fatal(err)
		}
		method, payload, err := readFrame(bufio.NewReader(strings.NewReader(wire)))
		if method == gzipFrameMethod {
			method, payload, err = decodeGZIPFrame(payload)
		}
		if err != nil || method != frame.method || payload != frame.payload {
			t.Fatalf("decoded frame changed: %s %v", frame.method, err)
		}
		if len(raw) >= 1024 && len(wire) >= len(raw) {
			t.Fatal("large fixture did not compress")
		}
	}
	raw := []byte("ApiUserAttack{\n" + strings.Repeat("row\n", 400) + "}\n")
	packed, _ := wirecompression.Compress(raw)
	envelope := func(p []byte, size int) string {
		return strconv.Itoa(size) + "," + base64.StdEncoding.EncodeToString(p)
	}
	corrupt := append([]byte(nil), packed...)
	corrupt[len(corrupt)-8] ^= 1
	joined := append(append([]byte(nil), packed...), packed...)
	for _, bad := range []string{envelope(corrupt, len(raw)), envelope(packed[:len(packed)-2], len(raw)), envelope(joined, len(raw)), envelope(packed, len(raw)-1), envelope(packed, maxFrameBytes+1), "01,AAAA"} {
		if _, _, err := decodeGZIPFrame(bad); err == nil {
			t.Fatal("accepted corrupt/oversized frame")
		}
	}
}

func TestBattleCompressionNegotiation(t *testing.T) {
	serverConn, peer := net.Pipe()
	defer peer.Close()
	defer serverConn.Close()
	_ = peer.SetDeadline(time.Now().Add(3 * time.Second))
	server := &Server{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	c := &clientConn{server: server, conn: serverConn}
	done := make(chan error, 1)
	go func() { done <- c.handle("Ping", gzipCapability) }()
	reader := bufio.NewReader(peer)
	method, body, err := readFrame(reader)
	if err != nil || method != "LocalCompression" || body != gzipCapability {
		t.Fatalf("capability acknowledgement: %s %s %v", method, body, err)
	}
	method, _, err = readFrame(reader)
	if err != nil || method != "Pong" {
		t.Fatal("native Pong was changed", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	// Negotiation state belongs to the connection, never the room/account.
	if !c.gzipFrames || (&clientConn{}).gzipFrames {
		t.Fatal("invalid negotiation state")
	}
	raw := (&preparedBattleFrame{frame: battleFrame{"Ping", ""}})
	_, wire, _ := raw.bytes(true)
	if wire != "Ping{\n}\n" {
		t.Fatal("small heartbeat changed")
	}
	fresh := &clientConn{}
	if err := fresh.handle(gzipFrameMethod, "2000,AAAA"); err == nil {
		t.Fatal("accepted compression without negotiation")
	}
}

// Optional cross-runtime exchange with ProbeNetworkCompression.cs. Inputs and
// outputs remain under _local; no live server/account is needed.
func TestBattleCompressionClientVectors(t *testing.T) {
	root := os.Getenv("CN602_COMPRESSION_VECTORS")
	if root == "" {
		t.Skip("offline client exchange not requested")
	}
	root, err := filepath.Abs(root)
	if err != nil || !strings.Contains(filepath.ToSlash(root), "/_local/transient/") {
		t.Fatal("vectors must stay in project transient")
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	for i, frame := range []battleFrame{{"Probe", ""}, {"Probe", "繁體／日本語／🙂"}, {"Probe", strings.Repeat("601001,0,1,2345\n", 700) + "end"}, {"Probe", strings.Repeat("技能：閃耀，回復\n", 500) + "尾行\n"}} {
		raw, wire, err := (&preparedBattleFrame{frame: frame}).bytes(true)
		if err != nil {
			t.Fatal(err)
		}
		stem := filepath.Join(root, strconv.Itoa(i))
		for ext, data := range map[string]string{".raw": raw, ".wire": wire} {
			path := stem + ext
			if existing, err := os.ReadFile(path); err == nil {
				if string(existing) != data {
					t.Fatal("vector identity changed")
				}
				continue
			}
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
		}
		// C# returns its own GZIP encoding; decode with the production Go path.
		if client, err := os.ReadFile(stem + ".client"); err == nil {
			method, body, err := readFrame(bufio.NewReader(bytes.NewReader(client)))
			if method == gzipFrameMethod {
				method, body, err = decodeGZIPFrame(body)
			}
			if err != nil || method != frame.method || body != frame.payload {
				t.Fatalf("Mono -> Go mismatch %d: %v", i, err)
			}
		}
		if upload, err := os.ReadFile(stem + ".upload.gz"); err == nil {
			decoded, err := wirecompression.Decompress(upload, maxFrameBytes)
			if err != nil || string(decoded) != raw {
				t.Fatal("Mono request GZIP mismatch", err)
			}
		}
	}
	actual, err := filepath.Glob(filepath.Join(root, "actual-*.raw"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range actual {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		method, body, err := readFrame(bufio.NewReader(bytes.NewReader(raw)))
		if err != nil {
			t.Fatal(err)
		}
		original, wire, err := (&preparedBattleFrame{frame: battleFrame{method, body}}).bytes(true)
		if err != nil || original != string(raw) {
			t.Fatal("captured frame reconstruction differs", err)
		}
		stem := strings.TrimSuffix(path, ".raw")
		if err := os.WriteFile(stem+".wire", []byte(wire), 0600); err != nil {
			t.Fatal(err)
		}
		if encoded, err := os.ReadFile(stem + ".client"); err == nil {
			gotMethod, gotBody, err := readFrame(bufio.NewReader(bytes.NewReader(encoded)))
			if gotMethod == gzipFrameMethod {
				gotMethod, gotBody, err = decodeGZIPFrame(gotBody)
			}
			if err != nil || gotMethod != method || gotBody != body {
				t.Fatal("actual client roundtrip differs", path, err)
			}
		}
	}
}
