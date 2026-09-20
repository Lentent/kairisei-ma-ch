package cnbootstrap

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

func cnBootstrapTradeShopBuy2(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := readCNSessionPayload(request, "TradeShopBuy2")
		if err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		var raw map[string]json.RawMessage
		if json.Unmarshal(payload, &raw) != nil || (len(raw) != 2 && len(raw) != 3) {
			http.Error(writer, "invalid CN TradeShopBuy2 payload", http.StatusBadRequest)
			return
		}
		if _, exists := raw["lineupid"]; !exists {
			http.Error(writer, "missing CN TradeShopBuy2 field: lineupid", http.StatusBadRequest)
			return
		}
		if _, exists := raw["num"]; !exists {
			http.Error(writer, "missing CN TradeShopBuy2 field: num", http.StatusBadRequest)
			return
		}
		if len(raw) == 3 {
			if _, exists := raw["uniqids"]; !exists {
				http.Error(writer, "invalid CN TradeShopBuy2 payload", http.StatusBadRequest)
				return
			}
		}
		var lineupID int
		var num int
		if json.Unmarshal(raw["lineupid"], &lineupID) != nil ||
			json.Unmarshal(raw["num"], &num) != nil || lineupID <= 0 || num <= 0 {
			http.Error(writer, "invalid CN TradeShopBuy2 fields", http.StatusBadRequest)
			return
		}
		if uniqidsRaw, exists := raw["uniqids"]; exists {
			var uniqids []int64
			if json.Unmarshal(uniqidsRaw, &uniqids) != nil || len(uniqids) == 0 {
				http.Error(writer, "invalid CN TradeShopBuy2 uniqids", http.StatusBadRequest)
				return
			}
			seen := make(map[int64]struct{}, len(uniqids))
			for _, uniqid := range uniqids {
				if uniqid <= 0 {
					http.Error(writer, "invalid CN TradeShopBuy2 uniqids", http.StatusBadRequest)
					return
				}
				if _, duplicate := seen[uniqid]; duplicate {
					http.Error(writer, "duplicate CN TradeShopBuy2 uniqid", http.StatusBadRequest)
					return
				}
				seen[uniqid] = struct{}{}
			}
		}
		businessHandler.ServeHTTP(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/TradeShopBuy2", payload),
		)
	}
}

func cnBootstrapItemShow(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := readCNSessionPayload(request, "ItemShow")
		if err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(payload, &fields); err != nil || len(fields) != 1 {
			http.Error(writer, "invalid CN ItemShow payload", http.StatusBadRequest)
			return
		}
		itemTypeRaw, ok := fields["item_type"]
		var itemType int
		if !ok || json.Unmarshal(itemTypeRaw, &itemType) != nil {
			http.Error(writer, "invalid CN ItemShow fields", http.StatusBadRequest)
			return
		}
		adapted := request.Clone(request.Context())
		adapted.Body = io.NopCloser(bytes.NewReader(payload))
		adapted.ContentLength = int64(len(payload))
		businessHandler.ServeHTTP(writer, adapted)
	}
}
