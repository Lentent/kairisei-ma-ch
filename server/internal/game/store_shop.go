package game

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func (s *Account) ItemState() []gamestate.Item {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]gamestate.Item, 0, len(s.items))
	for _, item := range s.items {
		if item.Num > 0 {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(left, right int) bool {
		return items[left].ItemID < items[right].ItemID
	})
	return items
}

func (s *Account) CardCategoryState() ([]gamestate.CardCategoryProfile, []gamestate.CardGroupProfile) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]gamestate.CardCategoryProfile(nil), s.cardCategoryProfiles...),
		cloneCardGroupProfiles(s.cardGroupProfiles)
}

func cloneCardGroupProfiles(source []gamestate.CardGroupProfile) []gamestate.CardGroupProfile {
	result := make([]gamestate.CardGroupProfile, len(source))
	for index, group := range source {
		result[index] = group
		result[index].MemberCardIDs = append([]int(nil), group.MemberCardIDs...)
	}
	return result
}

func (s *Account) ItemExchangeProfileState() map[int]gamestate.ItemExchangeProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[int]gamestate.ItemExchangeProfile, len(s.itemExchangeProfiles))
	for itemID, profile := range s.itemExchangeProfiles {
		profile.Reward = cloneReward(profile.Reward)
		result[itemID] = profile
	}
	return result
}

func (s *Account) ItemLackTipState(index int) (gamestate.ItemLackTipProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	profile, exists := s.itemLackTipProfiles[index]
	if !exists {
		return gamestate.ItemLackTipProfile{}, errors.New("item lack-tip profile is unavailable")
	}
	links := make([]gamestate.ItemLackTipLink, len(profile.TextURLs))
	copy(links, profile.TextURLs)
	profile.TextURLs = links
	return profile, nil
}

func (s *Account) ItemCount(itemID int) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if itemID <= 0 {
		return 0
	}
	return s.items[itemID].Num
}

func (s *Account) ItemShopState() ([]gamestate.ItemShopTab, map[int]int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	owned := make(map[int]int, len(s.items))
	for itemID, item := range s.items {
		owned[itemID] = item.Num
	}
	tabs := cloneItemShopTabs(s.itemShopTabs)
	for ti := range tabs {
		for li := range tabs[ti].Lineup {
			tabs[ti].Lineup[li] = s.configuredItemShopLineup(tabs[ti].Lineup[li])
		}
	}
	return tabs, owned
}

type eventShopLineupState struct {
	Profile     gamestate.EventShopLineupProfile
	StockRemain int
}

type EventShopState struct {
	EventID      int
	PointItemID  int
	PointItemNum int
	Lineups      []eventShopLineupState
}

func (s *Account) EventShopState(eventID int) (EventShopState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.eventShopStateLocked(eventID)
}

func (s *Account) eventShopStateLocked(eventID int) (EventShopState, error) {
	if eventID <= 0 {
		return EventShopState{}, errors.New("invalid event shop ID")
	}
	profile, published := s.eventShopProfiles[eventID]
	if !published {
		return EventShopState{}, errors.New("event shop is unavailable from official local data")
	}
	result := EventShopState{
		EventID:      profile.EventID,
		PointItemID:  profile.PointItemID,
		PointItemNum: s.items[profile.PointItemID].Num,
		Lineups:      make([]eventShopLineupState, len(profile.Lineups)),
	}
	for index, lineup := range profile.Lineups {
		remain := lineup.StockNum
		if remain > 0 {
			remain -= s.eventShopPurchases[lineup.LineupID]
			if remain < 0 {
				remain = 0
			}
		}
		lineup.Reward = cloneReward(lineup.Reward)
		result.Lineups[index] = eventShopLineupState{Profile: lineup, StockRemain: remain}
	}
	return result, nil
}

type tradeShopLineupState struct {
	Profile     gamestate.TradeShopLineupProfile
	StockRemain int
	IsNew       int8
}

type TradeShopState struct {
	Profile gamestate.TradeShopProfile
	Lineups []tradeShopLineupState
	Owns    []gamestate.TradeShopPointProfile
}

func (s *Account) TradeShopState() []TradeShopState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tradeShopStateLocked()
}

