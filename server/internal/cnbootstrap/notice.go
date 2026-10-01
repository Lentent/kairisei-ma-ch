package cnbootstrap

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/accounthttp"
)

// Only the native Home notice view gets this signed URL. Merely asking for
// HomeShow does not consume the notice: failed/unopened pages can retry.
func (app *application) autoNotice(w http.ResponseWriter, r *http.Request) {
	page := newBufferedResponseWriter()
	app.operations.LocalNotice(page, r)
	if userID, popupID, ok := app.operations.NoticeReadClaim(r); ok {
		body := []byte(fmt.Sprintf(`{"popupid":%d,"is_system":1}`, popupID))
		ack := adaptCNBusinessRequest(r, http.MethodPost, "/PopupExec", body)
		ack.Header.Set(accounthttp.AccountUserHeader, strconv.Itoa(userID))
		ack = ack.WithContext(context.WithValue(ack.Context(), chi.RouteCtxKey, chi.NewRouteContext()))
		result := newBufferedResponseWriter()
		app.business.ServeHTTP(result, ack)
		// The account router evicts failed writes, leaving the persisted notice
		// unread for retry. The public page can still be shown if storage fails.
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	_, _ = w.Write(page.body.Bytes())
}
