package httpapi

import (
	"errors"
	"net/http"
	"time"

	"kairisei.local/server/internal/release"
)

const (
	coinUseAPHealFull = iota
	coinUseBPHealFull
	coinUseCardExtend
)

const coinUseFullHealCrystalCost = 40

// The original window adds its base 100 to card_extend_limit.
const cardCapacityLimit = release.CardCapacityLimit
const cardCapacityBase = 100

func (a *API) coinUse(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		Type  int `json:"type"`
		Param int `json:"param"`
	}
	if err := decodeExact(request, []string{"type", "param"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.Type != coinUseCardExtend && payload.Param != 0 {
		writeError(writer, http.StatusBadRequest, "unsupported coin-use parameter")
		return
	}
	var err error
	if payload.Type == coinUseCardExtend {
		err = a.store.extendCardCapacity(payload.Param)
	} else {
		err = a.store.fullHealWithCrystals(payload.Type)
	}
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"type": payload.Type,
		"user": a.userPayload(),
	})
}

func (s *store) cardCapacity() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cardMax
}

func (s *store) extendCardCapacity(option int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Original ExtendedPurchaseExecutor options: +5/+25/+50 for 40/200/400.
	amounts := [...]int{5, 25, 50}
	if option < 0 || option >= len(amounts) {
		return errors.New("unsupported card expansion option")
	}
	amount := amounts[option]
	if s.cardMax > cardCapacityLimit-amount {
		return &businessError{-1, "卡牌容量已达到可扩展上限。"}
	}
	cost := amount * 8
	if s.coin+s.coinFree < cost {
		return errInsufficientCrystals
	}
	freeSpend := min(s.coinFree, cost)
	s.coinFree -= freeSpend
	s.coin -= cost - freeSpend
	s.cardMax += amount
	return nil
}

func (s *store) fullHealWithCrystals(coinUseType int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	switch coinUseType {
	case coinUseAPHealFull:
		s.refreshAPLocked(now)
		if s.ap >= s.apMax {
			return &businessError{-1, "探索点已满，无需恢复。"}
		}
	case coinUseBPHealFull:
		s.refreshBattlePointsLocked(now)
		if s.bp >= s.bpMax {
			return &businessError{-1042, "体力已满，无需恢复。"}
		}
	default:
		return errors.New("unsupported coin-use type")
	}

	if s.coin+s.coinFree < coinUseFullHealCrystalCost {
		return errInsufficientCrystals
	}
	freeSpend := min(s.coinFree, coinUseFullHealCrystalCost)
	s.coinFree -= freeSpend
	s.coin -= coinUseFullHealCrystalCost - freeSpend

	switch coinUseType {
	case coinUseAPHealFull:
		s.ap = s.apMax
		s.apNextRecovery = time.Time{}
	case coinUseBPHealFull:
		s.bp = s.bpMax
		s.bpNextRecovery = time.Time{}
	}
	return nil
}
