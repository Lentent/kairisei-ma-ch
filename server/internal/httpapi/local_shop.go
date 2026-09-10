package httpapi

import (
	"encoding/hex"
	"errors"
	"maps"
	"math"
	"net/http"
	"strings"
	"time"

	"kairisei.local/server/internal/release"
)

const localShopFirstPay = math.MaxInt32 // no retired first-purchase promotions

func cloneLocalShop(s release.LocalShopState) release.LocalShopState {
	s.Orders = maps.Clone(s.Orders)
	return s
}

func localShopDay(now time.Time) int64 { return (now.Unix() + 8*3600) / 86400 }

func (s *store) localCardFlagsLocked(now time.Time) (uint32, uint32, int) {
	day := localShopDay(now)
	days := max(int64(0), s.localShop.MonthEndDay-day)
	month := uint32(1)
	if s.localShop.MonthEndDay > 0 {
		month = 3
	}
	if days > 0 {
		month = 0
		if s.localShop.MonthClaimDay == day {
			month = 2
		}
	}
	forever := uint32(1)
	if s.localShop.Forever {
		forever = 0
		if s.localShop.ForeverClaimDay == day {
			forever = 2
		}
	}
	// Local renewal has no expired-card double-crystal promotion. Preserve the
	// native expired state without advertising that retired promotion.
	return uint32(min(days, 65535))<<16 | month, forever, int(max(int64(-1), s.localShop.MonthEndDay-day))
}

func (s *store) localCardPayload(now time.Time) map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	month, forever, days := s.localCardFlagsLocked(now)
	return map[string]any{"monthcardflag": month, "forevercardflag": forever, "monthcardtime": days}
}

func (a *API) localShopQueryCard(w http.ResponseWriter, _ *http.Request) {
	a.writeProtocol(w, a.store.localCardPayload(time.Now()))
}

func localOrderProduct(order string) (release.LocalShopProduct, error) {
	parts := strings.Split(order, ":")
	if len(parts) != 3 || parts[0] != "local" || len(parts[2]) != 32 {
		return release.LocalShopProduct{}, errors.New("invalid local order")
	}
	if _, err := hex.DecodeString(parts[2]); err != nil {
		return release.LocalShopProduct{}, errors.New("invalid local order identity")
	}
	for _, product := range release.LocalShopProducts() {
		if product.ID == parts[1] {
			return product, nil
		}
	}
	return release.LocalShopProduct{}, errors.New("unknown local product")
}

func (s *store) localPurchase(order string, now time.Time, base release.State, persist StatePersister) (map[string]any, error) {
	product, err := localOrderProduct(order)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	added, known := s.localShop.Orders[order]
	if !known {
		previous := cloneLocalShop(s.localShop)
		previousCoin := s.coinFree
		added = product.Crystal
		if !s.localCrystalPurchaseEnabled {
			added = 0
		}
		if product.ID == "2" && s.localShop.Forever {
			added = 0
		}
		if int64(s.coin)+int64(s.coinFree)+int64(added) > math.MaxInt32 {
			return nil, errCrystalCapacity
		}
		day := localShopDay(now)
		if product.ID == "1" {
			end := max(day, s.localShop.MonthEndDay) + 30
			if end-day > 65535 {
				return nil, &businessError{-1, "月卡有效期已达上限。"}
			}
			s.localShop.MonthEndDay = end
		}
		if product.ID == "2" {
			s.localShop.Forever = true
		}
		s.coinFree += added
		if s.localShop.Orders == nil {
			s.localShop.Orders = make(map[string]int)
		}
		s.localShop.Orders[order] = added
		if persist != nil {
			if err := persist(s.snapshotLocked(base)); err != nil {
				s.localShop, s.coinFree = previous, previousCoin
				return nil, err
			}
		}
	}
	return map[string]any{"code": 1, "coin": s.coin, "coin_free": s.coinFree, "firstpay": localShopFirstPay,
		"totalcost": 0, "totalcoin": 0, "addcoin": added}, nil
}

func (a *API) localShopQueryOrder(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Order string `json:"order"`
	}
	if err := decodeExact(r, []string{"order"}, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := localOrderProduct(input.Order); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := a.store.localPurchase(input.Order, time.Now(), a.release.State, a.persistState)
	if err != nil {
		if !a.writeBusinessError(w, err) {
			writeError(w, http.StatusInternalServerError, "local purchase could not be saved")
		}
		return
	}
	a.writeProtocol(w, result)
}

func (s *store) localCardClaim(cardType int, now time.Time, base release.State, persist StatePersister) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	day := localShopDay(now)
	code := 0
	claimDay := &s.localShop.MonthClaimDay
	if cardType == 0 {
		switch {
		case s.localShop.MonthEndDay == 0:
			code = 2
		case s.localShop.MonthEndDay <= day:
			code = 3
		case *claimDay == day:
			code = 4
		}
	} else {
		claimDay = &s.localShop.ForeverClaimDay
		switch {
		case !s.localShop.Forever:
			code = 5
		case *claimDay == day:
			code = 6
		}
	}
	if code == 0 {
		if int64(s.coin)+int64(s.coinFree)+100 > math.MaxInt32 {
			return nil, errCrystalCapacity
		}
		previousDay, previousCoin := *claimDay, s.coinFree
		*claimDay = day
		s.coinFree += 100
		if persist != nil {
			if err := persist(s.snapshotLocked(base)); err != nil {
				*claimDay, s.coinFree = previousDay, previousCoin
				return nil, err
			}
		}
	}
	return map[string]any{"code": code, "coin": s.coin, "coin_free": s.coinFree}, nil
}

func (a *API) localShopGetCard(w http.ResponseWriter, r *http.Request) {
	var input struct {
		CardType int `json:"cardtype"`
	}
	if err := decodeExact(r, []string{"cardtype"}, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if input.CardType < 0 || input.CardType > 1 {
		writeError(w, http.StatusBadRequest, "unknown card type")
		return
	}
	result, err := a.store.localCardClaim(input.CardType, time.Now(), a.release.State, a.persistState)
	if err != nil {
		if !a.writeBusinessError(w, err) {
			writeError(w, http.StatusInternalServerError, "card reward could not be saved")
		}
		return
	}
	a.writeProtocol(w, result)
}