func (s *Account) tradeShopStateLocked() []TradeShopState {
	shopIDs := make([]int, 0, len(s.tradeShopProfiles))
	for shopID := range s.tradeShopProfiles {
		shopIDs = append(shopIDs, shopID)
	}
	sort.Ints(shopIDs)
	ownedCards := s.collectedCardIDsLocked()
	result := make([]TradeShopState, 0, len(shopIDs))
	ownedStacks := make(map[int]bool, len(s.stackCards))
	for _, stack := range s.stackCards {
		if stack.Num > 0 {
			ownedStacks[stack.CardID] = true
		}
	}
	for _, shopID := range shopIDs {
		profile := cloneTradeShopProfile(s.tradeShopProfiles[shopID])
		if profile.Disabled || (profile.EndTime > 0 && time.Now().Unix() >= int64(profile.EndTime)) {
			continue
		}
		lineups := profile.Lineups[:0]
		for _, lineup := range profile.Lineups {
			if !lineup.Disabled {
				lineups = append(lineups, lineup)
			}
		}
		profile.Lineups = lineups
		if len(lineups) == 0 {
			continue
		}
		state := TradeShopState{
			Profile: profile,
			Lineups: make([]tradeShopLineupState, len(profile.Lineups)),
		}
		pointKeys := make(map[[2]int]struct{})
		for index, lineup := range profile.Lineups {
			remain := lineup.StockNum
			if len(lineup.Rewards) == 1 && s.ownsCollectionRewardLocked(lineup.Rewards[0]) {
				remain = 0
			}
			if remain > 0 {
				remain -= s.tradeShopPurchases[lineup.LineupID]
				if remain < 0 {
					remain = 0
				}
			}
			isNew := int8(0)
			if len(lineup.Rewards) == 1 && lineup.Rewards[0].Type == 6 {
				if _, owned := ownedCards[lineup.Rewards[0].RewardTypeID]; !owned {
					isNew = 1
				}
			}
			if len(lineup.Rewards) == 1 && lineup.Rewards[0].Type == 13 && !ownedStacks[lineup.Rewards[0].RewardTypeID] {
				isNew = 1
			}
			state.Lineups[index] = tradeShopLineupState{
				Profile:     lineup,
				StockRemain: remain,
				IsNew:       isNew,
			}
			for _, price := range lineup.Prices {
				key := [2]int{price.Type, price.ID}
				if _, exists := pointKeys[key]; exists {
					continue
				}
				pointKeys[key] = struct{}{}
				owned := price
				owned.Num = s.items[price.ID].Num
				state.Owns = append(state.Owns, owned)
			}
		}
		sort.Slice(state.Owns, func(left, right int) bool {
			if state.Owns[left].Type != state.Owns[right].Type {
				return state.Owns[left].Type < state.Owns[right].Type
			}
			return state.Owns[left].ID < state.Owns[right].ID
		})
		result = append(result, state)
	}
	return result
}

type itemExchangeResult struct {
	Item   gamestate.Item
	Reward PresentReceiveResult
}

