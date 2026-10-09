package gamestate

// LocalShopState is account data saved with the existing SQLite snapshot. It
// contains no channel SDK orders or real payment credentials.
type LocalShopState struct {
	MonthEndDay     int64          `json:"month_end_day,omitempty"`
	MonthClaimDay   int64          `json:"month_claim_day,omitempty"`
	Forever         bool           `json:"forever,omitempty"`
	ForeverClaimDay int64          `json:"forever_claim_day,omitempty"`
	Orders          map[string]int `json:"orders,omitempty"`
}

type LocalShopProduct struct {
	ID      string
	Name    string
	Crystal int
}

// The monthly amounts match CN settings/str_table.csv 215/1000/1-3.
// Only the two free local cards are offered. This catalog also validates orders,
// so removed crystal bundles cannot be purchased through stale client requests.
func LocalShopProducts() []LocalShopProduct {
	return []LocalShopProduct{
		{"1", "本地月卡（30天）", 250},
		{"2", "本地无穷卡", 600},
	}
}
