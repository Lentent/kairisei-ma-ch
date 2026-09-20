package game

import (
	"encoding/json"
	"errors"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func (s *Account) PvpResultReceipt(battleID int) (gamestate.PVPResultReceipt, bool) {
	if battleID <= 0 {
		return gamestate.PVPResultReceipt{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	receipt, exists := s.pvpResultReceipts[battleID]
	if !exists {
		return gamestate.PVPResultReceipt{}, false
	}
	receipt.Response = append(json.RawMessage(nil), receipt.Response...)
	return receipt, true
}

func (s *Account) RecordPVPResultReceipt(
	battleID int,
	requestSHA256 string,
	response json.RawMessage,
	claimedAt time.Time,
) error {
	if battleID <= 0 || !isLowerSHA256Digest(requestSHA256) || claimedAt.IsZero() ||
		len(response) == 0 || len(response) > MaxRequestBytes || !json.Valid(response) {
		return errors.New("PVP result receipt is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, exists := s.pvpResultReceipts[battleID]; exists {
		if existing.RequestSHA256 != requestSHA256 || string(existing.Response) != string(response) {
			return errors.New("PVP result receipt conflicts with the completed battle")
		}
		return nil
	}
	if len(s.pvpResultReceipts) >= maxBattleReceipts {
		oldestBattleID := 0
		oldestClaimedAt := int64(0)
		for existingBattleID, receipt := range s.pvpResultReceipts {
			if oldestBattleID == 0 || receipt.ClaimedAtUnix < oldestClaimedAt ||
				(receipt.ClaimedAtUnix == oldestClaimedAt && existingBattleID < oldestBattleID) {
				oldestBattleID = existingBattleID
				oldestClaimedAt = receipt.ClaimedAtUnix
			}
		}
		delete(s.pvpResultReceipts, oldestBattleID)
	}
	s.pvpResultReceipts[battleID] = gamestate.PVPResultReceipt{
		BattleID:      battleID,
		RequestSHA256: requestSHA256,
		ClaimedAtUnix: claimedAt.Unix(),
		Response:      append(json.RawMessage(nil), response...),
	}
	return nil
}
