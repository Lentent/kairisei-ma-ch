package httpapi

import (
	"strings"
)

// Called only after the native report envelope was validated. Preserve turn
// metadata for legacy score policies; current solo cups require a clear only.
func nativeScoreTurns(commands []string) int {
	turns := 0
	for _, wave := range commands {
		for _, line := range strings.Split(wave, "\n") {
			fields := strings.Split(strings.TrimSpace(line), ",")
			if len(fields) == 21 && strings.TrimSpace(fields[1]) == "10" {
				turns++
			}
		}
	}
	return max(1, turns)
}

func scoreInfoWire(info []any) []any {
	if info == nil {
		return []any{}
	}
	return info
}
