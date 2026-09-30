package game

import (
	"fmt"
	"maps"
	"sort"
	"time"

	"kairisei.local/server/internal/gamestate"
)

const OperatorItemShopFirstID = 70000001

func ConfiguredItemShopProduct(base gamestate.ItemShopLineup, setting ItemShopSetting, item gamestate.ItemDefinition) gamestate.ItemShopLineup {
	base.Disabled, base.Price = !setting.Enabled, setting.Price
	if setting.LineupID >= OperatorItemShopFirstID {
		kind := setting.BuyType
		if kind == 0 {
			kind = 1
		}
		base = gamestate.ItemShopLineup{LineupID: setting.LineupID, PictID: item.PictID, LineupName: item.Name, PayType: setting.PayType,
			Price: setting.Price, Disabled: !setting.Enabled, AppearEnd: -1, BuyNumMax: 99, Interiors: []gamestate.ItemShopInterior{{BuyType: kind, BuyTypeID: setting.ItemID, Num: 1}}}
		if kind != 1 {
			base.PictID = 0
		}
		if setting.Name != "" {
			base.LineupName = setting.Name
		}
	}
	if setting.PayType != 0 {
		base.PayType = setting.PayType
	}
	if setting.Quantity > 0 && len(base.Interiors) == 1 {
		base.Interiors = append([]gamestate.ItemShopInterior(nil), base.Interiors...)
		base.Interiors[0].Num = setting.Quantity
	}
	if setting.BuyNumMax > 0 {
		base.BuyNumMax = setting.BuyNumMax
	}
	if setting.LineupID >= OperatorItemShopFirstID {
		// The native shop renders note directly as the product description.
		// Stock fields supply the separate remaining-purchase label.
		entry := base.Interiors[0]
		switch entry.BuyType {
		case 1:
			base.Note = item.Description
		case 2:
			base.Note = "购买后可在聊天编辑中使用。"
		case 3:
			base.Note = fmt.Sprintf("卡牌仓库上限增加%d格。", entry.Num)
		case 4:
			base.Note = fmt.Sprintf("卡牌持有上限增加%d格。", entry.Num)
		}
	}
	return base
}

func shopPeriodKeys(now time.Time) (string, string, string) {
	local := now.In(time.FixedZone("CN shop day", 8*60*60))
	monday := local.AddDate(0, 0, -(int(local.Weekday())+6)%7)
	return local.Format("2006-01-02"), monday.Format("2006-01-02"), local.Format("2006-01")
}

func (s *Account) shopCountsAt(id int, now time.Time) gamestate.ItemShopPeriodCounts {
	p := s.itemShopPeriods[id]
	day, week, month := shopPeriodKeys(now)
	if p.Day != day {
		p.Day, p.DayCount = day, 0
	}
	if p.Week != week {
		p.Week, p.WeekCount = week, 0
	}
	if p.Month != month {
		p.Month, p.MonthCount = month, 0
	}
	return p
}

func (s *Account) itemShopLineupAt(base gamestate.ItemShopLineup, now time.Time) gamestate.ItemShopLineup {
	setting, ok := s.itemShopSettings[base.LineupID]
	if base.Hidden || !ok {
		return base
	}
	base = ConfiguredItemShopProduct(base, setting, s.itemDefinitions[setting.ItemID])
	remaining, total := -1, 0
	if setting.TotalLimit > 0 {
		total = setting.TotalLimit
		remaining = max(0, total-s.itemShopPurchases[base.LineupID])
	}
	if setting.PeriodLimit > 0 {
		counts := s.shopCountsAt(base.LineupID, now)
		used := map[string]int{"day": counts.DayCount, "week": counts.WeekCount, "month": counts.MonthCount}[setting.Period]
		left := max(0, setting.PeriodLimit-used)
		if remaining < 0 || left < remaining {
			remaining = left
		}
		if total == 0 || setting.PeriodLimit < total {
			total = setting.PeriodLimit
		}
	}
	if remaining >= 0 {
		base.StockType, base.StockNum, base.StockRemain = 2, total, remaining
		if setting.Period == "day" && setting.PeriodLimit > 0 {
			base.StockType = 1
		}
		base.BuyNumMax = min(base.BuyNumMax, remaining)
	} else {
		base.StockType, base.StockNum, base.StockRemain = 0, 0, 0
	}
	if len(base.Interiors) == 1 {
		entry := base.Interiors[0]
		capacity := base.BuyNumMax
		switch entry.BuyType {
		case 2:
			capacity = 1
			if _, owned := s.stampIDs[entry.BuyTypeID]; owned {
				capacity = 0
			}
		case 3:
			capacity = max(0, (CardCapacityLimit-s.cardContainerMax)/entry.Num)
		case 4:
			capacity = max(0, (CardCapacityLimit-s.cardMax)/entry.Num)
		}
		base.BuyNumMax = min(base.BuyNumMax, capacity)
		if entry.BuyType == 2 || (entry.BuyType >= 3 && capacity == 0) {
			base.StockType, base.StockNum = 2, max(1, base.StockNum)
			if remaining < 0 {
				base.StockRemain = capacity
			} else {
				base.StockRemain = min(base.StockRemain, capacity)
			}
		}
	}
	return base
}

func (s *Account) itemShopBaseTabs() []gamestate.ItemShopTab {
	tabs := cloneItemShopTabs(s.itemShopTabs)
	ids := []int{}
	for id := range s.itemShopSettings {
		if id >= OperatorItemShopFirstID {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	for _, id := range ids {
		setting := s.itemShopSettings[id]
		if setting.TabType < 0 || setting.TabType >= len(tabs) {
			continue
		}
		base := ConfiguredItemShopProduct(gamestate.ItemShopLineup{}, setting, s.itemDefinitions[setting.ItemID])
		tabs[setting.TabType].Lineup = append(tabs[setting.TabType].Lineup, base)
	}
	return tabs
}

func (s *Account) recordItemShopPurchase(id, num int, now time.Time) {
	if s.itemShopPurchases == nil {
		s.itemShopPurchases = map[int]int{}
	}
	if s.itemShopPeriods == nil {
		s.itemShopPeriods = map[int]gamestate.ItemShopPeriodCounts{}
	}
	p := s.shopCountsAt(id, now)
	p.DayCount += num
	p.WeekCount += num
	p.MonthCount += num
	s.itemShopPurchases[id] += num
	s.itemShopPeriods[id] = p
}

func (s *Account) snapshotItemShopProgress(state *gamestate.State) {
	state.ItemShopPurchases = maps.Clone(s.itemShopPurchases)
	state.ItemShopPeriods = maps.Clone(s.itemShopPeriods)
}
