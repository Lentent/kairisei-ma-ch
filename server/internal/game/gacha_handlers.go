package game

import (
	"errors"
)

const gachaOddsScale = 100 * 100000

func CardNewFlag(ownedCardIDs map[int]struct{}, cardID int) int8 {
	if _, owned := ownedCardIDs[cardID]; owned {
		return 0
	}
	return 1
}

func scaledGachaOdds(weights []int) ([]int, error) {
	if len(weights) == 0 {
		return nil, errors.New("gacha odds have no weights")
	}
	var total int64
	for _, weight := range weights {
		if weight <= 0 || total > int64(^uint64(0)>>1)-int64(weight) {
			return nil, errors.New("invalid gacha odds weight")
		}
		total += int64(weight)
	}
	if total > int64(^uint64(0)>>1)/int64(gachaOddsScale) {
		return nil, errors.New("gacha odds weight total is too large")
	}
	result := make([]int, len(weights))
	var cumulative int64
	var previousScaled int64
	for index, weight := range weights {
		cumulative += int64(weight)
		scaled := cumulative * int64(gachaOddsScale) / total
		result[index] = int(scaled - previousScaled)
		previousScaled = scaled
	}
	return result, nil
}
