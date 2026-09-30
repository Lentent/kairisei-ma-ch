package gamestate

import (
	"fmt"
	"math"
	"time"
)

func ValidateItemShopProgress(total map[int]int, periods map[int]ItemShopPeriodCounts) error {
	for id, count := range total {
		if id <= 0 || count < 0 || count > math.MaxInt32 {
			return fmt.Errorf("invalid item-shop purchase count %d", id)
		}
	}
	for id, p := range periods {
		if id <= 0 {
			return fmt.Errorf("invalid item-shop period identity %d", id)
		}
		for _, v := range []struct {
			key, layout string
			count       int
		}{{p.Day, "2006-01-02", p.DayCount}, {p.Week, "2006-01-02", p.WeekCount}, {p.Month, "2006-01", p.MonthCount}} {
			if v.count < 0 || v.count > total[id] {
				return fmt.Errorf("invalid item-shop period count %d", id)
			}
			if v.key == "" && v.count == 0 {
				continue
			}
			if _, err := time.Parse(v.layout, v.key); err != nil {
				return fmt.Errorf("invalid item-shop period date %d", id)
			}
		}
	}
	return nil
}