func (s *Account) ExchangeItem(itemID int, changeSets int) (itemExchangeResult, error) {
	if itemID <= 0 || changeSets <= 0 {
		return itemExchangeResult{}, errors.New("invalid item exchange selection")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	profile, published := s.itemExchangeProfiles[itemID]
	if !published || profile.IsAppearEvent != 0 {
		return itemExchangeResult{}, errors.New("item exchange is unavailable from official local data")
	}
	item, owned := s.items[itemID]
	if !owned || item.Num <= 0 {
		return itemExchangeResult{}, ErrInsufficientMaterials
	}
	if item.LimitTime > 0 && time.Now().Unix() >= int64(item.LimitTime) {
		return itemExchangeResult{}, ErrItemExpired
	}
	maximum := int(^uint(0) >> 1)
	if profile.NeedNum > maximum/changeSets {
		return itemExchangeResult{}, errors.New("item exchange cost overflows")
	}
	cost := profile.NeedNum * changeSets
	if item.Num < cost {
		return itemExchangeResult{}, ErrInsufficientMaterials
	}
	reward := cloneReward(profile.Reward)
	if reward.Num <= 0 || reward.Num > maximum/changeSets {
		return itemExchangeResult{}, errors.New("item exchange reward overflows")
	}
	reward.Num *= changeSets
	if err := s.validateLocalTradeRewardLocked(reward); err != nil {
		return itemExchangeResult{}, err
	}
	if err := s.validateLocalTradeCapacityLocked(itemID, cost, reward); err != nil {
		return itemExchangeResult{}, err
	}

	previousItem := item
	item.Num -= cost
	s.items[itemID] = item
	result := PresentReceiveResult{}
	if err := s.applyLocalTradeRewardLocked(reward, &result); err != nil {
		s.items[itemID] = previousItem
		return itemExchangeResult{}, err
	}
	return itemExchangeResult{Item: s.items[itemID], Reward: result}, nil
}

func (s *Account) validateLocalTradeRewardLocked(reward gamestate.Reward) error {
	if gamestate.IsCollectionReward(reward.Type) {
		return s.validateRewardLocked(reward)
	}
	if reward.Num <= 0 || reward.CardSkillLevels == nil {
		return errors.New("local trade reward is incomplete")
	}
	switch reward.Type {
	case 4, 10, 12:
		if reward.RewardTypeID != 0 {
			return errors.New("local trade scalar reward has an unexpected ID")
		}
	case 6:
		if _, exists := s.cardTemplates[reward.RewardTypeID]; !exists ||
			reward.CardLevel < 1 || reward.CardFame < 1 || reward.CardLove < 0 ||
			len(reward.CardSkillLevels) == 0 {
			return errors.New("local trade references an invalid card reward")
		}
	case 8:
		if _, exists := s.itemDefinitions[reward.RewardTypeID]; !exists {
			return errors.New("local trade references an unknown item reward")
		}
	case 13:
		if _, exists := s.stackCardTemplates[reward.RewardTypeID]; exists {
			return nil
		}
		return errors.New("local trade references an unknown stack-card reward")
	case 15:
		if _, exists := s.sphereDefinitions[reward.RewardTypeID]; !exists {
			return errors.New("local trade references an unknown sphere reward")
		}
	case 19:
		if _, exists := s.buddyDefinitions[reward.RewardTypeID]; !exists {
			return errors.New("local trade references an unknown buddy reward")
		}
	default:
		return fmt.Errorf("unsupported local trade reward type %d", reward.Type)
	}
	return nil
}

func (s *Account) validateLocalTradeCapacityLocked(itemID int, cost int, reward gamestate.Reward) error {
	maximum := int(^uint(0) >> 1)
	switch reward.Type {
	case 4:
		if s.gold > maximum-reward.Num {
			return errors.New("local trade gold reward overflows")
		}
	case 6:
		if reward.Num > s.cardMax-len(s.cards) {
			return ErrCardCapacity
		}
	case 8:
		owned := s.items[reward.RewardTypeID].Num
		if reward.RewardTypeID == itemID {
			owned -= cost
		}
		definition := s.itemDefinitions[reward.RewardTypeID]
		if owned < 0 || reward.Num > definition.MaxOwned-owned {
			return errItemCapacity
		}
	case 10:
		if s.coinFree > maximum-reward.Num {
			return errors.New("local trade crystal reward overflows")
		}
	case 13:
		owned := 0
		for _, stack := range s.stackCards {
			if stack.CardID == reward.RewardTypeID {
				owned = stack.Num
				break
			}
		}
		if owned < 0 || reward.Num > maximum-owned {
			return errors.New("local trade stack-card reward overflows")
		}
	case 15:
		if reward.Num > s.sphereMax-len(s.spheres) {
			return ErrSphereCapacity
		}
		if s.nextSphereUniqueID <= 0 || int64(reward.Num) > int64(^uint64(0)>>1)-s.nextSphereUniqueID {
			return errors.New("sphere unique ID space is exhausted")
		}
	case 19:
		if reward.Num > s.buddyMax-len(s.buddies) {
			return ErrBuddyCapacity
		}
		if s.nextBuddyUniqueID <= 0 || int64(reward.Num) > int64(^uint64(0)>>1)-s.nextBuddyUniqueID {
			return errors.New("buddy unique ID space is exhausted")
		}
	}
	return nil
}

func (s *Account) applyLocalTradeRewardLocked(reward gamestate.Reward, result *PresentReceiveResult) error {
	return s.applyRewardLocked(reward, result)
}

type itemUseResult struct {
	Item gamestate.Item
	AP   apStatus
	BP   BattlePointStatus
}

func (s *Account) UseItem(itemID int) (itemUseResult, error) {
	if itemID <= 0 {
		return itemUseResult{}, errors.New("invalid item ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item, exists := s.items[itemID]
	if !exists || item.Num <= 0 {
		return itemUseResult{}, ErrInsufficientMaterials
	}
	definition, exists := s.itemDefinitions[itemID]
	if !exists {
		return itemUseResult{}, errors.New("unknown item definition")
	}
	now := time.Now()
	s.refreshAPLocked(now)
	s.refreshBattlePointsLocked(now)
	switch definition.Function {
	case "BP_HEAL_FULL", "BP_HEAL_HALF", "BP_HEAL_30":
		if s.bp >= s.bpMax {
			return itemUseResult{}, &BusinessError{-1, "体力已满，无需使用恢复药。"}
		}
		heal := s.bpMax
		if definition.Function == "BP_HEAL_HALF" {
			heal = (s.bpMax + 1) / 2
		}
		if definition.Function == "BP_HEAL_30" {
			heal = 30
		}
		s.bp = min(s.bpMax, s.bp+heal)
		if s.bp == s.bpMax {
			s.bpNextRecovery = time.Time{}
		}
	case "AP_HEAL_FULL", "AP_HEAL_1":
		if s.ap >= s.apMax {
			return itemUseResult{}, &BusinessError{-1, "精力已满，无需使用恢复药。"}
		}
		heal := s.apMax
		if definition.Function == "AP_HEAL_1" {
			heal = 1
		}
		s.ap = min(s.apMax, s.ap+heal)
		if s.ap == s.apMax {
			s.apNextRecovery = time.Time{}
		}
	default:
		return itemUseResult{}, errors.New("item cannot be used directly")
	}
	item.Num--
	s.items[itemID] = item
	return itemUseResult{
		Item: item,
		AP:   s.apStatusLocked(now),
		BP:   s.battlePointStatusLocked(now),
	}, nil
}

func (s *Account) BuyItemShop(lineupID int, buyNum int) ([]gamestate.Item, error) {
	if lineupID <= 0 || buyNum <= 0 {
		return nil, errors.New("invalid item shop purchase")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var selected *gamestate.ItemShopLineup
	for tabIndex := range s.itemShopTabs {
		for lineupIndex := range s.itemShopTabs[tabIndex].Lineup {
			lineup := &s.itemShopTabs[tabIndex].Lineup[lineupIndex]
			if lineup.LineupID == lineupID {
				configured := s.configuredItemShopLineup(*lineup)
				selected = &configured
				break
			}
		}
	}
	if selected == nil || selected.Disabled {
		return nil, &BusinessError{-3600, "该商品已下架，请重新选择。"}
	}
	if buyNum > selected.BuyNumMax {
		return nil, &BusinessError{-1, "超过单次购买数量上限。"}
	}
	if selected.PayType != 1 && selected.PayType != 3 {
		return nil, errors.New("unsupported item shop payment type")
	}
	cost := int64(selected.Price) * int64(buyNum)
	if cost <= 0 || cost > int64(math.MaxInt) {
		return nil, errors.New("item shop purchase cost is invalid")
	}
	switch selected.PayType {
	case 1:
		if cost > int64(s.gold) {
			return nil, ErrInsufficientGold
		}
	case 3:
		if cost > int64(s.coin)+int64(s.coinFree) {
			return nil, ErrInsufficientCrystals
		}
	}
	updates := make([]gamestate.Item, 0, len(selected.Interiors))
	for _, interior := range selected.Interiors {
		if interior.BuyType != 1 {
			return nil, errors.New("unsupported item shop interior")
		}
		definition, exists := s.itemDefinitions[interior.BuyTypeID]
		if !exists {
			return nil, errors.New("unknown item shop item")
		}
		addition := int64(interior.Num) * int64(buyNum)
		current := s.items[interior.BuyTypeID]
		if addition <= 0 || int64(current.Num)+addition > int64(definition.MaxOwned) {
			return nil, errItemCapacity
		}
	}
	switch selected.PayType {
	case 1:
		s.gold -= int(cost)
	case 3:
		freeSpend := min(s.coinFree, int(cost))
		s.coinFree -= freeSpend
		s.coin -= int(cost) - freeSpend
	}
	for _, interior := range selected.Interiors {
		current := s.items[interior.BuyTypeID]
		current.ItemID = interior.BuyTypeID
		current.Num += interior.Num * buyNum
		s.items[current.ItemID] = current
		updates = append(updates, current)
	}
	sort.Slice(updates, func(left, right int) bool {
		return updates[left].ItemID < updates[right].ItemID
	})
	return updates, nil
}

type eventShopBuyResult struct {
	UseItem    gamestate.Item
	LineupName string
	Price      int
	Shop       EventShopState
}

func (s *Account) BuyEventShop(lineupID int) (eventShopBuyResult, error) {
	if lineupID <= 0 {
		return eventShopBuyResult{}, errors.New("invalid event shop lineup")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var eventID int
	var pointItemID int
	var selected *gamestate.EventShopLineupProfile
	for _, profile := range s.eventShopProfiles {
		for index := range profile.Lineups {
			if profile.Lineups[index].LineupID != lineupID {
				continue
			}
			lineup := profile.Lineups[index]
			selected = &lineup
			eventID = profile.EventID
			pointItemID = profile.PointItemID
			break
		}
		if selected != nil {
			break
		}
	}
	if selected == nil {
		return eventShopBuyResult{}, errors.New("event shop lineup is unavailable from official local data")
	}
	if selected.StockNum > 0 && s.eventShopPurchases[lineupID] >= selected.StockNum {
		return eventShopBuyResult{}, &BusinessError{-3702, "该商品兑换次数已用完。"}
	}
	pointItem, owned := s.items[pointItemID]
	if !owned || pointItem.Num < selected.Price {
		return eventShopBuyResult{}, ErrInsufficientMaterials
	}
	if pointItem.LimitTime > 0 && time.Now().Unix() >= int64(pointItem.LimitTime) {
		return eventShopBuyResult{}, ErrItemExpired
	}
	reward := cloneReward(selected.Reward)
	if err := s.validateLocalTradeRewardLocked(reward); err != nil {
		return eventShopBuyResult{}, err
	}
	if err := s.validateLocalTradeCapacityLocked(pointItemID, selected.Price, reward); err != nil {
		return eventShopBuyResult{}, err
	}

	previousPointItem := pointItem
	pointItem.Num -= selected.Price
	s.items[pointItemID] = pointItem
	rewardResult := PresentReceiveResult{}
	if err := s.applyLocalTradeRewardLocked(reward, &rewardResult); err != nil {
		s.items[pointItemID] = previousPointItem
		return eventShopBuyResult{}, err
	}
	s.eventShopPurchases[lineupID]++
	shop, err := s.eventShopStateLocked(eventID)
	if err != nil {
		return eventShopBuyResult{}, err
	}
	return eventShopBuyResult{
		UseItem:    s.items[pointItemID],
		LineupName: selected.Name,
		Price:      selected.Price,
		Shop:       shop,
	}, nil
}

type tradeShopBuyResult struct {
	LineupName string
	Shops      []TradeShopState
	Decks      []DeckInfo
}

func (s *Account) BuyTradeShop(lineupID int, num int, uniqueIDs []int64) (tradeShopBuyResult, error) {
	if lineupID <= 0 || num <= 0 || len(uniqueIDs) != 0 {
		return tradeShopBuyResult{}, errors.New("invalid trade shop purchase")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var selected *gamestate.TradeShopLineupProfile
	for _, shop := range s.tradeShopProfiles {
		if shop.Disabled || (shop.EndTime > 0 && time.Now().Unix() >= int64(shop.EndTime)) {
			continue
		}
		for index := range shop.Lineups {
			if shop.Lineups[index].LineupID != lineupID || shop.Lineups[index].Disabled {
				continue
			}
			lineup := shop.Lineups[index]
			selected = &lineup
			break
		}
		if selected != nil {
			break
		}
	}
	if selected == nil || len(selected.Prices) != 1 || len(selected.Rewards) == 0 {
		return tradeShopBuyResult{}, &BusinessError{-6301, "该兑换商品已下架，请重新打开兑换所。"}
	}
	for _, reward := range selected.Rewards {
		if gamestate.IsCollectionReward(reward.Type) && (num != 1 || s.ownsCollectionRewardLocked(reward)) {
			return tradeShopBuyResult{}, &BusinessError{-6301, "该皮肤、对话、表情或称号已经拥有，或本次兑换数量不是1。"}
		}
	}
	if selected.StockNum > 0 && s.tradeShopPurchases[lineupID]+num > selected.StockNum {
		return tradeShopBuyResult{}, &BusinessError{-6301, "该商品兑换次数已用完。"}
	}
	price := selected.Prices[0]
	if price.Type != 4 || price.ID <= 0 || price.Num <= 0 {
		return tradeShopBuyResult{}, errors.New("unsupported trade shop price")
	}
	cost64 := int64(price.Num) * int64(num)
	if cost64 <= 0 || cost64 > int64(^uint(0)>>1) {
		return tradeShopBuyResult{}, errors.New("trade shop cost overflows")
	}
	pointItem, owned := s.items[price.ID]
	if !owned || pointItem.Num < int(cost64) {
		return tradeShopBuyResult{}, ErrInsufficientMaterials
	}
	if pointItem.LimitTime > 0 && time.Now().Unix() >= int64(pointItem.LimitTime) {
		return tradeShopBuyResult{}, ErrItemExpired
	}
	rewards := make([]gamestate.Reward, 0, len(selected.Rewards)*num)
	for count := 0; count < num; count++ {
		for _, reward := range selected.Rewards {
			cloned := cloneReward(reward)
			if err := s.validateLocalTradeRewardLocked(cloned); err != nil {
				return tradeShopBuyResult{}, err
			}
			rewards = append(rewards, cloned)
		}
	}
	if err := s.validateRewardBatchCapacityLocked(rewards); err != nil {
		return tradeShopBuyResult{}, err
	}

	pointItem.Num -= int(cost64)
	s.items[price.ID] = pointItem
	rewardResult := PresentReceiveResult{}
	for _, reward := range rewards {
		if err := s.applyLocalTradeRewardLocked(reward, &rewardResult); err != nil {
			return tradeShopBuyResult{}, err
		}
	}
	s.tradeShopPurchases[lineupID] += num
	return tradeShopBuyResult{
		LineupName: selected.LineupName,
		Shops:      s.tradeShopStateLocked(),
		Decks:      s.rankedDecksLocked(s.decks),
	}, nil
}

func cloneItemShopTabs(source []gamestate.ItemShopTab) []gamestate.ItemShopTab {
	result := make([]gamestate.ItemShopTab, len(source))
	for tabIndex, tab := range source {
		result[tabIndex].TabType = tab.TabType
		result[tabIndex].Lineup = make([]gamestate.ItemShopLineup, len(tab.Lineup))
		for lineupIndex, lineup := range tab.Lineup {
			result[tabIndex].Lineup[lineupIndex] = lineup
			result[tabIndex].Lineup[lineupIndex].Interiors = append(
				[]gamestate.ItemShopInterior(nil),
				lineup.Interiors...,
			)
		}
	}
	return result
}

func cloneEventShopProfile(source gamestate.EventShopProfile) gamestate.EventShopProfile {
	result := source
	result.Lineups = make([]gamestate.EventShopLineupProfile, len(source.Lineups))
	for index, lineup := range source.Lineups {
		result.Lineups[index] = lineup
		result.Lineups[index].Reward = cloneReward(lineup.Reward)
	}
	return result
}

func cloneTradeShopProfile(source gamestate.TradeShopProfile) gamestate.TradeShopProfile {
	result := source
	result.Lineups = make([]gamestate.TradeShopLineupProfile, len(source.Lineups))
	for lineupIndex, lineup := range source.Lineups {
		result.Lineups[lineupIndex] = lineup
		result.Lineups[lineupIndex].Prices = make([]gamestate.TradeShopPointProfile, len(lineup.Prices))
		for priceIndex, price := range lineup.Prices {
			result.Lineups[lineupIndex].Prices[priceIndex] = price
			result.Lineups[lineupIndex].Prices[priceIndex].PointCardCondition = make(
				[]gamestate.TradeShopPointCardCondition,
				len(price.PointCardCondition),
			)
			copy(result.Lineups[lineupIndex].Prices[priceIndex].PointCardCondition, price.PointCardCondition)
		}
		result.Lineups[lineupIndex].Rewards = cloneRewards(lineup.Rewards)
	}
	return result
}
