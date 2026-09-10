package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"sort"

	"kairisei.local/server/internal/release"
)

const avatarDeckSlots = 7

type avatarPartsDeckInput struct {
	ArthurType    int8  `json:"arthur_type"`
	Index         int8  `json:"idx"`
	AvatarPartIDs []int `json:"avatar_partsids"`
}

type wireAvatarPartInfo struct {
	PartID int `json:"0"`
}

type wireAvatarPartsDeckInfo struct {
	ArthurType    int8  `json:"0"`
	Index         int8  `json:"1"`
	AvatarPartIDs []int `json:"2"`
}

type wireAvatarPartsShow struct {
	Parts   []wireAvatarPartInfo      `json:"0"`
	Decks   []wireAvatarPartsDeckInfo `json:"1"`
	Avatars []wireAvatarInfo          `json:"2"`
}

type avatarShopSaleInfo struct {
	PayType   int `json:"pay_type"`
	PayTypeID int `json:"pay_typeid"`
	Price     int `json:"price"`
}

type avatarShopSaleList struct {
	Sales []avatarShopSaleInfo `json:"sales"`
}

type avatarShopLineupInfo struct {
	PartID       int                  `json:"partsid"`
	AppearEnd    int                  `json:"appear_end"`
	NewAppearEnd int                  `json:"new_appear_end"`
	SalesList    []avatarShopSaleList `json:"sales_list"`
}

type avatarPurchaseResult struct {
	UpdatedItems []release.Item
	Rewards      []receivedReward
}

func cloneAvatarCompletions(source []release.AvatarSeriesCompletion) []release.AvatarSeriesCompletion {
	result := make([]release.AvatarSeriesCompletion, len(source))
	for index, completion := range source {
		result[index] = completion
		result[index].PartIDs = append([]int(nil), completion.PartIDs...)
		result[index].Rewards = append([]release.AvatarCompletionReward(nil), completion.Rewards...)
	}
	return result
}

func cloneIntMatrix(source [][]int) [][]int {
	result := make([][]int, len(source))
	for index := range source {
		result[index] = append([]int(nil), source[index]...)
	}
	return result
}

func avatarEquipAllowed(definition release.AvatarPartDefinition, arthurType int, slot int, defaults [][]int) bool {
	if arthurType < 1 || arthurType > 4 || slot < 0 || slot >= avatarDeckSlots ||
		definition.ArthurMask&(1<<arthurType) == 0 {
		return false
	}
	if arthurType <= len(defaults) && slot < len(defaults[arthurType-1]) &&
		defaults[arthurType-1][slot] == definition.PartID {
		return true
	}
	return definition.SlotMask&(1<<slot) != 0
}

func (s *store) avatarPartsState() []int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]int, 0, len(s.avatarParts))
	for partID := range s.avatarParts {
		result = append(result, partID)
	}
	sort.Ints(result)
	return result
}

func (s *store) avatarPartsDeckState() []avatarPartsDeckInput {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]avatarPartsDeckInput, len(s.avatars))
	for index, avatar := range s.avatars {
		result[index] = avatarPartsDeckInput{
			ArthurType:    int8(index + 1),
			Index:         0,
			AvatarPartIDs: append([]int(nil), avatar.AvatarPartIDs...),
		}
	}
	return result
}

