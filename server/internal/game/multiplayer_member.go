package game

import (
	"errors"
	"fmt"

	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
)

func (s *Account) MultiplayerMember(userID int, arthurType int8, deckIndex int8) (multiplayer.Member, error) {
	cards, decks := s.Show()
	deck, found := ExactDeck(decks, arthurType, deckIndex)
	if !found {
		return multiplayer.Member{}, errors.New("local multiplayer deck is unavailable")
	}
	if len(deck.CardUniqueIDs) != 10 {
		return multiplayer.Member{}, errors.New("local multiplayer deck must contain ten cards")
	}
	cardByUniqueID := make(map[int64]CardInfo, len(cards))
	for _, card := range cards {
		cardByUniqueID[card.UniqueID] = card
	}
	leader, found := PartnerLeaderCard(deck, cardByUniqueID)
	if !found {
		return multiplayer.Member{}, errors.New("local multiplayer leader card is unavailable")
	}
	hp, attack, magic, mind := PartnerDeckStats(
		deck,
		cardByUniqueID,
		s.JobParameter(deck.JobType),
	)
	battleCards := make([]multiplayer.BattleCard, len(deck.CardUniqueIDs))
	for index, uniqueID := range deck.CardUniqueIDs {
		card, exists := cardByUniqueID[uniqueID]
		if !exists || card.CardID <= 0 || card.Level <= 0 {
			return multiplayer.Member{}, fmt.Errorf("local multiplayer card in deck slot %d is unavailable", index+1)
		}
		battleCards[index] = multiplayer.BattleCard{
			CardType: index + 1,
			CardID:   card.CardID,
			Level:    card.Level,
			Love:     card.Love,
		}
	}
	supportCards := make([]multiplayer.BattleCard, 0, len(deck.SupportCardUniqueIDs))
	for index, uniqueID := range deck.SupportCardUniqueIDs {
		if uniqueID == 0 {
			continue
		}
		card, exists := cardByUniqueID[uniqueID]
		if !exists || card.CardID <= 0 || card.Level <= 0 {
			return multiplayer.Member{}, fmt.Errorf("local multiplayer support card in deck slot %d is unavailable", index+1)
		}
		supportCards = append(supportCards, multiplayer.BattleCard{
			CardType: index + 11, CardID: card.CardID, Level: card.Level, Love: card.Love,
		})
	}
	sphereByUniqueID := make(map[int64]gamestate.Sphere)
	for _, sphere := range s.SphereState() {
		sphereByUniqueID[sphere.UniqueID] = sphere
	}
	battleSpheres := make([]multiplayer.BattleSphere, 0, len(deck.SphereUniqueIDs))
	for index, uniqueID := range deck.SphereUniqueIDs {
		if uniqueID == 0 {
			continue
		}
		sphere, exists := sphereByUniqueID[uniqueID]
		if !exists || sphere.SphereID <= 0 || sphere.Level <= 0 {
			return multiplayer.Member{}, fmt.Errorf("local multiplayer sphere in deck slot %d is unavailable", index+1)
		}
		battleSpheres = append(battleSpheres, multiplayer.BattleSphere{
			SphereType: index + 1,
			SphereID:   sphere.SphereID,
			Level:      sphere.Level,
		})
	}
	battleBuddies := make([]multiplayer.BattleBuddy, 0, len(deck.BuddyUniqueIDs))
	buddyByUniqueID := make(map[int64]gamestate.Buddy)
	for _, buddy := range s.BuddyState() {
		buddyByUniqueID[buddy.UniqueID] = buddy
	}
	for index, uniqueID := range deck.BuddyUniqueIDs {
		if uniqueID == 0 {
			continue
		}
		buddy, exists := buddyByUniqueID[uniqueID]
		if !exists || buddy.BuddyID <= 0 || buddy.Level <= 0 {
			return multiplayer.Member{}, fmt.Errorf("local multiplayer buddy in deck slot %d is unavailable", index+1)
		}
		battleBuddies = append(battleBuddies, multiplayer.BattleBuddy{
			BuddyType: index + 1,
			BuddyID:   buddy.BuddyID,
			Level:     buddy.Level,
		})
	}
	avatarIndex := int(arthurType) - 1
	avatars := s.AvatarsState()
	if avatarIndex < 0 || avatarIndex >= len(avatars) {
		return multiplayer.Member{}, errors.New("local multiplayer avatar is unavailable")
	}
	honorIDs, _ := s.HonorState()
	if len(honorIDs) != 4 {
		return multiplayer.Member{}, errors.New("local multiplayer honor deck is unavailable")
	}
	avatar := avatars[avatarIndex]
	return multiplayer.Member{
		UserID:       userID,
		IsBurst:      int(s.ArthurBurstUnlocked(arthurType)),
		Level:        s.UserLevel(),
		ArthurType:   int(arthurType),
		JobType:      int(deck.JobType),
		HP:           hp,
		Attack:       attack,
		Magic:        magic,
		Mind:         mind,
		Name:         s.UserName(),
		LeaderCardID: leader.CardID,
		LeaderFame:   leader.Fame,
		LeaderLevel:  leader.Level,
		DeckRank:     int(deck.DeckRank),
		DeckName:     deck.Name,
		CostumeID:    avatar.CostumeID,
		PartsIDs:     append([]int(nil), avatar.AvatarPartIDs...),
		DeckHonorIDs: append([]int(nil), honorIDs...),
		DeckCards:    battleCards,
		SupportCards: supportCards,
		DeckSpheres:  battleSpheres,
		DeckBuddies:  battleBuddies,
		IsBuddy:      BoolInt(len(battleBuddies) > 0),
	}, nil
}

