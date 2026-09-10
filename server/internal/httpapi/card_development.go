package httpapi

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"time"

	"kairisei.local/server/internal/release"
)

const (
	cardDecomposeFame = 1
	cardDecomposeCard = 2

	cardDevelopmentDecompose = 1
	cardDevelopmentTrain     = 2
)

type cardFameTrainState struct {
	UniqueID  int64
	BeginTime int
	Remain    int
	Fame      int
	Stive     int
	IsLock    int8
	Coin      int
	CoinFree  int
}

type howToGetCardEntry struct {
	Type      int    `json:"type"`
	Subtype   int    `json:"subtype"`
	ContentID int    `json:"contentid"`
	Text      string `json:"text"`
}

type howToGetCardList struct {
	CardID   int                 `json:"cardid"`
	GetCards []howToGetCardEntry `json:"get_cards"`
}

func (s *store) refreshCardFameTrainingLocked(now time.Time) bool {
	training := s.cardFameTraining
	if training == nil {
		return false
	}
	duration := int64(training.Fame) * int64(s.cardDevelopmentPolicy.TimeEveryFameSeconds)
	if duration <= 0 || now.Unix() < training.BeginAtUnix+duration {
		return false
	}
	index := cardIndexByUniqueID(s.cards, training.UniqueID)
	if index >= 0 {
		rule := s.cardDevelopmentRules[s.cards[index].CardID]
		candidate := s.cards[index]
		candidate.Fame = min(candidate.Fame+training.Fame, rule.FameMax)
		candidate.IsLock = 0
		normalized, err := s.normalizeCardLocked(candidate)
		if err != nil {
			return false
		}
		s.cards[index] = normalized
	}
	s.cardFameTraining = nil
	return true
}

func (s *store) cardDevelopmentHomeState(now time.Time) (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshCardFameTrainingLocked(now)
	return s.stive, s.cardFameRemainLocked(now)
}

func (s *store) cardFameRemainLocked(now time.Time) int {
	if s.cardFameTraining == nil {
		return 0
	}
	total := int64(s.cardFameTraining.Fame) * int64(s.cardDevelopmentPolicy.TimeEveryFameSeconds)
	remain := s.cardFameTraining.BeginAtUnix + total - now.Unix()
	if remain <= 0 {
		return 0
	}
	if remain > math.MaxInt32 {
		return math.MaxInt32
	}
	return int(remain)
}

func (s *store) cardFameInfo(now time.Time) (cardFameTrainState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := s.refreshCardFameTrainingLocked(now)
	result := cardFameTrainState{Stive: s.stive, Coin: s.coin, CoinFree: s.coinFree}
	if s.cardFameTraining == nil {
		return result, changed
	}
	result.UniqueID = s.cardFameTraining.UniqueID
	result.BeginTime = int(s.cardFameTraining.BeginAtUnix)
	result.Remain = s.cardFameRemainLocked(now)
	result.Fame = s.cardFameTraining.Fame
	result.IsLock = 1
	return result, changed
}

func (s *store) clearCardFromDecksLocked(uniqueID int64) []deckInfo {
	updated := make([]deckInfo, 0)
	for deckIndex := range s.decks {
		changed := false
		for slot, current := range s.decks[deckIndex].CardUniqueIDs {
			if current == uniqueID {
				s.decks[deckIndex].CardUniqueIDs[slot] = 0
				changed = true
			}
		}
		for slot, current := range s.decks[deckIndex].SupportCardUniqueIDs {
			if current == uniqueID {
				s.decks[deckIndex].SupportCardUniqueIDs[slot] = 0
				changed = true
			}
		}
		if !changed {
			continue
		}
		leader := int(s.decks[deckIndex].LeaderCardIndex)
		if leader < 0 || leader >= len(s.decks[deckIndex].CardUniqueIDs) ||
			s.decks[deckIndex].CardUniqueIDs[leader] == 0 {
			s.decks[deckIndex].LeaderCardIndex = 0
			for slot, current := range s.decks[deckIndex].CardUniqueIDs {
				if current != 0 {
					s.decks[deckIndex].LeaderCardIndex = int8(slot)
					break
				}
			}
		}
		updated = append(updated, cloneDeck(s.decks[deckIndex]))
	}
	return s.rankedDecksLocked(updated)
}

