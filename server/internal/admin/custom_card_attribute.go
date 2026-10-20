package admin

import (
	"errors"
	"strings"
)

// The client ATTR enum names pairs in FIRE, ICE, WIND, LIGHT, DARK order.
// Fusion uses a bit mask instead of that enum, so both representations must
// be generated from the same selection.
func normalizeCustomCardAttribute(value string) (string, uint8, error) {
	parts := strings.Split(value, "_")
	if len(parts) < 1 || len(parts) > 2 {
		return "", 0, errors.New("请选择一种或两种不同的属性（火、冰、风、光、暗）")
	}
	var mask uint8
	attributes := []string{"FIRE", "ICE", "WIND", "LIGHT", "DARK"}
	for _, part := range parts {
		var bit uint8
		for i, attribute := range attributes {
			if part == attribute {
				bit = 1 << i
				break
			}
		}
		if bit == 0 || mask&bit != 0 {
			return "", 0, errors.New("请选择一种或两种不同的属性（火、冰、风、光、暗）")
		}
		mask |= bit
	}
	names := []string{}
	for i, attribute := range attributes {
		if mask&(1<<i) != 0 {
			names = append(names, attribute)
		}
	}
	return strings.Join(names, "_"), mask, nil
}
