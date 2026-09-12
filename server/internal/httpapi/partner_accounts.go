package httpapi

import (
	"errors"
	"sort"

	"kairisei.local/server/internal/release"
)

type friendPointPartnerView struct {
	IsBurst         int8
	LastLoginUnix   int64
	InviteID        string
	UserID          int
	Name            string
	Comment         string
	Level           int
	PVPPoint        int
	ArthurType      int8
	Cards           map[int64]cardInfo
	CardAttributes  map[int]uint8
	CardKinds       map[int]int
	Decks           []deckInfo
	Avatar          release.Avatar
	Spheres         map[int64]release.Sphere
	Buddies         map[int64]release.Buddy
	SupportUnlocked int8
	Jobs            []release.JobParameter
	HonorIDs        []int
	FriendState     int8
	System          bool
}

func deckInfosFromRelease(source []release.Deck) []deckInfo {
	decks := make([]deckInfo, len(source))
	for index, deck := range source {
		decks[index] = deckInfo{
			ArthurType:           deck.ArthurType,
			Index:                deck.Index,
			JobType:              deck.JobType,
			LeaderCardIndex:      deck.LeaderCardIndex,
			CardUniqueIDs:        append([]int64(nil), deck.CardUniqueIDs...),
			SupportCardUniqueIDs: append([]int64(nil), deck.SupportCardUniqueIDs...),
			SphereUniqueIDs:      fixedInt64Slots(deck.SphereUniqueIDs, deckSphereSlots),
			BuddyUniqueIDs:       fixedInt64Slots(deck.BuddyUniqueIDs, deckBuddySlots),
			Name:                 deck.Name,
			IsActive:             deck.IsActive,
			IsRental:             deck.IsRental,
			DeckRank:             deck.DeckRank,
		}
	}
	return decks
}

func friendPointPartnerViewFromState(state release.State) (friendPointPartnerView, bool) {
	arthurType := int8(state.User.ActiveArthurType)
	if state.User.UserID <= 0 || state.User.Name == "" || arthurType < 1 || arthurType > 4 ||
		len(state.Avatars) != 4 || len(state.SupportDeck.UnlockSlotNums) != 4 {
		return friendPointPartnerView{}, false
	}
	attributes := make(map[int]uint8, len(state.Cards))
	for _, card := range state.Cards {
		attributes[card.CardID] = card.FusionAttributes
	}
	cards := cardInfosFromRelease(state.Cards, 0)
	cardByUniqueID := make(map[int64]cardInfo, len(cards))
	for _, card := range cards {
		cardByUniqueID[card.UniqueID] = card
	}
	decks := deckInfosFromRelease(state.Decks)
	rentalDeck, found := selectRentalPartnerDeck(decks, arthurType, cardByUniqueID)
	if !found {
		return friendPointPartnerView{}, false
	}
	arthurType = rentalDeck.ArthurType
	buddies := make(map[int64]release.Buddy, len(state.Buddies))
	for _, buddy := range state.Buddies {
		buddies[buddy.UniqueID] = buddy
	}
	spheres := make(map[int64]release.Sphere, len(state.Spheres))
	for _, sphere := range state.Spheres {
		spheres[sphere.UniqueID] = sphere
	}
	honorIDs := make([]int, 4)
	copy(honorIDs, state.Honors.DeckHonorIDs)
	return friendPointPartnerView{
		IsBurst:         release.ArthurBurstUnlocked(state.User.UnlockedFeatureIDs, arthurType),
		InviteID:        state.User.InviteID,
		LastLoginUnix:   state.LastLoginUnix,
		UserID:          state.User.UserID,
		Name:            state.User.Name,
		Comment:         state.User.Comment,
		Level:           state.User.Level,
		PVPPoint:        state.User.PVPPoint,
		ArthurType:      arthurType,
		Cards:           cardByUniqueID,
		CardAttributes:  attributes,
		Decks:           decks,
		Avatar:          state.Avatars[int(arthurType)-1],
		Spheres:         spheres,
		Buddies:         buddies,
		SupportUnlocked: state.SupportDeck.UnlockSlotNums[int(arthurType)-1],
		Jobs:            append([]release.JobParameter(nil), state.User.Jobs...),
		HonorIDs:        honorIDs,
	}, true
}

func (view friendPointPartnerView) job(jobType int8) release.JobParameter {
	index := int(jobType)
	if index < 0 || index >= len(view.Jobs) {
		return release.JobParameter{}
	}
	return view.Jobs[index]
}

