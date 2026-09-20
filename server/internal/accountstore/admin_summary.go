package accountstore

type AccountSummary struct {
	Players int `json:"players"`
	Bound   int `json:"bound"`
	Guests  int `json:"guests"`
	System  int `json:"system"`
}

func (accounts *Accounts) AccountSummary() (AccountSummary, error) {
	result := AccountSummary{}
	db, err := accounts.storage.OpenRead()
	if err != nil {
		return result, err
	}
	err = db.QueryRow(`SELECT
		COALESCE(SUM(a.user_id<?),0),
		COALESCE(SUM(a.user_id<? AND c.user_id IS NOT NULL),0),
		COALESCE(SUM(a.user_id<? AND c.user_id IS NULL),0),
		COALESCE(SUM(a.user_id>=?),0)
		FROM cn_local_account a LEFT JOIN cn_account_credentials c ON c.user_id=a.user_id`,
		SystemPartnerUserIDBase, SystemPartnerUserIDBase, SystemPartnerUserIDBase, SystemPartnerUserIDBase).
		Scan(&result.Players, &result.Bound, &result.Guests, &result.System)
	return result, err
}
