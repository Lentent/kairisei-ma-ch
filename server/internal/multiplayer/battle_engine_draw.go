package multiplayer

// 5dd72 is called by TurnPhase, before enemy charge/AI checks. Hand state
// must already be authoritative while waiting for TurnPhaseFinish; only the
// visible DEAL notifications wait for UserPhase. Preserve empty slot order.
func (engine *BattleEngine) prepareTurnDraw() {
	engine.turnDrawn = [4][5]bool{}
	for index := range engine.players {
		player := &engine.players[index]
		drawLimit := 1
		if engine.turn == 1 {
			drawLimit = 0
			for _, slot := range player.Hand {
				if slot == 0 {
					drawLimit++
				}
			}
		}
		drawLimit += playerDrawEffectValue(player, battleBuffCodes["DEAL_BONUS"]) - playerDrawEffectValue(player, battleBuffCodes["DEAL_PENALTY"])
		if player.ContinueDraw {
			// 5cecc replaces the effect-derived bonus with 48974's empty
			// slot count. The base draw cannot exceed the five-slot hand.
			drawLimit = len(player.Hand)
			player.ContinueDraw = false
		}
		drawLimit = maxInt(0, drawLimit)
		if player.HP <= 0 {
			continue
		}
		for slot := range player.Hand {
			if drawLimit == 0 {
				break
			}
			if player.Hand[slot] != 0 {
				continue
			}
			player.Hand[slot] = engine.drawCard(player)
			if player.Hand[slot] != 0 {
				engine.turnDrawn[index][slot] = true
				drawLimit--
			}
		}
	}
}