func (s *store) setAvatarPartsDecks(updates []avatarPartsDeckInput) error {
	if len(updates) == 0 {
		return errors.New("Avatar deck update is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	staged := cloneAvatars(s.avatars)
	seen := make(map[int8]struct{}, len(updates))
	for _, update := range updates {
		if update.ArthurType < 1 || update.ArthurType > 4 || update.Index != 0 ||
			len(update.AvatarPartIDs) != avatarDeckSlots {
			return errors.New("Avatar deck update shape is invalid")
		}
		if _, duplicate := seen[update.ArthurType]; duplicate {
			return fmt.Errorf("Avatar deck update repeats Arthur %d", update.ArthurType)
		}
		seen[update.ArthurType] = struct{}{}
		for slot, partID := range update.AvatarPartIDs {
			if partID == 0 {
				continue
			}
			definition, exists := s.avatarDefinitions[partID]
			if !exists {
				return fmt.Errorf("unknown Avatar part %d", partID)
			}
			if _, owned := s.avatarParts[partID]; !owned {
				return fmt.Errorf("Avatar part %d is not owned", partID)
			}
			if !avatarEquipAllowed(definition, int(update.ArthurType), slot, s.avatarDefaultDecks) {
				return fmt.Errorf("Avatar part %d is incompatible with Arthur %d slot %d", partID, update.ArthurType, slot)
			}
		}
		staged[int(update.ArthurType)-1].AvatarPartIDs = append([]int(nil), update.AvatarPartIDs...)
	}
	s.avatars = staged
	return nil
}

func (s *store) avatarShopState() []avatarShopLineupInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	partIDs := make([]int, 0, len(s.avatarShopParts))
	for partID := range s.avatarShopParts {
		if _, owned := s.avatarParts[partID]; !owned {
			partIDs = append(partIDs, partID)
		}
	}
	sort.Ints(partIDs)
	result := make([]avatarShopLineupInfo, len(partIDs))
	for index, partID := range partIDs {
		result[index] = avatarShopLineupInfo{
			PartID:       partID,
			AppearEnd:    s.avatarShopPolicy.AppearEnd,
			NewAppearEnd: s.avatarShopPolicy.NewAppearEnd,
			SalesList: []avatarShopSaleList{{Sales: []avatarShopSaleInfo{{
				PayType: s.avatarShopPolicy.PayType, PayTypeID: s.avatarShopPolicy.PayTypeID,
				Price: s.avatarShopPolicy.Price,
			}}}},
		}
	}
	return result
}

func (s *store) buyAvatarPart(partID int, salesIndex int) (avatarPurchaseResult, error) {
	if partID <= 0 || salesIndex != 0 {
		return avatarPurchaseResult{}, errors.New("Avatar shop selection is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.avatarDefinitions[partID]; !exists {
		return avatarPurchaseResult{}, errors.New("unknown Avatar shop lineup")
	}
	if _, published := s.avatarShopParts[partID]; !published {
		return avatarPurchaseResult{}, errors.New("Avatar shop lineup is not asset-closed")
	}
	if _, owned := s.avatarParts[partID]; owned {
		return avatarPurchaseResult{}, &businessError{-1, "已拥有这个外观。"}
	}
	if s.avatarShopPolicy.PayType != 1 {
		return avatarPurchaseResult{}, errors.New("unsupported local Avatar shop payment policy")
	}
	if s.avatarShopPolicy.Price <= 0 {
		return avatarPurchaseResult{}, errors.New("invalid Avatar shop price")
	}
	if s.gold < s.avatarShopPolicy.Price {
		return avatarPurchaseResult{}, errInsufficientGold
	}
	prospective := make(map[int]struct{}, len(s.avatarParts)+1)
	for ownedPartID := range s.avatarParts {
		prospective[ownedPartID] = struct{}{}
	}
	prospective[partID] = struct{}{}
	completed := make([]release.AvatarCompletionReward, 0)
	for _, completion := range s.avatarCompletions {
		containsPurchasedPart := false
		isComplete := true
		for _, requiredPartID := range completion.PartIDs {
			if requiredPartID == partID {
				containsPurchasedPart = true
			}
			if _, owned := prospective[requiredPartID]; !owned {
				isComplete = false
			}
		}
		if containsPurchasedPart && isComplete {
			completed = append(completed, completion.Rewards...)
		}
	}
	newSphereCount := 0
	for _, reward := range completed {
		if reward.Type != "SPHR" || reward.Num <= 0 {
			return avatarPurchaseResult{}, errors.New("unsupported Avatar completion reward")
		}
		if _, exists := s.sphereDefinitions[reward.RewardID]; !exists {
			return avatarPurchaseResult{}, fmt.Errorf("Avatar completion references unknown sphere %d", reward.RewardID)
		}
		newSphereCount += reward.Num
	}
	if len(s.spheres)+newSphereCount > s.sphereMax {
		return avatarPurchaseResult{}, errSphereCapacity
	}
	stagedSpheres := append([]release.Sphere(nil), s.spheres...)
	stagedNextSphereUniqueID := s.nextSphereUniqueID
	result := avatarPurchaseResult{UpdatedItems: []release.Item{}}
	for _, completionReward := range completed {
		reward := release.Reward{
			Type: 15, Num: completionReward.Num, RewardTypeID: completionReward.RewardID,
			CardSkillLevels: []int16{},
		}
		received := receivedReward{Reward: reward, UniqueID: []int64{}}
		isNew := true
		for _, ownedSphere := range stagedSpheres {
			if ownedSphere.SphereID == completionReward.RewardID {
				isNew = false
				break
			}
		}
		if isNew {
			received.IsNew = 1
		}
		definition := s.sphereDefinitions[completionReward.RewardID]
		for count := 0; count < completionReward.Num; count++ {
			sphere, err := s.normalizeSphereLocked(release.Sphere{
				UniqueID: stagedNextSphereUniqueID, SphereID: completionReward.RewardID,
				Level: 1, Experience: 0, IsLock: 0,
			}, definition)
			if err != nil {
				return avatarPurchaseResult{}, err
			}
			stagedNextSphereUniqueID++
			stagedSpheres = append(stagedSpheres, sphere)
			received.UniqueID = append(received.UniqueID, sphere.UniqueID)
		}
		result.Rewards = append(result.Rewards, received)
	}
	s.gold -= s.avatarShopPolicy.Price
	s.avatarParts[partID] = struct{}{}
	s.spheres = stagedSpheres
	s.nextSphereUniqueID = stagedNextSphereUniqueID
	return result, nil
}

func (a *API) avatarPartsShow(writer http.ResponseWriter, _ *http.Request) {
	partIDs := a.store.avatarPartsState()
	parts := make([]wireAvatarPartInfo, len(partIDs))
	for index, partID := range partIDs {
		parts[index] = wireAvatarPartInfo{PartID: partID}
	}
	decks := a.store.avatarPartsDeckState()
	wireDecks := make([]wireAvatarPartsDeckInfo, len(decks))
	for index, deck := range decks {
		wireDecks[index] = wireAvatarPartsDeckInfo{
			ArthurType: deck.ArthurType, Index: deck.Index,
			AvatarPartIDs: append([]int(nil), deck.AvatarPartIDs...),
		}
	}
	avatars := a.store.avatarsState()
	wireAvatars := make([]wireAvatarInfo, len(avatars))
	for index, avatar := range avatars {
		wireAvatars[index] = wireAvatarInfo{
			CostumeID: avatar.CostumeID, AvatarPartIDs: append([]int(nil), avatar.AvatarPartIDs...),
		}
	}
	a.writeProtocol(writer, wireAvatarPartsShow{Parts: parts, Decks: wireDecks, Avatars: wireAvatars})
}

func (a *API) avatarPartsDeckSet(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		Decks []avatarPartsDeckInput `json:"avatar_parts_decks"`
	}
	if err := decodeExact(request, []string{"avatar_parts_decks"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if err := a.store.setAvatarPartsDecks(payload.Decks); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{})
}

func (a *API) avatarShopShow(writer http.ResponseWriter, _ *http.Request) {
	a.writeProtocol(writer, map[string]any{
		"lineup": a.store.avatarShopState(),
		"user":   a.userPayload(),
	})
}

func (a *API) avatarShopBuy(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		LineupID  int `json:"avatar_shop_lineupid"`
		SaleIndex int `json:"sales_index"`
	}
	if err := decodeExact(request, []string{"avatar_shop_lineupid", "sales_index"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	result, err := a.store.buyAvatarPart(payload.LineupID, payload.SaleIndex)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	var rewards any
	if len(result.Rewards) != 0 {
		rewards = battleResultRewardsWire(result.Rewards)
	}
	a.writeProtocol(writer, map[string]any{
		"user":           a.userPayload(),
		"items":          a.itemInfosWire(result.UpdatedItems),
		"result_rewards": rewards,
	})
}
