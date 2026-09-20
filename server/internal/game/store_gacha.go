package game

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func (s *Account) GachaState() []gamestate.GachaProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.VisibleGachasLocked()
}

// homeBannerGachaID selects a crystal multi-draw profile because those rows
// remain visible whether or not the account owns a single-draw ticket. The CN
// client resolves the exact gacha ID to its group before displaying the scene.
func (s *Account) HomeBannerGachaID(now time.Time) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	selected := gamestate.GachaProfile{}
	for _, gacha := range s.gachas {
		if !s.gachaScheduledLocked(gacha.GachaID) || gacha.GachaType != 0 || gacha.PayType != 3 ||
			gacha.CardNum <= 1 || gacha.CardNumMax <= 1 ||
			int64(gacha.EndTime) <= now.Unix() {
			continue
		}
		if selected.GachaID == 0 ||
			gacha.CategoryNum < selected.CategoryNum ||
			(gacha.CategoryNum == selected.CategoryNum && gacha.OrderNum < selected.OrderNum) ||
			(gacha.CategoryNum == selected.CategoryNum && gacha.OrderNum == selected.OrderNum &&
				gacha.GachaID < selected.GachaID) {
			selected = gacha
		}
	}
	return selected.GachaID
}

func (s *Account) GachaStateWithOwnership() ([]gamestate.GachaProfile, map[int]struct{}) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.VisibleGachasLocked(), s.collectedCardIDsLocked()
}

func (s *Account) VisibleGachasLocked() []gamestate.GachaProfile {
	// The original guide selects the first category. The local client's selector
	// keeps a sole gacha category ahead of exchange, so no normal pool is needed
	// to make the reserved tutorial pool the landing page.
	tutorialGachaID := s.onboardingVisibleGachaForStepLocked()
	if tutorialGachaID != 0 {
		for _, gacha := range s.gachas {
			if gacha.GachaID == tutorialGachaID && gacha.PlayCount == 0 {
				return CloneGachaProfiles([]gamestate.GachaProfile{gacha})
			}
		}
		return []gamestate.GachaProfile{}
	}

	preferredSingles := make(map[int]int)
	for _, payType := range []int{4, 2, 3, 6} {
		for _, gacha := range s.gachas {
			if isOnboardingGachaID(gacha.GachaID) || !s.gachaScheduledLocked(gacha.GachaID) {
				continue
			}
			if gacha.CardNumMax != 1 || gacha.PayType != payType {
				continue
			}
			if _, exists := preferredSingles[gacha.GroupID]; exists {
				continue
			}
			if payType == 4 && s.items[gacha.PayTypeID].Num < gacha.Price {
				continue
			}
			preferredSingles[gacha.GroupID] = gacha.GachaID
		}
	}
	// A group whose only single-draw payment is an item must remain visible
	// even when the account does not own that item. The stock client uses that
	// row to open ItemLackTips (notably item 9010 / idx 8). The preference pass
	// above may still replace an unavailable ticket row when the same group has
	// an actually usable crystal or FP single-draw alternative.
	for _, gacha := range s.gachas {
		if isOnboardingGachaID(gacha.GachaID) || !s.gachaScheduledLocked(gacha.GachaID) || gacha.CardNumMax != 1 {
			continue
		}
		if _, exists := preferredSingles[gacha.GroupID]; exists {
			continue
		}
		preferredSingles[gacha.GroupID] = gacha.GachaID
	}

	visible := make([]gamestate.GachaProfile, 0, len(s.gachas))
	for _, gacha := range s.gachas {
		if isOnboardingGachaID(gacha.GachaID) || !s.gachaScheduledLocked(gacha.GachaID) {
			continue
		}
		gacha = s.currentGachaLocked(gacha)
		if gacha.UnownedOnly && len(gacha.CardIDs) == 0 {
			continue
		}
		if gacha.CardNumMax == 1 && preferredSingles[gacha.GroupID] != gacha.GachaID {
			continue
		}
		visible = append(visible, gacha)
	}
	result := CloneGachaProfiles(visible)
	dayKey := gachaLocalDayKey(time.Now())
	for index := range result {
		result[index].DailyFirstAvailable = result[index].DailyFirstFree &&
			s.gachaDailyClaims[result[index].GachaID] != dayKey
	}
	return result
}

