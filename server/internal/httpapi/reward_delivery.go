package httpapi

import (
	"kairisei.local/server/internal/game"
)

func battleAwardsInPresentBox(awards []game.TeamBattleFameAward) int {
	for _, award := range awards {
		if award.Result.InPresentBox {
			return 1
		}
	}
	return 0
}
