package httpapi

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"

	"kairisei.local/server/internal/release"
)

const (
	sphereFusionInputSphere     = 0
	sphereFusionInputCardStack  = 1
	sphereFusionMaterialType    = 7
	sphereEvolutionMaterialType = 8
)

type sphereFusionInput struct {
	InputType int8  `json:"input_type"`
	ID        int64 `json:"id"`
	Num       int   `json:"num"`
}

func cloneSphereExperienceTables(source map[int][]int) map[int][]int {
	result := make(map[int][]int, len(source))
	for tableID, values := range source {
		result[tableID] = append([]int(nil), values...)
	}
	return result
}

func cloneSphereEvolutionPrices(source map[string][]int) map[string][]int {
	result := make(map[string][]int, len(source))
	for rarity, values := range source {
		result[rarity] = append([]int(nil), values...)
	}
	return result
}

func (s *store) sphereState() []release.Sphere {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := append([]release.Sphere(nil), s.spheres...)
	sort.Slice(result, func(left, right int) bool {
		return result[left].UniqueID < result[right].UniqueID
	})
	return result
}

func (s *store) sphereIndexByUniqueIDLocked(uniqueID int64) int {
	for index, sphere := range s.spheres {
		if sphere.UniqueID == uniqueID {
			return index
		}
	}
	return -1
}

func (s *store) clearSphereIDsFromDecksLocked(remove map[int64]struct{}) {
	if len(remove) == 0 {
		return
	}
	for deckIndex := range s.decks {
		for slot, uniqueID := range s.decks[deckIndex].SphereUniqueIDs {
			if _, exists := remove[uniqueID]; exists {
				s.decks[deckIndex].SphereUniqueIDs[slot] = 0
			}
		}
	}
}

func (s *store) normalizeSphereLocked(sphere release.Sphere, definition release.SphereDefinition) (release.Sphere, error) {
	values, exists := s.sphereExperience[definition.ExperienceTableID]
	if !exists && definition.MaxLevel > 1 {
		return release.Sphere{}, errors.New("sphere experience table is unavailable")
	}
	progression, err := release.NormalizeSphereExperience(definition.MaxLevel, sphere.Experience, values)
	if err != nil || progression.Level != sphere.Level {
		return release.Sphere{}, errors.New("sphere level or experience is invalid")
	}
	sphere.Experience = progression.Experience
	sphere.NowLevelExperience = progression.NowLevelExperience
	sphere.NextLevelExperience = progression.NextLevelExperience
	addExperience, err := release.SphereMaterialExperience(
		definition.MaterialAddExperience,
		sphere.Level,
		s.sphereProgression.MaterialLevelBonusPermillePerLevel,
	)
	if err != nil {
		return release.Sphere{}, err
	}
	sphere.AddExperience = addExperience
	sphere.BaseAddPrice = definition.FusionBaseAddPrice
	return sphere, nil
}

func (s *store) addSphereExperienceLocked(sphere release.Sphere, definition release.SphereDefinition, addition int) (release.Sphere, error) {
	if addition <= 0 || sphere.Level >= definition.MaxLevel {
		return release.Sphere{}, errors.New("sphere cannot gain experience")
	}
	if sphere.Experience > math.MaxInt-addition {
		return release.Sphere{}, errors.New("sphere experience overflow")
	}
	progression, err := release.NormalizeSphereExperience(
		definition.MaxLevel,
		sphere.Experience+addition,
		s.sphereExperience[definition.ExperienceTableID],
	)
	if err != nil {
		return release.Sphere{}, err
	}
	sphere.Level = progression.Level
	sphere.Experience = progression.Experience
	sphere.NowLevelExperience = progression.NowLevelExperience
	sphere.NextLevelExperience = progression.NextLevelExperience
	addExperience, err := release.SphereMaterialExperience(
		definition.MaterialAddExperience,
		sphere.Level,
		s.sphereProgression.MaterialLevelBonusPermillePerLevel,
	)
	if err != nil {
		return release.Sphere{}, err
	}
	sphere.AddExperience = addExperience
	sphere.BaseAddPrice = definition.FusionBaseAddPrice
	return sphere, nil
}

