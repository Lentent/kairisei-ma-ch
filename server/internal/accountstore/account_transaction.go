package accountstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"kairisei.local/server/internal/gamestate"
)

// PersistStates commits a room's start fees and receipts together. Callers hold
// all participating account locks; the SQLite writer serializes this transaction
// with unrelated saves without any room or registry lock.
func (accounts *Accounts) PersistStates(states []gamestate.State) error {
	if len(states) == 0 || len(states) > 4 {
		return errors.New("account transaction requires one to four participants")
	}
	contents := make([][]byte, len(states))
	seen := make(map[int]bool, len(states))
	for i, state := range states {
		userID := state.User.UserID
		if userID < PrimaryUserID || userID >= SystemPartnerUserIDBase || seen[userID] {
			return errors.New("account transaction has an invalid or duplicate user")
		}
		seen[userID] = true
		content, err := EncodeAccountMetadata(state)
		if err != nil {
			return err
		}
		contents[i] = content
	}
	database, err := accounts.storage.Open()
	if err != nil {
		return err
	}
	transaction, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("begin account transaction: %w", err)
	}
	defer transaction.Rollback()
	updatedUTC := time.Now().UTC().Format(time.RFC3339Nano)
	for i, state := range states {
		digest := sha256.Sum256(contents[i])
		digestText := hex.EncodeToString(digest[:])
		if state.User.UserID == PrimaryUserID {
			err = writePrimaryAccountSnapshot(transaction, state, contents[i], digestText, updatedUTC, nil)
		} else {
			err = writeAccountSnapshot(transaction, state.User.UserID, state, contents[i], digestText, updatedUTC, nil)
		}
		if err != nil {
			return err
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit account transaction: %w", err)
	}
	for _, state := range states {
		accounts.RememberBattle(state.User.UserID, state.ActiveTeamBattle, state.StoryTeamBattleSession)
	}
	return nil
}
