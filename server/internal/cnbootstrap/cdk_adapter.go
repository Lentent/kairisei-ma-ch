package cnbootstrap

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"kairisei.local/server/internal/accountstore"
)

type nativeCDKRedeemer interface{ RedeemCDK(string, int) error }

func writeCNGiftCodeResult(w http.ResponseWriter, code int) {
	writeCNProtocolResponseWithPopup(w, cnBootstrapCommon(), map[string]any{"code": code})
}

// CN 6.0.2 GiftCodeWindow.sendCode -> ProtoGen.GiftCodeAdSend (method 141).
// Send: session prefix + {channel:string,codenumber:string}.
// Receive: common, {code:int}, popup. Code 0 opens the native success window;
// positive codes address StrTable (10001,1,code) inside the input window.
func cnBootstrapGiftCodeAd(service nativeCDKRedeemer) http.HandlerFunc {
	// Reuse the bounded request-window limiter, keyed by authenticated role.
	limiter := &cnAccountGateway{requests: make(map[string][]time.Time)}
	return func(w http.ResponseWriter, r *http.Request) {
		respond := func(code int) { writeCNGiftCodeResult(w, code) }
		userID, authenticated := r.Context().Value(cnAuthenticatedUserKey{}).(int)
		if !authenticated || userID < accountstore.PrimaryUserID || userID >= accountstore.SystemPartnerUserIDBase {
			respond(8)
			return
		}
		if !limiter.allow(strconv.Itoa(userID)) {
			respond(9)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 2049))
		if err != nil || len(body) > 2048 {
			respond(1)
			return
		}
		_, payload, ok := splitCNSessionPayload(body)
		if !ok {
			respond(1)
			return
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(payload, &fields) != nil || len(fields) != 2 || fields["channel"] == nil || fields["codenumber"] == nil {
			respond(1)
			return
		}
		var input struct {
			Channel string `json:"channel"`
			Code    string `json:"codenumber"`
		}
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil || len(input.Channel) > 128 {
			respond(1)
			return
		}
		// channel is client metadata, never an identity or reward entitlement.
		err = service.RedeemCDK(input.Code, userID)
		switch {
		case err == nil:
			respond(0)
		case errors.Is(err, accountstore.ErrCDKExpired):
			respond(2)
		case errors.Is(err, accountstore.ErrCDKUsed), errors.Is(err, accountstore.ErrCDKExhausted):
			respond(3)
		case errors.Is(err, accountstore.ErrCDKUnavailable):
			respond(1)
		default:
			respond(5)
		}
	}
}
