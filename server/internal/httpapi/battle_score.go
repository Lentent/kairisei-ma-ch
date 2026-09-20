package httpapi

import (
	"strings"
)

// Called only after the native report envelope was validated. This local policy
// credits a fixed clear score and a turn multiplier; it does not claim to replay damage.
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
