package admin

import (
	_ "embed"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/accountstore"
)

//go:embed web/cdk.html
var cdkHTML []byte

//go:embed web/cdk.js
var cdkJS []byte

type cdkGateway struct {
	admin    *API
	mu       sync.Mutex
	requests map[string][]time.Time
	workers  chan struct{}
}

func (gateway *cdkGateway) allow(address string) bool {
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	now := time.Now()
	for key, times := range gateway.requests {
		for len(times) > 0 && now.Sub(times[0]) >= time.Minute {
			times = times[1:]
		}
		if len(times) == 0 {
			delete(gateway.requests, key)
		} else {
			gateway.requests[key] = times
		}
	}
	times := gateway.requests[address]
	if len(times) >= 12 || len(times) == 0 && len(gateway.requests) >= 4096 {
		return false
	}
	gateway.requests[address] = append(times, now)
	return true
}

func (gateway *cdkGateway) redeem(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" || r.Header.Get("X-Kairisei-CDK") != "1" || !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		WriteAdminError(w, 400, "兑换请求格式无效")
		return
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		WriteAdminError(w, 403, "请在本服务器兑换页面提交")
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host != r.Host || u.Scheme != "http" && u.Scheme != "https" {
			WriteAdminError(w, 403, "兑换来源无效")
			return
		}
	}
	address, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		WriteAdminError(w, 400, "请求来源无效")
		return
	}
	if !gateway.allow(address) {
		w.Header().Set("Retry-After", "60")
		WriteAdminError(w, 429, "操作过于频繁，请稍后重试")
		return
	}
	select {
	case gateway.workers <- struct{}{}:
		defer func() { <-gateway.workers }()
	default:
		WriteAdminError(w, 429, "服务繁忙，请稍后重试")
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := DecodeAdminJSONLimit(r, &input, 2048); err != nil {
		WriteAdminError(w, 400, "兑换请求格式无效")
		return
	}
	identity, err := gateway.admin.accounts.LoginAccount(input.Username, input.Password)
	if err != nil {
		WriteAdminError(w, 401, "账号或密码不正确，请使用游戏内已绑定账号")
		return
	}
	result, err := gateway.admin.redeemCDK(input.Code, identity.UserID)
	if err != nil {
		status := 400
		if errors.Is(err, accountstore.ErrDocumentConflict) {
			status = 409
		}
		if !errors.Is(err, accountstore.ErrCDKUnavailable) && !errors.Is(err, accountstore.ErrCDKExpired) && !errors.Is(err, accountstore.ErrCDKUsed) && !errors.Is(err, accountstore.ErrCDKExhausted) && !errors.Is(err, accountstore.ErrDocumentConflict) && !errors.Is(err, errCDKInboxFull) {
			WriteAdminError(w, 500, "兑换未完成，请稍后重试或联系管理员")
			return
		}
		WriteAdminError(w, status, err.Error())
		return
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "result": result, "message": "兑换成功，奖励已发到游戏礼物箱"})
}

func (admin *API) cdkPublicRouter() http.Handler {
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
			next.ServeHTTP(w, r)
		})
	})
	router.Get("/cdk", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(cdkHTML)
	})
	router.Get("/cdk.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write(cdkJS)
	})
	gateway := &cdkGateway{admin: admin, requests: map[string][]time.Time{}, workers: make(chan struct{}, 4)}
	router.Post("/api/cdk/redeem", gateway.redeem)
	return router
}