func (view friendPointPartnerView) activeDeck() (deckInfo, bool) {
	return selectRentalPartnerDeck(view.Decks, view.ArthurType, view.Cards)
}

// The native deck editor marks the public rental decks independently of the
// Arthur currently used by the owner. Never substitute an active battle deck
// when the owner has explicitly selected rental decks.
func selectRentalPartnerDeck(decks []deckInfo, fallbackArthur int8, cards map[int64]cardInfo) (deckInfo, bool) {
	hasRental := false
	for _, deck := range decks {
		if deck.IsRental == 0 {
			continue
		}
		hasRental = true
		if complete, ok := selectCompletePartnerDeck([]deckInfo{deck}, deck.ArthurType, cards); ok {
			return complete, true
		}
	}
	if hasRental {
		return deckInfo{}, false
	}
	return selectCompletePartnerDeck(decks, fallbackArthur, cards)
}

func (view friendPointPartnerView) listWire(friendPoint int, friendState int8) (map[string]any, error) {
	deck, found := view.activeDeck()
	if !found {
		return nil, errors.New("friend-point partner active deck is unavailable")
	}
	leader, found := partnerLeaderCard(deck, view.Cards)
	if !found {
		return nil, errors.New("friend-point partner leader is unavailable")
	}
	hp, attack, magic, mind := partnerDeckStats(deck, view.Cards, view.job(deck.JobType))
	return map[string]any{
		"userid":        view.UserID,
		"name":          view.Name,
		"lv":            view.Level,
		"arthur_type":   view.ArthurType,
		"is_burst":      view.IsBurst,
		"job_type":      deck.JobType,
		"hp":            hp,
		"atkp":          attack,
		"intp":          magic,
		"mndp":          mind,
		"deck_rank":     deck.DeckRank,
		"leader_card":   partnerCardWire(leader),
		"get_fp":        friendPoint,
		"comment":       view.Comment,
		"friend_state":  friendState,
		"rookie_type":   0,
		"deck_name":     deck.Name,
		"rental_idx":    deck.Index,
		"play_log_turn": 0,
		"attr_nums":     view.deckAttributeCounts(deck),
		"kind_nums":     view.deckKindCounts(deck),
		"deck_honorids": append([]int(nil), view.HonorIDs...),
	}, nil
}

func (view friendPointPartnerView) deckWire(deckIndex int8) (map[string]any, error) {
	deck, found := exactDeck(view.Decks, view.ArthurType, deckIndex)
	if !found {
		return nil, errors.New("friend-point partner deck is unavailable")
	}
	return teamBattlePartnerDeckWire(
		view.UserID,
		view.ArthurType,
		deck,
		view.Cards,
		view.job(deck.JobType),
		view.Avatar,
		view.Spheres,
		view.Buddies,
		view.SupportUnlocked,
		view.IsBurst,
	)
}

func (view friendPointPartnerView) rentalDeckWires() ([]any, error) {
	selected := make([]deckInfo, 0)
	hasRental := false
	for _, deck := range view.Decks {
		hasRental = hasRental || deck.IsRental != 0
	}
	for _, deck := range view.Decks {
		if deck.ArthurType != view.ArthurType || (hasRental && deck.IsRental == 0) {
			continue
		}
		if _, complete := selectCompletePartnerDeck([]deckInfo{deck}, view.ArthurType, view.Cards); complete {
			selected = append(selected, deck)
		}
	}
	sort.Slice(selected, func(left, right int) bool {
		return selected[left].Index < selected[right].Index
	})
	result := make([]any, 0, len(selected))
	for _, deck := range selected {
		wire, err := view.deckWire(deck.Index)
		if err != nil {
			return nil, err
		}
		result = append(result, wire)
	}
	if len(result) == 0 {
		return nil, errors.New("friend-point partner has no complete rental deck")
	}
	return result, nil
}