// multiplayerOwnerFallbackParty projects available active Arthur decks
// from the room owner's persisted account. Missing/unconfigured professions
// can still be filled by humans; countdown checks the actual remaining roles.
// These are combat participants, not
// connected room users, so their stable character IDs use the same per-account
// namespace as the local solo partner projections.
func (s *Account) MultiplayerOwnerFallbackParty(userID int, selectedArthurType int8) []multiplayer.Member {
	_, decks := s.Show()
	result := make([]multiplayer.Member, 0, 3)
	for arthurType := int8(1); arthurType <= 4; arthurType++ {
		if arthurType == selectedArthurType {
			continue
		}
		deckIndex, found := ActiveDeckIndex(decks, arthurType)
		if !found {
			continue
		}
		member, err := s.MultiplayerMember(userID, arthurType, deckIndex)
		if err != nil {
			continue
		}
		member.UserID = userID*10 + int(arthurType)
		member.Name = s.UserName()
		member.IsRoomLoading = 1
		result = append(result, member)
	}
	return result
}

func ActiveDeckIndex(decks []DeckInfo, arthurType int8) (int8, bool) {
	var fallback int8
	hasFallback := false
	for _, deck := range decks {
		if deck.ArthurType != arthurType {
			continue
		}
		if !hasFallback || deck.Index < fallback {
			fallback = deck.Index
			hasFallback = true
		}
		if deck.IsActive != 0 {
			return deck.Index, true
		}
	}
	return fallback, hasFallback
}
func ExactDeck(decks []DeckInfo, arthurType int8, deckIndex int8) (DeckInfo, bool) {
	for _, deck := range decks {
		if deck.ArthurType == arthurType && deck.Index == deckIndex {
			return deck, true
		}
	}
	return DeckInfo{}, false
}

func PartnerDeckStats(
	deck DeckInfo,
	cards map[int64]CardInfo,
	job gamestate.JobParameter,
) (int, int, int, int) {
	hp, attack, magic, mind := job.HP, job.Attack, job.Magic, job.Mind
	for index, uniqueID := range deck.CardUniqueIDs {
		card, exists := cards[uniqueID]
		if !exists {
			continue
		}
		if index == int(deck.LeaderCardIndex) {
			// CN battle5_api_leader_card_param_calc: each card parameter *150/100,
			// truncated separately. Job stats, support cards and skill power are
			// not leader parameters.
			card.HP = card.HP * 150 / 100
			card.Attack = card.Attack * 150 / 100
			card.Magic = card.Magic * 150 / 100
			card.Mind = card.Mind * 150 / 100
		}
		hp += card.HP
		attack += card.Attack
		magic += card.Magic
		mind += card.Mind
	}
	for _, uniqueID := range deck.SupportCardUniqueIDs {
		card, exists := cards[uniqueID]
		if !exists {
			continue
		}
		// CN CardUtility truncates getLovePer before calling native
		// battle5_api_support_card_param_calc. card_param_bonus.csv SUPPORT
		// supplies HP/ATK/MAG/MIND rates 60/200/200/100; native scales them
		// by (10 + 90*lovePercent/100) percent, then truncates each stat.
		lovePercent := 100 // CardCsvData.getLovePer: cards without a love cap use 100%.
		if card.LoveMax > 0 {
			lovePercent = max(0, min(100, card.Love*100/card.LoveMax))
		}
		scale := float64(lovePercent)*90/100 + 10
		hp += int((60 * scale / 100) * float64(card.HP) / 100)
		attack += int((200 * scale / 100) * float64(card.Attack) / 100)
		magic += int((200 * scale / 100) * float64(card.Magic) / 100)
		mind += int((100 * scale / 100) * float64(card.Mind) / 100)
	}
	return hp, attack, magic, mind
}
