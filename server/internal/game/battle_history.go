package game

import (
	"errors"
	"kairisei.local/server/internal/gamestate"
	"time"
)

type BattleHistoryRepository interface {
	PersistSoloClear(gamestate.State, gamestate.BattleClearRecord, string) error
	RecentBattleClears(int, time.Time) ([]gamestate.BattleClearRecord, error)
	YesterdayBattleRanks(int, int, time.Time) ([]gamestate.BattleClearRecord, error)
}

func (s *Account) SetTeamBattleClearDecks(bossID int, decks []gamestate.BattleClearDeck) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeBattle == nil || s.activeBattle.BossID != bossID || len(decks) != 4 {
		return errors.New("clear-deck snapshot does not match active battle")
	}
	if len(s.activeBattle.ClearDecks) == 0 {
		s.activeBattle.ClearDecks = gamestate.CloneBattleClearDecks(decks)
	}
	return nil
}
