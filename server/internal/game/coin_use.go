package game

import (
	"errors"
	"time"

	"kairisei.local/server/internal/gamestate"
)

const (
	coinUseAPHealFull = iota
	coinUseBPHealFull
	CoinUseCardExtend
)

const coinUseFullHealCrystalCost = 40

// The original window adds its base 100 to card_extend_limit.
const CardCapacityLimit = gamestate.CardCapacityLimit

func (s *Account) CardCapacity() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cardMax
}

func (s *Account) ExtendCardCapacity(option int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Original ExtendedPurchaseExecutor options: +5/+25/+50 for 40/200/400.
	amounts := [...]int{5, 25, 50}
	if option < 0 || option >= len(amounts) {
		return errors.New("unsupported card expansion option")
	}
	amount := amounts[option]
	if s.cardMax > CardCapacityLimit-amount {
		return &BusinessError{-1, "卡牌容量已达到可扩展上限。"}
	}
	cost := amount * 8
	if s.coin+s.coinFree < cost {
		return ErrInsufficientCrystals
	}
	freeSpend := min(s.coinFree, cost)
	s.coinFree -= freeSpend
	s.coin -= cost - freeSpend
	s.cardMax += amount
	return nil
}

func (s *Account) FullHealWithCrystals(coinUseType int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	switch coinUseType {
	case coinUseAPHealFull:
		s.refreshAPLocked(now)
		if s.ap >= s.apMax {
			return &BusinessError{-1, "探索点已满，无需恢复。"}
		}
	case coinUseBPHealFull:
		s.refreshBattlePointsLocked(now)
		if s.bp >= s.bpMax {
			return &BusinessError{-1042, "体力已满，无需恢复。"}
		}
	default:
		return errors.New("unsupported coin-use type")
	}

	if s.coin+s.coinFree < coinUseFullHealCrystalCost {
		return ErrInsufficientCrystals
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
