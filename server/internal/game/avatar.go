package game

import (
	"errors"
	"fmt"
	"sort"

	"kairisei.local/server/internal/gamestate"
)

const avatarDeckSlots = 7

type AvatarPartsDeckInput struct {
	ArthurType    int8  `json:"arthur_type"`
	Index         int8  `json:"idx"`
	AvatarPartIDs []int `json:"avatar_partsids"`
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
	UpdatedItems []gamestate.Item
	Rewards      []ReceivedReward
}

func cloneAvatarCompletions(source []gamestate.AvatarSeriesCompletion) []gamestate.AvatarSeriesCompletion {
	result := make([]gamestate.AvatarSeriesCompletion, len(source))
	for index, completion := range source {
		result[index] = completion
		result[index].PartIDs = append([]int(nil), completion.PartIDs...)
		result[index].Rewards = append([]gamestate.AvatarCompletionReward(nil), completion.Rewards...)
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

func avatarEquipAllowed(definition gamestate.AvatarPartDefinition, arthurType int, slot int, defaults [][]int) bool {
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

func (s *Account) AvatarPartsState() []int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]int, 0, len(s.avatarParts))
	for partID := range s.avatarParts {
		result = append(result, partID)
	}
	sort.Ints(result)
	return result
}

func (s *Account) AvatarPartsDeckState() []AvatarPartsDeckInput {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]AvatarPartsDeckInput, len(s.avatars))
	for index, avatar := range s.avatars {
		result[index] = AvatarPartsDeckInput{
			ArthurType:    int8(index + 1),
			Index:         0,
			AvatarPartIDs: append([]int(nil), avatar.AvatarPartIDs...),
		}
	}
	return result
}

func (s *Account) SetAvatarPartsDecks(updates []AvatarPartsDeckInput) error {
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

func (s *Account) AvatarShopState() []avatarShopLineupInfo {
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

func (s *Account) BuyAvatarPart(partID int, salesIndex int) (avatarPurchaseResult, error) {
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
		return avatarPurchaseResult{}, &BusinessError{-1, "已拥有这个外观。"}
	}
	if s.avatarShopPolicy.PayType != 1 {
		return avatarPurchaseResult{}, errors.New("unsupported local Avatar shop payment policy")
	}
	if s.avatarShopPolicy.Price <= 0 {
		return avatarPurchaseResult{}, errors.New("invalid Avatar shop price")
	}
	if s.gold < s.avatarShopPolicy.Price {
		return avatarPurchaseResult{}, ErrInsufficientGold
	}
	prospective := make(map[int]struct{}, len(s.avatarParts)+1)
	for ownedPartID := range s.avatarParts {
		prospective[ownedPartID] = struct{}{}
	}
	prospective[partID] = struct{}{}
	completed := make([]gamestate.AvatarCompletionReward, 0)
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
		return avatarPurchaseResult{}, ErrSphereCapacity
	}
	stagedSpheres := append([]gamestate.Sphere(nil), s.spheres...)
	stagedNextSphereUniqueID := s.nextSphereUniqueID
	result := avatarPurchaseResult{UpdatedItems: []gamestate.Item{}}
	for _, completionReward := range completed {
		reward := gamestate.Reward{
			Type: 15, Num: completionReward.Num, RewardTypeID: completionReward.RewardID,
			CardSkillLevels: []int16{},
		}
		received := ReceivedReward{Reward: reward, UniqueID: []int64{}}
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
			sphere, err := s.normalizeSphereLocked(gamestate.Sphere{
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
