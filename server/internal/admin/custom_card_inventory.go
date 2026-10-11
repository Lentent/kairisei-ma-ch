package admin

import (
	"bytes"
	"strings"

	"kairisei.local/server/internal/protocol"
)

// Older animation patches updated the server's 3D playlist names without
// refreshing resource-set.json. Accept that baseline only if undoing the
// known presentation suffixes reproduces the inventoried file byte for byte.
func customCardInventoryMatches(name string, raw []byte, row map[string]any) bool {
	size, ok := row["bytes"].(float64)
	if !ok {
		return false
	}
	if size == float64(len(raw)) && row["sha256"] == resourceHash(raw) {
		return true
	}
	if name != customRoleCSVPath {
		return false
	}
	var baseline bytes.Buffer
	changed := false
	for _, line := range bytes.SplitAfter(raw, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		terminated := line[len(line)-1] == '\n'
		text := strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r")
		if text != "" && !strings.HasPrefix(text, "#") {
			fields := protocol.SplitCSVLine(text)
			if len(fields) >= 32 {
				if original, ok := customOriginal3DPlaylist(fields[3]); ok {
					fields[3] = original
					text = strings.Join(fields, ",")
					changed = true
				}
			}
		}
		baseline.WriteString(text)
		if terminated {
			baseline.WriteString("\r\n")
		}
	}
	return changed && size == float64(baseline.Len()) && row["sha256"] == resourceHash(baseline.Bytes())
}

func customOriginal3DPlaylist(name string) (string, bool) {
	index := strings.LastIndex(name, "_cp_")
	if index <= 0 {
		return "", false
	}
	kind, attribute, ok := strings.Cut(name[index+4:], "_")
	if !ok {
		return "", false
	}
	switch kind {
	case "support", "disruption", "defense", "healing":
	default:
		return "", false
	}
	switch attribute {
	case "fire", "ice", "wind", "light", "dark":
		return name[:index], true
	default:
		return "", false
	}
}