func (s *store) decomposeCard(uniqueID int64, decomposeType int) (int, []deckInfo, error) {
	if uniqueID <= 0 || (decomposeType != cardDecomposeFame && decomposeType != cardDecomposeCard) {
		return 0, nil, errors.New("invalid card decomposition selection")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index := cardIndexByUniqueID(s.cards, uniqueID)
	if index < 0 || s.cards[index].IsLock != 0 {
		return 0, nil, errors.New("card decomposition target is unavailable")
	}
	card := s.cards[index]
	rule, exists := s.cardDevelopmentRules[card.CardID]
	if !exists || rule.DevelopmentType != cardDevelopmentDecompose || rule.DecomposeRadix <= 0 {
		return 0, nil, errors.New("card is not decomposable")
	}
	fame := card.Fame
	if fame < 1 {
		return 0, nil, errors.New("card fame is invalid")
	}
	consumedFame := fame
	if decomposeType == cardDecomposeFame {
		consumedFame--
		if consumedFame <= 0 {
			return 0, nil, errors.New("card has no decomposable fame")
		}
	}
	gain := int64(rule.DecomposeRadix) * int64(consumedFame)
	if gain <= 0 || gain > math.MaxInt || int64(s.stive)+gain > math.MaxInt {
		return 0, nil, errors.New("card decomposition stive overflow")
	}
	if decomposeType == cardDecomposeFame {
		candidate := s.cards[index]
		candidate.Fame = 1
		normalized, err := s.normalizeCardLocked(candidate)
		if err != nil {
			return 0, nil, err
		}
		s.stive += int(gain)
		s.cards[index] = normalized
		return s.stive, []deckInfo{}, nil
	}
	s.stive += int(gain)
	decks := s.clearCardFromDecksLocked(uniqueID)
	s.cards = append(s.cards[:index], s.cards[index+1:]...)
	return s.stive, decks, nil
}

func (s *store) startCardFameTraining(uniqueID int64, fame int, now time.Time) (cardFameTrainState, error) {
	if uniqueID <= 0 || fame <= 0 {
		return cardFameTrainState{}, errors.New("invalid card fame training selection")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cardFameTraining != nil {
		return cardFameTrainState{}, errors.New("another card fame training is active")
	}
	index := cardIndexByUniqueID(s.cards, uniqueID)
	if index < 0 || s.cards[index].IsLock != 0 {
		return cardFameTrainState{}, errors.New("card fame training target is unavailable")
	}
	card := s.cards[index]
	rule, exists := s.cardDevelopmentRules[card.CardID]
	if !exists || rule.DevelopmentType != cardDevelopmentTrain || rule.DevelopRadix <= 0 ||
		rule.FameMax <= card.Fame || fame > rule.FameMax-card.Fame {
		return cardFameTrainState{}, errors.New("card cannot train the requested fame")
	}
	cost := int64(rule.DevelopRadix) * int64(fame)
	duration := int64(s.cardDevelopmentPolicy.TimeEveryFameSeconds) * int64(fame)
	if cost <= 0 || duration <= 0 || duration > math.MaxInt32 {
		return cardFameTrainState{}, errors.New("unsupported card fame cost or duration")
	}
	if cost > int64(s.stive) {
		return cardFameTrainState{}, errInsufficientStive
	}
	begin := now.Unix()
	if begin <= 0 || begin > math.MaxInt32 {
		return cardFameTrainState{}, errors.New("card fame training clock is outside the client range")
	}
	s.stive -= int(cost)
	s.cards[index].IsLock = 1
	s.cardFameTraining = &release.CardFameTraining{UniqueID: uniqueID, BeginAtUnix: begin, Fame: fame}
	return cardFameTrainState{
		UniqueID: uniqueID, BeginTime: int(begin), Remain: int(duration), Fame: fame,
		Stive: s.stive, IsLock: 1, Coin: s.coin, CoinFree: s.coinFree,
	}, nil
}

func (s *store) cancelCardFameTraining(uniqueID int64, now time.Time) (cardFameTrainState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	training := s.cardFameTraining
	if training == nil || training.UniqueID != uniqueID {
		return cardFameTrainState{}, errors.New("card fame training does not match")
	}
	index := cardIndexByUniqueID(s.cards, uniqueID)
	if index < 0 {
		return cardFameTrainState{}, errors.New("card fame training target is missing")
	}
	rule := s.cardDevelopmentRules[s.cards[index].CardID]
	elapsed := max(int64(0), now.Unix()-training.BeginAtUnix)
	completed := int(elapsed / int64(s.cardDevelopmentPolicy.TimeEveryFameSeconds))
	completed = min(completed, training.Fame)
	refund := int64(training.Fame-completed) * int64(rule.DevelopRadix)
	if refund < 0 || int64(s.stive)+refund > math.MaxInt {
		return cardFameTrainState{}, errors.New("card fame training refund overflow")
	}
	candidate := s.cards[index]
	candidate.Fame = min(candidate.Fame+completed, rule.FameMax)
	candidate.IsLock = 0
	normalized, err := s.normalizeCardLocked(candidate)
	if err != nil {
		return cardFameTrainState{}, err
	}
	s.stive += int(refund)
	s.cards[index] = normalized
	s.cardFameTraining = nil
	return cardFameTrainState{
		UniqueID: uniqueID, Fame: s.cards[index].Fame, Stive: s.stive,
		IsLock: 0, Coin: s.coin, CoinFree: s.coinFree,
	}, nil
}

func (s *store) finishCardFameTraining(uniqueID int64, now time.Time) (cardFameTrainState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	training := s.cardFameTraining
	if training == nil || training.UniqueID != uniqueID {
		return cardFameTrainState{}, errors.New("card fame training does not match")
	}
	index := cardIndexByUniqueID(s.cards, uniqueID)
	if index < 0 {
		return cardFameTrainState{}, errors.New("card fame training target is missing")
	}
	remain := s.cardFameRemainLocked(now)
	hours := (remain + 3599) / 3600
	cost := hours * s.cardDevelopmentPolicy.CoinEveryHour
	if cost <= 0 {
		return cardFameTrainState{}, errors.New("card fame completion cost is invalid")
	}
	if s.coin+s.coinFree < cost {
		return cardFameTrainState{}, errInsufficientCrystals
	}
	freeSpend := min(s.coinFree, cost)
	rule := s.cardDevelopmentRules[s.cards[index].CardID]
	candidate := s.cards[index]
	candidate.Fame = min(candidate.Fame+training.Fame, rule.FameMax)
	candidate.IsLock = 0
	normalized, err := s.normalizeCardLocked(candidate)
	if err != nil {
		return cardFameTrainState{}, err
	}
	s.coinFree -= freeSpend
	s.coin -= cost - freeSpend
	s.cards[index] = normalized
	s.cardFameTraining = nil
	return cardFameTrainState{
		UniqueID: uniqueID, Fame: s.cards[index].Fame, IsLock: 0,
		Stive: s.stive, Coin: s.coin, CoinFree: s.coinFree,
	}, nil
}

func (s *store) howToGetCards(cardIDs []int) ([]howToGetCardList, error) {
	if len(cardIDs) == 0 || len(cardIDs) > 64 {
		return nil, errors.New("card acquisition query is empty or too large")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := make(map[int]struct{}, len(cardIDs))
	result := make([]howToGetCardList, len(cardIDs))
	visibleGachas := s.visibleGachasLocked()
	for index, cardID := range cardIDs {
		_, cardExists := s.cardDefinitions[cardID]
		_, stackExists := s.stackCardTemplates[cardID]
		_, developmentExists := s.cardDevelopmentRules[cardID]
		// Growth eligibility is unrelated to looking up a card's acquisition
		// sources. Round-table and EXP materials live in the stack master.
		if cardID <= 0 || (!cardExists && !stackExists && !developmentExists) {
			return nil, fmt.Errorf("unknown card acquisition query %d", cardID)
		}
		if _, duplicate := seen[cardID]; duplicate {
			return nil, fmt.Errorf("duplicate card acquisition query %d", cardID)
		}
		seen[cardID] = struct{}{}
		entries := make([]howToGetCardEntry, 0)
		for _, transition := range s.cardActions.EvolutionTransitions {
			if transition.ToCardID != cardID {
				continue
			}
			typeID := 4 + transition.Type
			if transition.Type == 3 {
				typeID = 12
			}
			entries = append(entries, howToGetCardEntry{Type: typeID, ContentID: transition.FromCardID})
		}
		for _, gacha := range visibleGachas {
			for _, poolCardID := range gacha.CardIDs {
				if poolCardID != cardID {
					continue
				}
				entries = append(entries, howToGetCardEntry{
					Type:      7,
					ContentID: gacha.GachaID,
					Text:      gacha.Name,
				})
				break
			}
		}
		sort.Slice(entries, func(left, right int) bool {
			if entries[left].Type != entries[right].Type {
				return entries[left].Type < entries[right].Type
			}
			return entries[left].ContentID < entries[right].ContentID
		})
		if len(entries) == 0 {
			entries = []howToGetCardEntry{{Type: 0, Text: ""}}
		}
		result[index] = howToGetCardList{CardID: cardID, GetCards: entries}
	}
	return result, nil
}

func (a *API) cardDecompose(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		UniqueID int64 `json:"base_uniqid"`
		Type     int   `json:"type"`
	}
	if err := decodeExact(request, []string{"base_uniqid", "type"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	stive, decks, err := a.store.decomposeCard(payload.UniqueID, payload.Type)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"stive": stive, "decompose_type": payload.Type, "uniqid": payload.UniqueID,
		"decks": toWireDecks(decks),
	})
}

func (a *API) cardFameTrainInfo(writer http.ResponseWriter, _ *http.Request) {
	state, changed := a.store.cardFameInfo(time.Now())
	if changed && !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"uniqid": state.UniqueID, "begin_tm": state.BeginTime, "remain_tm": state.Remain,
		"fame": state.Fame, "time_every_fame": a.store.cardDevelopmentPolicy.TimeEveryFameSeconds,
		"coin_every_hour": a.store.cardDevelopmentPolicy.CoinEveryHour,
		"url":             a.baseURL + a.store.cardDevelopmentPolicy.HelpPath,
	})
}

