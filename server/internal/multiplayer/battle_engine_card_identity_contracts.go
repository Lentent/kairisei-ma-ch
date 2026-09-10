package multiplayer

import (
	"errors"
	"fmt"
)

func validateBattleCardIdentityContracts() error {
	row, err := cardIdentityResult(2, BattleCard{CardType: 3, CardID: 10131011, Level: 60}).CSV()
	if err != nil {
		return err
	}
	if row != "23,2,3,10131011,60" {
		return fmt.Errorf("card identity projection is %q", row)
	}
	if _, boundaryErr := (BattleResult{
		Command: resultCardIdentity,
		Args:    []int64{2, 3, 10131011},
	}).CSV(); boundaryErr == nil {
		return errors.New("card identity accepted a short row")
	}
	return nil
}
