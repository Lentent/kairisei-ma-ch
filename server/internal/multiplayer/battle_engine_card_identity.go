package multiplayer

// cardIdentityResult mirrors the CN native ResultCmd23 producer and the
// managed BattleParamMgr.setCardBattleData consumer. The row initializes
// CardBattleData for remote room members before any hand projection uses the
// one-based card type.
func cardIdentityResult(memberType int, card BattleCard) BattleResult {
	return BattleResult{Command: resultCardIdentity, Args: []int64{
		int64(memberType), int64(card.CardType), int64(card.CardID), int64(card.Level),
	}}
}