func isOnboardingGachaID(gachaID int) bool {
	return gachaID == cnOnboardingGachaID || gachaID == cnOnboardingMultiGachaID
}

// onboardingVisibleGachaForStepLocked follows the stock client's two reserved
// first-gacha families.  The single-draw profile is replaced by the multi-draw
// profile immediately after the fixed single draw; the multi remains visible
// while fusion, deck editing, and the first area are still guiding the player.
// Playback is gated separately by gachaAvailableForPlayLocked.
func (s *Account) onboardingVisibleGachaForStepLocked() int {
	if s.onboarding.ConfigVersion != cnOnboardingConfigVersion {
		return 0
	}
	switch s.onboarding.Step {
	case 1:
		return cnOnboardingGachaID
	case 2, 3, 4, 5:
		return cnOnboardingMultiGachaID
	default:
		return 0
	}
}

func (s *Account) GachaAvailableForPlayLocked(gachaID int) bool {
	if !s.gachaScheduledLocked(gachaID) {
		return false
	}
	if s.onboarding.ConfigVersion == cnOnboardingConfigVersion {
		switch s.onboarding.Step {
		case 0, 2, 3, 4:
			return false
		case 1:
			return gachaID == cnOnboardingGachaID
		case 5:
			return gachaID == cnOnboardingMultiGachaID
		}
	}
	return !isOnboardingGachaID(gachaID)
}

func gachaLocalDayKey(now time.Time) string {
	return now.In(time.FixedZone("CN local service day", 8*60*60)).Format("2006-01-02")
}

type GachaLineupEntry struct {
	Prize gamestate.Reward
	IsNew int8
}

func (s *Account) GachaSelectLineup(gachaID int) ([]GachaLineupEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	profile := findGachaProfile(s.gachas, gachaID)
	if profile == nil {
		return nil, ErrGachaUnavailable
	}
	if profile.UserSelectMax <= 0 {
		return nil, errors.New("gacha has no user-select lineup")
	}
	owned := s.collectedCardIDsLocked()
	result := make([]GachaLineupEntry, len(profile.CardIDs))
	for index, cardID := range profile.CardIDs {
		result[index] = GachaLineupEntry{
			Prize: GachaCardReward(cardID),
			IsNew: CardNewFlag(owned, cardID),
		}
	}
	return result, nil
}

func (s *Account) GachaSelectedLineup(gachaID int) ([]GachaLineupEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	profile := findGachaProfile(s.gachas, gachaID)
	if profile == nil {
		return nil, ErrGachaUnavailable
	}
	if profile.UserSelectMax <= 0 {
		return nil, errors.New("gacha has no user-select lineup")
	}
	selected := s.gachaSelections[gachaID]
	owned := s.collectedCardIDsLocked()
	result := make([]GachaLineupEntry, len(selected))
	for index, reward := range selected {
		result[index] = GachaLineupEntry{
			Prize: cloneReward(reward),
			IsNew: CardNewFlag(owned, reward.RewardTypeID),
		}
	}
	return result, nil
}

func (s *Account) collectedCardIDsLocked() map[int]struct{} {
	owned := make(map[int]struct{}, len(s.cardCollectionIDs)+len(s.cards)+len(s.containerCards))
	for id := range s.cardCollectionIDs {
		owned[id] = struct{}{}
	}
	for _, inventory := range [][]CardInfo{s.cards, s.containerCards} {
		for _, card := range inventory {
			owned[card.CardID] = struct{}{}
		}
	}
	return owned
}

