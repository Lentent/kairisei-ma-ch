package httpapi

import (
	"errors"
	"net/http"
	"time"

	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/release"
)

// MultiplayerBattleStarter must be called under the same account lock as
// ServeHTTP. It is an in-process domain boundary, never a public HTTP route.
type MultiplayerBattleStarter interface {
	ChargeMultiplayerStart(multiplayer.BattleStart) error
}

type accountBusinessHandler struct {
	http.Handler
	api             *API
	gachaRevision   uint64
	contentRevision uint64
	playerRevision  uint64
}

func (h *accountBusinessHandler) ChargeMultiplayerStart(start multiplayer.BattleStart) error {
	a := h.api
	if start.OwnerUserID != a.release.State.User.UserID || start.RoomID <= 0 || start.BossID <= 0 || start.BPUse < 0 {
		return errors.New("multiplayer start identity is invalid")
	}
	return a.store.chargeMultiplayerStart(start, a.release.State, a.persistState)
}

func (s *store) chargeMultiplayerStart(start multiplayer.BattleStart, base release.State, persist StatePersister) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, receipt := range s.teamBattleStartReceipts {
		if receipt.RoomID != start.RoomID {
			continue
		}
		if receipt.BossID != start.BossID || receipt.BPUse != start.BPUse {
			return errors.New("multiplayer start receipt conflicts with room")
		}
		return nil
	}
	now := time.Now()
	s.refreshBattlePointsLocked(now)
	if start.BPUse < 0 || s.bp < start.BPUse {
		return errors.New("multiplayer host battle points are insufficient")
	}
	previousBP, previousRecovery, previousReceipts := s.bp, s.bpNextRecovery, s.teamBattleStartReceipts
	s.bp -= start.BPUse
	if s.bp < s.bpMax && s.bpNextRecovery.IsZero() {
		s.bpNextRecovery = now.Add(s.bpRecoveryInterval)
	}
	// Active rooms never survive a process restart and completed rooms expire
	// after 30 minutes. Keep a bounded recent ledger for retry acknowledgement;
	// only the live Hub can request a start, never a client-provided room ID.
	const retainedStarts = 512
	first := max(0, len(previousReceipts)-(retainedStarts-1))
	s.teamBattleStartReceipts = append(append([]release.TeamBattleStartReceipt(nil), previousReceipts[first:]...), release.TeamBattleStartReceipt{
		RoomID: start.RoomID, BossID: start.BossID, BPUse: start.BPUse,
	})
	if persist != nil {
		if err := persist(s.snapshotLocked(base)); err != nil {
			s.bp, s.bpNextRecovery, s.teamBattleStartReceipts = previousBP, previousRecovery, previousReceipts
			return err
		}
	}
	return nil
}

func (s *store) prepaidTeamBattleCostLocked(roomID int64, bossID int) (int, bool) {
	for _, receipt := range s.teamBattleStartReceipts {
		if receipt.RoomID == roomID && receipt.BossID == bossID {
			return receipt.BPUse, true
		}
	}
	return 0, false
}
