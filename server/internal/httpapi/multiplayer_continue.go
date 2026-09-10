package httpapi

import (
	"errors"
	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/release"
)

// User-approved local policy, shared by solo and multiplayer. The corresponding
// original-client dialog adapter is Patch-CNLocalContinue.ps1.
const continueCrystalCost = 50

type MultiplayerBattleContinuer interface {
	ChargeMultiplayerContinue(multiplayer.BattleContinue) (multiplayer.ContinueBalance, error)
}

func (h *accountBusinessHandler) ChargeMultiplayerContinue(request multiplayer.BattleContinue) (multiplayer.ContinueBalance, error) {
	a := h.api
	if request.UserID != a.release.State.User.UserID || request.RoomID <= 0 || request.BossID <= 0 || request.Sequence <= 0 {
		return multiplayer.ContinueBalance{}, errors.New("multiplayer continue identity is invalid")
	}
	return a.store.chargeMultiplayerContinue(request, a.release.State, a.persistState)
}

func (s *store) chargeMultiplayerContinue(request multiplayer.BattleContinue, base release.State, persist StatePersister) (multiplayer.ContinueBalance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, receipt := range s.teamBattleContinueReceipts {
		if receipt.RoomID == request.RoomID && receipt.Sequence == request.Sequence {
			if receipt.BossID != request.BossID {
				return multiplayer.ContinueBalance{}, errors.New("multiplayer continue receipt conflicts with room")
			}
			return multiplayer.ContinueBalance{Coin: s.coin, CoinFree: s.coinFree}, nil
		}
	}
	if s.coin+s.coinFree < continueCrystalCost {
		return multiplayer.ContinueBalance{}, errors.New("insufficient crystals for continuation")
	}
	previousCoin, previousFree, previousReceipts := s.coin, s.coinFree, s.teamBattleContinueReceipts
	freeSpend := min(s.coinFree, continueCrystalCost)
	s.coinFree -= freeSpend
	s.coin -= continueCrystalCost - freeSpend
	first := max(0, len(previousReceipts)-511)
	s.teamBattleContinueReceipts = append(append([]release.TeamBattleContinueReceipt(nil), previousReceipts[first:]...), release.TeamBattleContinueReceipt{
		RoomID: request.RoomID, BossID: request.BossID, Sequence: request.Sequence,
	})
	if persist != nil {
		if err := persist(s.snapshotLocked(base)); err != nil {
			s.coin, s.coinFree, s.teamBattleContinueReceipts = previousCoin, previousFree, previousReceipts
			return multiplayer.ContinueBalance{}, err
		}
	}
	return multiplayer.ContinueBalance{Coin: s.coin, CoinFree: s.coinFree}, nil
}