func (s *Account) hasCollectedCardLocked(id int) bool {
	if _, ok := s.cardCollectionIDs[id]; ok {
		return true
	}
	for _, inventory := range [][]CardInfo{s.cards, s.containerCards} {
		for _, card := range inventory {
			if card.CardID == id {
				return true
			}
		}
	}
	return false
}

func findGachaProfile(gachas []gamestate.GachaProfile, gachaID int) *gamestate.GachaProfile {
	for index := range gachas {
		if gachas[index].GachaID == gachaID {
			return &gachas[index]
		}
	}
	return nil
}

func GachaCardReward(cardID int) gamestate.Reward {
	return gamestate.Reward{
		Type: 6, Num: 1, RewardTypeID: cardID,
		CardLevel: 1, CardFame: 1, CardSkillLevels: []int16{1},
	}
}

func validateGachaSelection(gacha gamestate.GachaProfile, rewards []gamestate.Reward) error {
	if gacha.UserSelectMax < 0 || gacha.UserSelectMax > len(gacha.CardIDs) {
		return errors.New("gacha user-select configuration is invalid")
	}
	if len(rewards) != gacha.UserSelectMax {
		return errors.New("gacha user-select lineup has an invalid length")
	}
	allowed := make(map[int]struct{}, len(gacha.CardIDs))
	for _, cardID := range gacha.CardIDs {
		allowed[cardID] = struct{}{}
	}
	seen := make(map[int]struct{}, len(rewards))
	for _, reward := range rewards {
		if reward.Type != 6 || reward.Num != 1 || reward.CardLevel != 1 ||
			reward.CardFame != 1 || reward.CardLove != 0 ||
			len(reward.CardSkillLevels) != 1 || reward.CardSkillLevels[0] != 1 {
			return errors.New("gacha user-select lineup contains an invalid reward")
		}
		if _, exists := allowed[reward.RewardTypeID]; !exists {
			return errors.New("gacha user-select lineup contains an unavailable card")
		}
		if _, duplicate := seen[reward.RewardTypeID]; duplicate {
			return errors.New("gacha user-select lineup repeats a card")
		}
		seen[reward.RewardTypeID] = struct{}{}
	}
	return nil
}

func cloneRewards(source []gamestate.Reward) []gamestate.Reward {
	result := make([]gamestate.Reward, len(source))
	for index, reward := range source {
		result[index] = cloneReward(reward)
	}
	return result
}

func cloneTowerQuestProfile(source gamestate.TowerQuestProfile) gamestate.TowerQuestProfile {
	result := source
	result.Ranks = append([]gamestate.TowerQuestRankProfile(nil), source.Ranks...)
	result.Floors = make([]gamestate.TowerQuestFloorProfile, len(source.Floors))
	for index, floor := range source.Floors {
		floor.Boss = append(json.RawMessage(nil), floor.Boss...)
		floor.ClearRewards = cloneRewards(floor.ClearRewards)
		result.Floors[index] = floor
	}
	return result
}

func cloneTowerQuestProgress(source gamestate.TowerQuestProgress) gamestate.TowerQuestProgress {
	result := source
	result.ClearedFloors = append([]int(nil), source.ClearedFloors...)
	return result
}

type gachaPlayResult struct {
	Gachas       []gamestate.GachaProfile
	Item         gamestate.Item
	Reward       PresentReceiveResult
	Expectancy   int
	Gifts        []gamestate.Reward
	PresentGifts []gamestate.Reward
}

// CN GACHA_EXPECTANCY is zero-based (RARE=2, SUPERRARE=3, ULTRARARE=4),
// whereas the official card master projects rarity_rank as 1..8. The whole
// draw uses its highest actual result, not the pool's maximum or an evolved card.
func gachaResultExpectancy(rewards []gamestate.Reward, definitions map[int]gamestate.Card) int {
	maxRank := 1
	for _, reward := range rewards {
		if reward.Type == 6 && reward.Num > 0 {
			maxRank = max(maxRank, definitions[reward.RewardTypeID].RarityRank)
		}
	}
	return min(maxRank, 8) - 1
}