func (a *API) friendPointPartnerViews() ([]friendPointPartnerView, error) {
	if a.friendPointAccounts == nil {
		return nil, nil
	}
	relations, err := a.friendPointAccounts.ListFriendPointAccountRelations(a.release.State.User.UserID)
	if err != nil {
		return nil, err
	}
	views := make([]friendPointPartnerView, 0, len(relations))
	for _, relation := range relations {
		if view, ok := friendPointPartnerViewFromState(relation.State); ok {
			// Public account projections omit master fields; resolve attributes from
			// the shared catalog rather than enlarging persisted account summaries.
			a.store.mu.RLock()
			for _, card := range view.Cards {
				if definition, ok := a.store.cardDefinitions[card.CardID]; ok {
					view.CardAttributes[card.CardID] = definition.FusionAttributes
				}
			}
			a.store.mu.RUnlock()
			cardIDs := make([]int, 0, len(view.Cards))
			for _, card := range view.Cards {
				cardIDs = append(cardIDs, card.CardID)
			}
			view.CardKinds = a.multiplayer.PartnerCardKinds(cardIDs)
			view.FriendState = relation.FriendState
			view.System = relation.System
			views = append(views, view)
		}
	}
	sort.Slice(views, func(left, right int) bool {
		return views[left].UserID < views[right].UserID
	})
	return views, nil
}

func (view friendPointPartnerView) deckKindCounts(deck deckInfo) []int {
	counts := make([]int, 8)
	for _, id := range deck.CardUniqueIDs {
		card, ok := view.Cards[id]
		if !ok {
			continue
		}
		if kind, known := view.CardKinds[card.CardID]; known && kind >= 0 && kind < len(counts) {
			counts[kind]++
		}
	}
	return counts
}

func friendPointAccountFriend(relation FriendPointAccountRelation) (release.Friend, bool) {
	view, ok := friendPointPartnerViewFromState(relation.State)
	if !ok {
		return release.Friend{}, false
	}
	deck, ok := view.activeDeck()
	if !ok {
		return release.Friend{}, false
	}
	leader, ok := partnerLeaderCard(deck, view.Cards)
	if !ok {
		return release.Friend{}, false
	}
	hp, attack, magic, mind := partnerDeckStats(deck, view.Cards, view.job(deck.JobType))
	return release.Friend{
		InviteID:        view.InviteID,
		UserID:          view.UserID,
		Name:            view.Name,
		ArthurType:      view.ArthurType,
		IsBurst:         view.IsBurst,
		JobType:         deck.JobType,
		Level:           view.Level,
		DeckRank:        deck.DeckRank,
		FriendState:     relation.FriendState,
		LeaderCardID:    leader.CardID,
		LeaderCardLevel: leader.Level,
		LeaderCardFame:  leader.Fame,
		LastLoginTime:   int(relation.State.LastLoginUnix),
		Comment:         view.Comment,
		PVPPoint:        view.PVPPoint,
		DeckHonorIDs:    append([]int(nil), view.HonorIDs...),
		HP:              hp,
		Attack:          attack,
		Magic:           magic,
		Mind:            mind,
	}, true
}

func (a *API) localAccountFriendRelations() ([]FriendPointAccountRelation, error) {
	if a.friendPointAccounts == nil {
		return nil, errors.New("local-account friend repository is unavailable")
	}
	return a.friendPointAccounts.ListFriendPointAccountRelations(a.release.State.User.UserID)
}

func (a *API) friendPointAccountStates(targetUserIDs []int) (map[int]int8, error) {
	states := make(map[int]int8)
	if a.friendPointAccounts == nil || len(targetUserIDs) == 0 {
		return states, nil
	}
	return a.friendPointAccounts.FriendPointAccountStates(
		a.release.State.User.UserID,
		targetUserIDs,
	)
}

func findFriendPointPartnerView(views []friendPointPartnerView, userID int) (friendPointPartnerView, bool) {
	for _, view := range views {
		if view.UserID == userID {
			return view, true
		}
	}
	return friendPointPartnerView{}, false
}

// Native ATTR indexes: NULL=0, FIRE=1, ICE=2, WIND=3, LIGHT=4, DARK=5.
// Multi-attribute cards count once in each of their attributes; support cards
// and the leader's duplicate display are not additional main-deck slots.
func (view friendPointPartnerView) deckAttributeCounts(deck deckInfo) []int {
	counts := make([]int, 6)
	for _, id := range deck.CardUniqueIDs {
		card, ok := view.Cards[id]
		if !ok {
			continue
		}
		mask, known := view.CardAttributes[card.CardID]
		if !known {
			continue
		}
		if mask == 0 {
			counts[0]++
			continue
		}
		for bit := 0; bit < 5; bit++ {
			if mask&(1<<bit) != 0 {
				counts[bit+1]++
			}
		}
	}
	return counts
}