func (s *store) fuseSphere(baseUniqueID int64, inputs []sphereFusionInput) (release.Sphere, int, int, int, []deckInfo, error) {
	if baseUniqueID <= 0 || len(inputs) == 0 {
		return release.Sphere{}, 0, 0, 0, nil, errors.New("sphere fusion selection is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	baseIndex := s.sphereIndexByUniqueIDLocked(baseUniqueID)
	if baseIndex < 0 {
		return release.Sphere{}, 0, 0, 0, nil, errors.New("unknown sphere fusion base")
	}
	base := s.spheres[baseIndex]
	definition := s.sphereDefinitions[base.SphereID]
	if base.Level >= definition.MaxLevel {
		return release.Sphere{}, 0, 0, 0, nil, &businessError{-1, "圣剑已达到最高等级。"}
	}
	remove := make(map[int64]struct{}, len(inputs))
	stackUses := make([]release.CardStackUse, 0, len(inputs))
	seenStackCards := make(map[int]struct{}, len(inputs))
	materialCount := 0
	addExperience := 0
	for _, input := range inputs {
		if input.ID <= 0 || input.Num <= 0 || materialCount > math.MaxInt-input.Num {
			return release.Sphere{}, 0, 0, 0, nil, errors.New("invalid sphere fusion input")
		}
		materialCount += input.Num
		switch input.InputType {
		case sphereFusionInputSphere:
			if input.Num != 1 || input.ID == baseUniqueID {
				return release.Sphere{}, 0, 0, 0, nil, errors.New("invalid sphere fusion material")
			}
			index := s.sphereIndexByUniqueIDLocked(input.ID)
			if index < 0 {
				return release.Sphere{}, 0, 0, 0, nil, errSphereUnavailable
			}
			if s.spheres[index].IsLock != 0 {
				return release.Sphere{}, 0, 0, 0, nil, errSphereLocked
			}
			if _, duplicate := remove[input.ID]; duplicate {
				return release.Sphere{}, 0, 0, 0, nil, errors.New("duplicate sphere fusion material")
			}
			remove[input.ID] = struct{}{}
			if addExperience > math.MaxInt-s.spheres[index].AddExperience {
				return release.Sphere{}, 0, 0, 0, nil, errors.New("sphere fusion experience overflow")
			}
			addExperience += s.spheres[index].AddExperience
		case sphereFusionInputCardStack:
			if input.ID > math.MaxInt {
				return release.Sphere{}, 0, 0, 0, nil, errors.New("sphere fusion stack-card ID is invalid")
			}
			cardID := int(input.ID)
			if _, duplicate := seenStackCards[cardID]; duplicate {
				return release.Sphere{}, 0, 0, 0, nil, errors.New("duplicate sphere fusion stack-card input")
			}
			seenStackCards[cardID] = struct{}{}
			owned := -1
			for index, stack := range s.stackCards {
				if stack.CardID == cardID {
					owned = index
					break
				}
			}
			if owned < 0 || s.stackCards[owned].Num < input.Num {
				return release.Sphere{}, 0, 0, 0, nil, errInsufficientMaterials
			}
			if s.stackCards[owned].MaterialType != sphereFusionMaterialType || s.stackCards[owned].AddExperience <= 0 {
				return release.Sphere{}, 0, 0, 0, nil, errors.New("sphere fusion stack-card material is unavailable")
			}
			addition := int64(s.stackCards[owned].AddExperience) * int64(input.Num)
			if addition > math.MaxInt || int64(addExperience)+addition > math.MaxInt {
				return release.Sphere{}, 0, 0, 0, nil, errors.New("sphere fusion experience overflow")
			}
			addExperience += int(addition)
			stackUses = append(stackUses, release.CardStackUse{CardID: cardID, Num: input.Num})
		default:
			return release.Sphere{}, 0, 0, 0, nil, errors.New("unknown sphere fusion input type")
		}
	}
	cost := int64(base.BaseAddPrice) * int64(materialCount)
	if cost <= 0 || cost > math.MaxInt {
		return release.Sphere{}, 0, 0, 0, nil, errors.New("invalid sphere fusion cost")
	}
	if cost > int64(s.gold) {
		return release.Sphere{}, 0, 0, 0, nil, errInsufficientGold
	}
	success, err := rollCardFusionSuccess(s.cardProgression)
	if err != nil {
		return release.Sphere{}, 0, 0, 0, nil, err
	}
	boostedExperience, err := boostedFusionExperience(addExperience, success)
	if err != nil {
		return release.Sphere{}, 0, 0, 0, nil, err
	}
	updated, err := s.addSphereExperienceLocked(base, definition, boostedExperience)
	if err != nil {
		return release.Sphere{}, 0, 0, 0, nil, err
	}
	s.spheres[baseIndex] = updated
	kept := make([]release.Sphere, 0, len(s.spheres)-len(remove))
	for _, sphere := range s.spheres {
		if _, consumed := remove[sphere.UniqueID]; !consumed {
			kept = append(kept, sphere)
		}
	}
	s.spheres = kept
	consumeStackUses(s.stackCards, stackUses)
	s.clearSphereIDsFromDecksLocked(remove)
	s.gold -= int(cost)
	return updated, success.SuccessType, s.gold, len(s.spheres), s.rankedDecksLocked(s.decks), nil
}

func (s *store) evolveSphere(baseUniqueID int64, materialUniqueID int64, materialCardID int) (release.Sphere, int, int, []deckInfo, error) {
	if baseUniqueID <= 0 || (materialUniqueID == 0) == (materialCardID == 0) {
		return release.Sphere{}, 0, 0, nil, errors.New("sphere evolution requires exactly one material")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	baseIndex := s.sphereIndexByUniqueIDLocked(baseUniqueID)
	if baseIndex < 0 {
		return release.Sphere{}, 0, 0, nil, errors.New("unknown sphere evolution base")
	}
	base := s.spheres[baseIndex]
	definition := s.sphereDefinitions[base.SphereID]
	target, exists := s.sphereDefinitions[definition.EvolutionID]
	if !exists || definition.EvolutionID == 0 ||
		target.SameSphereID != definition.SameSphereID ||
		target.EvolutionCount != definition.EvolutionCount+1 ||
		target.ExperienceTableID != definition.ExperienceTableID ||
		target.MaxLevel < definition.MaxLevel {
		return release.Sphere{}, 0, 0, nil, errors.New("sphere is not ready for evolution")
	}
	prices := s.sphereEvoPrices[target.Rarity]
	priceIndex := target.EvolutionCount - 1
	if priceIndex < 0 || priceIndex >= len(prices) {
		return release.Sphere{}, 0, 0, nil, errors.New("invalid sphere evolution price index")
	}
	if prices[priceIndex] > s.gold {
		return release.Sphere{}, 0, 0, nil, errInsufficientGold
	}
	remove := make(map[int64]struct{}, 1)
	stackIndex := -1
	if materialUniqueID != 0 {
		if materialUniqueID == baseUniqueID {
			return release.Sphere{}, 0, 0, nil, errors.New("sphere evolution material equals the base")
		}
		materialIndex := s.sphereIndexByUniqueIDLocked(materialUniqueID)
		if materialIndex < 0 {
			return release.Sphere{}, 0, 0, nil, errSphereUnavailable
		}
		if s.spheres[materialIndex].IsLock != 0 {
			return release.Sphere{}, 0, 0, nil, errSphereLocked
		}
		materialDefinition := s.sphereDefinitions[s.spheres[materialIndex].SphereID]
		if materialDefinition.SameSphereID != definition.SameSphereID {
			return release.Sphere{}, 0, 0, nil, errors.New("sphere evolution material belongs to another family")
		}
		remove[materialUniqueID] = struct{}{}
	} else {
		for index, stack := range s.stackCards {
			if stack.CardID == materialCardID {
				stackIndex = index
				break
			}
		}
		if stackIndex < 0 || s.stackCards[stackIndex].Num < 1 {
			return release.Sphere{}, 0, 0, nil, errInsufficientMaterials
		}
		if s.stackCards[stackIndex].MaterialType != sphereEvolutionMaterialType {
			return release.Sphere{}, 0, 0, nil, errors.New("sphere evolution stack-card material is unavailable")
		}
	}
	result := base
	result.SphereID = target.SphereID
	result, err := s.normalizeSphereLocked(result, target)
	if err != nil {
		return release.Sphere{}, 0, 0, nil, err
	}
	s.spheres[baseIndex] = result
	if stackIndex >= 0 {
		s.stackCards[stackIndex].Num--
	}
	if len(remove) != 0 {
		kept := make([]release.Sphere, 0, len(s.spheres)-1)
		for _, sphere := range s.spheres {
			if _, consumed := remove[sphere.UniqueID]; !consumed {
				kept = append(kept, sphere)
			}
		}
		s.spheres = kept
		s.clearSphereIDsFromDecksLocked(remove)
	}
	s.gold -= prices[priceIndex]
	return result, s.gold, len(s.spheres), s.rankedDecksLocked(s.decks), nil
}

func (s *store) sellSpheres(uniqueIDs []int64) (int, int, int, []deckInfo, error) {
	if len(uniqueIDs) == 0 {
		return 0, 0, 0, nil, errors.New("sphere sell selection is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	remove := make(map[int64]struct{}, len(uniqueIDs))
	getGold := int64(0)
	for _, uniqueID := range uniqueIDs {
		index := s.sphereIndexByUniqueIDLocked(uniqueID)
		if index < 0 {
			return 0, 0, 0, nil, errSphereUnavailable
		}
		if s.spheres[index].IsLock != 0 {
			return 0, 0, 0, nil, errSphereLocked
		}
		if _, duplicate := remove[uniqueID]; duplicate {
			return 0, 0, 0, nil, errors.New("duplicate sphere sell selection")
		}
		remove[uniqueID] = struct{}{}
		definition := s.sphereDefinitions[s.spheres[index].SphereID]
		getGold += int64(definition.SellGold)
		if getGold > math.MaxInt || int64(s.gold)+getGold > math.MaxInt {
			return 0, 0, 0, nil, errors.New("sphere sell gold overflow")
		}
	}
	kept := make([]release.Sphere, 0, len(s.spheres)-len(remove))
	for _, sphere := range s.spheres {
		if _, sold := remove[sphere.UniqueID]; !sold {
			kept = append(kept, sphere)
		}
	}
	s.spheres = kept
	s.clearSphereIDsFromDecksLocked(remove)
	s.gold += int(getGold)
	return int(getGold), s.gold, len(s.spheres), s.rankedDecksLocked(s.decks), nil
}

func (s *store) setSphereLock(uniqueID int64, locked bool) error {
	if uniqueID <= 0 {
		return errors.New("invalid sphere unique ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.sphereIndexByUniqueIDLocked(uniqueID)
	if index < 0 {
		return errors.New("unknown sphere unique ID")
	}
	if locked {
		s.spheres[index].IsLock = 1
	} else {
		s.spheres[index].IsLock = 0
	}
	return nil
}

func (a *API) sphereFusion(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		BaseUniqueID int64               `json:"base_uniqid"`
		Inputs       []sphereFusionInput `json:"add_inputs"`
	}
	if err := decodeExact(request, []string{"base_uniqid", "add_inputs"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	base, successType, gold, count, decks, err := a.store.fuseSphere(payload.BaseUniqueID, payload.Inputs)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, wireSphereFusion{
		SuccessType: successType, BaseSphere: toWireSphere(base), Gold: gold, SphereNum: count,
		Decks: toWireDecks(decks), BackUniqueIDs: []int64{}, BackStackCards: []wireCardStackInfo{},
	})
}

func (a *API) sphereEvolution(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		BaseUniqueID     int64 `json:"base_uniqid"`
		MaterialUniqueID int64 `json:"add_uniqid"`
		MaterialCardID   int   `json:"add_cardid"`
	}
	if err := decodeExact(request, []string{"base_uniqid", "add_uniqid", "add_cardid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	base, gold, count, decks, err := a.store.evolveSphere(
		payload.BaseUniqueID, payload.MaterialUniqueID, payload.MaterialCardID,
	)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, wireSphereEvolution{
		BaseSphere: toWireSphere(base), Gold: gold, SphereNum: count,
		Decks: toWireDecks(decks), Reward: nil,
	})
}

func (a *API) sphereSell(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		UniqueIDs []int64 `json:"uniqids"`
	}
	if err := decodeExact(request, []string{"uniqids"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	getGold, gold, count, decks, err := a.store.sellSpheres(payload.UniqueIDs)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, wireSphereSell{
		SphereNum: count, GetGold: getGold, Gold: gold, Decks: toWireDecks(decks),
	})
}

func (a *API) setSphereLock(writer http.ResponseWriter, request *http.Request, locked bool) {
	var payload struct {
		UniqueID int64 `json:"uniqid"`
	}
	if err := decodeExact(request, []string{"uniqid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if err := a.store.setSphereLock(payload.UniqueID, locked); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, struct{}{})
}

func (a *API) sphereLock(writer http.ResponseWriter, request *http.Request) {
	a.setSphereLock(writer, request, true)
}

func (a *API) sphereUnlock(writer http.ResponseWriter, request *http.Request) {
	a.setSphereLock(writer, request, false)
}

func (s *store) validateDeckSpheres(deck deckInfo) error {
	if len(deck.SphereUniqueIDs) != deckSphereSlots {
		return fmt.Errorf("sphr_uniqid must contain %d entries", deckSphereSlots)
	}
	families := make(map[int]struct{}, deckSphereSlots)
	for _, uniqueID := range deck.SphereUniqueIDs {
		if uniqueID == 0 {
			continue
		}
		index := s.sphereIndexByUniqueIDLocked(uniqueID)
		if index < 0 {
			return fmt.Errorf("unknown sphere unique ID %d", uniqueID)
		}
		definition := s.sphereDefinitions[s.spheres[index].SphereID]
		if deck.ArthurType < 1 || deck.ArthurType > 4 || !definition.EquipAllowed[deck.ArthurType-1] {
			return fmt.Errorf("sphere %d cannot equip Arthur %d", uniqueID, deck.ArthurType)
		}
		if _, duplicate := families[definition.SameSphereID]; duplicate {
			return fmt.Errorf("sphere family %d is repeated in the deck", definition.SameSphereID)
		}
		families[definition.SameSphereID] = struct{}{}
	}
	return nil
}
