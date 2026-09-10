package release

import "errors"

// The account owns the payer identity; the live room owns its monotonically
// increasing decision sequence. A sequence can succeed at most once.
type TeamBattleContinueReceipt struct {
	RoomID   int64 `json:"room_id"`
	BossID   int   `json:"boss_id"`
	Sequence int   `json:"sequence"`
}

func ValidateTeamBattleContinueReceipts(receipts []TeamBattleContinueReceipt) error {
	if len(receipts) > 512 {
		return errors.New("too many multiplayer continue receipts")
	}
	seen := make(map[[2]int64]bool, len(receipts))
	for _, receipt := range receipts {
		key := [2]int64{receipt.RoomID, int64(receipt.Sequence)}
		if receipt.RoomID <= 0 || receipt.BossID <= 0 || receipt.Sequence <= 0 || seen[key] {
			return errors.New("invalid or duplicate multiplayer continue receipt")
		}
		seen[key] = true
	}
	return nil
}