type itemGachaPlayResult struct {
	Item   gamestate.Item
	Reward PresentReceiveResult
}

func (s *Account) PlayItemGacha(itemID int, playCount int) (itemGachaPlayResult, error) {
	if itemID <= 0 || playCount < 1 || playCount > 10 {
		return itemGachaPlayResult{}, errors.New("invalid item gacha selection")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	item, owned := s.items[itemID]
	if !owned || item.Num < playCount {
		return itemGachaPlayResult{}, ErrInsufficientMaterials
	}
	if item.LimitTime > 0 && time.Now().Unix() >= int64(item.LimitTime) {
		return itemGachaPlayResult{}, ErrItemExpired
	}
	definition, exists := s.itemDefinitions[itemID]
	if !exists || definition.ItemType != "GACHA" || definition.Function != "GACHA_EXEC" {
		return itemGachaPlayResult{}, errors.New("item cannot execute a gacha")
	}
	profile, exists := s.itemGachaProfiles[itemID]
	if !exists || profile.FunctionValue != definition.FunctionValue {
		return itemGachaPlayResult{}, errors.New("item gacha pool unavailable from official local data")
	}
	rewards := make([]gamestate.Reward, 0, len(profile.Rewards)*playCount)
	for play := 0; play < playCount; play++ {
		rewards = append(rewards, profile.Rewards...)
		if len(profile.RewardPool) > 0 {
			reward, err := drawWeightedReward(profile.RewardPool)
			if err != nil {
				return itemGachaPlayResult{}, err
			}
			rewards = append(rewards, reward)
		}
	}
	if err := s.validateSettlementRewardsLocked(rewards); err != nil {
		return itemGachaPlayResult{}, err
	}

	item.Num -= playCount
	s.items[itemID] = item
	result := PresentReceiveResult{}
	for _, reward := range rewards {
		if err := s.applyRewardOrPresentLocked(reward, &result, "扭蛋奖励"); err != nil {
			return itemGachaPlayResult{}, err
		}
	}
	return itemGachaPlayResult{Item: s.items[itemID], Reward: result}, nil
}

func (s *Account) PlayGacha(
	gachaID int,
	payType int,
	selectedRewards []gamestate.Reward,
) (gachaPlayResult, error) {
	if gachaID <= 0 {
		return gachaPlayResult{}, errors.New("invalid gacha ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index := -1
	for current := range s.gachas {
		if s.gachas[current].GachaID == gachaID {
			index = current
			break
		}
	}
	if index < 0 || payType != s.gachas[index].PayType {
		return gachaPlayResult{}, ErrGachaUnavailable
	}
	profile := s.currentGachaLocked(s.gachas[index])
	gacha := &profile
	if gacha.PlayCount == math.MaxInt || (gacha.UnownedOnly && len(gacha.CardIDs) == 0) {
		return gachaPlayResult{}, ErrGachaUnavailable
	}
	if !s.GachaAvailableForPlayLocked(gachaID) ||
		(isOnboardingGachaID(gachaID) && gacha.PlayCount != 0) {
		return gachaPlayResult{}, ErrGachaUnavailable
	}
	if err := validateGachaSelection(*gacha, selectedRewards); err != nil {
		return gachaPlayResult{}, err
	}
	dayKey := gachaLocalDayKey(time.Now())
	dailyFree := gacha.DailyFirstFree && s.gachaDailyClaims[gachaID] != dayKey
	drawCount := gacha.CardNum
	paymentCost := gacha.Price
	if dailyFree {
		paymentCost = 0
	} else if gacha.PayType == 2 && gacha.CardNumMax > gacha.CardNum {
		drawCount = min(gacha.CardNumMax, s.friendPoint/gacha.Price)
		if drawCount < gacha.CardNum {
			return gachaPlayResult{}, errInsufficientFriendPoints
		}
		if gacha.Price > math.MaxInt/drawCount {
			return gachaPlayResult{}, errors.New("friend-point gacha cost overflows")
		}
		paymentCost = gacha.Price * drawCount
	}
	paidItem := gamestate.Item{}
	switch gacha.PayType {
	case 2:
		if s.friendPoint < paymentCost {
			return gachaPlayResult{}, errInsufficientFriendPoints
		}
	case 3:
		if int64(s.coin)+int64(s.coinFree) < int64(paymentCost) {
			return gachaPlayResult{}, ErrInsufficientCrystals
		}
	case 4:
		paidItem = s.items[gacha.PayTypeID]
		if paidItem.Num < paymentCost || (paidItem.LimitTime > 0 && time.Now().Unix() >= int64(paidItem.LimitTime)) {
			return gachaPlayResult{}, ErrInsufficientMaterials
		}
	case 6:
		if s.coin < paymentCost {
			return gachaPlayResult{}, errInsufficientPaidCrystals
		}
	default:
		return gachaPlayResult{}, errors.New("unsupported gacha payment type")
	}

	drawCardIDs := gacha.CardIDs
	drawCardWeights := gacha.CardWeights
	if len(selectedRewards) > 0 {
		drawCardIDs = make([]int, len(selectedRewards))
		drawCardWeights = make([]int, len(selectedRewards))
		weightsByCardID := make(map[int]int, len(gacha.CardIDs))
		for cardIndex, cardID := range gacha.CardIDs {
			weightsByCardID[cardID] = gacha.CardWeights[cardIndex]
		}
		for selectedIndex, reward := range selectedRewards {
			drawCardIDs[selectedIndex] = reward.RewardTypeID
			drawCardWeights[selectedIndex] = weightsByCardID[reward.RewardTypeID]
		}
	}
	guaranteedCardIDs := []int(nil)
	guaranteedCardWeights := []int(nil)
	remainderCardIDs := drawCardIDs
	remainderCardWeights := drawCardWeights
	if gacha.GuaranteedCount > 0 {
		if len(selectedRewards) != 0 || gacha.GuaranteedCount >= drawCount ||
			gacha.GuaranteedRarityRank <= 0 || gacha.RemainderRarityRank <= 0 {
			return gachaPlayResult{}, errors.New("gacha guaranteed-result policy is invalid")
		}
		var err error
		guaranteedCardIDs, guaranteedCardWeights, err = s.gachaPoolForRarityLocked(
			drawCardIDs, drawCardWeights, gacha.GuaranteedRarityRank,
		)
		if err != nil {
			return gachaPlayResult{}, err
		}
		remainderCardIDs, remainderCardWeights, err = s.gachaPoolForRarityLocked(
			drawCardIDs, drawCardWeights, gacha.RemainderRarityRank,
		)
		if err != nil {
			return gachaPlayResult{}, err
		}
	}
	rewards := make([]gamestate.Reward, drawCount)
	for draw := range rewards {
		if len(gacha.RewardPool) > 0 {
			reward, err := drawWeightedReward(gacha.RewardPool)
			if err != nil {
				return gachaPlayResult{}, err
			}
			rewards[draw] = reward
			continue
		}
		poolCardIDs := remainderCardIDs
		poolCardWeights := remainderCardWeights
		if draw < gacha.GuaranteedCount {
			poolCardIDs = guaranteedCardIDs
			poolCardWeights = guaranteedCardWeights
		}
		cardID, err := weightedGachaCard(poolCardIDs, poolCardWeights)
		if err != nil {
			return gachaPlayResult{}, err
		}
		rewards[draw] = GachaCardReward(cardID)
		if err := s.validateRewardLocked(rewards[draw]); err != nil {
			return gachaPlayResult{}, err
		}
	}
	gifts := gacha.CurrentGifts()
	allRewards := append(cloneRewards(rewards), gifts...)
	if err := s.validateSettlementRewardsLocked(allRewards); err != nil {
		return gachaPlayResult{}, err
	}

	switch gacha.PayType {
	case 2:
		s.friendPoint -= paymentCost
	case 3:
		freeSpend := min(s.coinFree, paymentCost)
		s.coinFree -= freeSpend
		s.coin -= paymentCost - freeSpend
	case 4:
		paidItem.Num -= paymentCost
		s.items[paidItem.ItemID] = paidItem
	case 6:
		s.coin -= paymentCost
	}
	s.gachas[index].PlayCount++
	if dailyFree {
		s.gachaDailyClaims[gachaID] = dayKey
	}
	if len(selectedRewards) > 0 {
		s.gachaSelections[gachaID] = cloneRewards(selectedRewards)
	}
	result := PresentReceiveResult{}
	for _, reward := range allRewards {
		if err := s.applyRewardOrPresentLocked(reward, &result, "扭蛋奖励"); err != nil {
			return gachaPlayResult{}, err
		}
	}
	directGifts, presentGifts := []gamestate.Reward{}, []gamestate.Reward{}
	for _, gift := range result.Rewards[len(rewards):] {
		if gift.InPresentBox {
			presentGifts = append(presentGifts, gift.Reward)
		} else {
			directGifts = append(directGifts, gift.Reward)
		}
	}
	// Gift inventory updates share the transaction but have their own original
	// client display; do not also animate them as ordinary draw results.
	result.Rewards = result.Rewards[:len(rewards)]
	if paidItem.ItemID != 0 {
		paidItem = s.items[paidItem.ItemID]
	}
	if err := s.advanceOnboardingLocked(onboardingEvent{kind: "gacha", gachaID: gachaID}); err != nil {
		return gachaPlayResult{}, err
	}
	return gachaPlayResult{
		Gachas:       s.VisibleGachasLocked(),
		Item:         paidItem,
		Reward:       result,
		Expectancy:   s.gachaMixedResultExpectancy(rewards),
		Gifts:        directGifts,
		PresentGifts: presentGifts,
	}, nil
}

func (s *Account) gachaPoolForRarityLocked(
	cardIDs []int,
	weights []int,
	rarityRank int,
) ([]int, []int, error) {
	if len(cardIDs) == 0 || len(cardIDs) != len(weights) || rarityRank <= 0 {
		return nil, nil, errors.New("invalid gacha rarity pool request")
	}
	filteredIDs := make([]int, 0, len(cardIDs))
	filteredWeights := make([]int, 0, len(cardIDs))
	for index, cardID := range cardIDs {
		definition, exists := s.cardDefinitions[cardID]
		if !exists {
			return nil, nil, fmt.Errorf("gacha card definition %d is unavailable", cardID)
		}
		if definition.RarityRank != rarityRank {
			continue
		}
		filteredIDs = append(filteredIDs, cardID)
		filteredWeights = append(filteredWeights, weights[index])
	}
	if len(filteredIDs) == 0 {
		return nil, nil, fmt.Errorf("gacha rarity %d pool is empty", rarityRank)
	}
	return filteredIDs, filteredWeights, nil
}

func weightedGachaCard(cardIDs []int, weights []int) (int, error) {
	if len(cardIDs) == 0 || len(cardIDs) != len(weights) {
		return 0, errors.New("invalid gacha card weights")
	}
	total := 0
	for _, weight := range weights {
		if weight <= 0 || total > int(^uint(0)>>1)-weight {
			return 0, errors.New("invalid gacha card weight")
		}
		total += weight
	}
	value, err := rand.Int(rand.Reader, big.NewInt(int64(total)))
	if err != nil {
		return 0, fmt.Errorf("select gacha card: %w", err)
	}
	selected := int(value.Int64())
	for index, weight := range weights {
		if selected < weight {
			return cardIDs[index], nil
		}
		selected -= weight
	}
	return 0, errors.New("select gacha card outside configured weights")
}

func CloneGachaProfiles(source []gamestate.GachaProfile) []gamestate.GachaProfile {
	return gamestate.CloneGachas(source)
}