func (a *API) cardFameStartTrain(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		UniqueID int64 `json:"base_uniqid"`
		Fame     int   `json:"base_fame"`
	}
	if err := decodeExact(request, []string{"base_uniqid", "base_fame"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	state, err := a.store.startCardFameTraining(payload.UniqueID, payload.Fame, time.Now())
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"uniqid": state.UniqueID, "begin_tm": state.BeginTime, "remain_tm": state.Remain,
		"fame": state.Fame, "stive": state.Stive, "is_lock": state.IsLock,
	})
}

func (a *API) cardFameCancelTrain(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		UniqueID int64 `json:"base_uniqid"`
	}
	if err := decodeExact(request, []string{"base_uniqid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	state, err := a.store.cancelCardFameTraining(payload.UniqueID, time.Now())
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"uniqid": state.UniqueID, "fame": state.Fame, "stive": state.Stive, "is_lock": state.IsLock,
	})
}

func (a *API) cardFameTrainFinish(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		UniqueID int64 `json:"base_uniqid"`
	}
	if err := decodeExact(request, []string{"base_uniqid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	state, err := a.store.finishCardFameTraining(payload.UniqueID, time.Now())
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"uniqid": state.UniqueID, "fame": state.Fame, "coin": state.Coin,
		"coin_free": state.CoinFree, "is_lock": state.IsLock,
	})
}

func (a *API) howToGetCardShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		CardIDs []int `json:"cardids"`
	}
	if err := decodeExact(request, []string{"cardids"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	lists, err := a.store.howToGetCards(payload.CardIDs)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	a.writeProtocol(writer, map[string]any{"how_to_list": lists})
}
