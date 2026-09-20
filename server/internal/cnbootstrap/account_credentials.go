package cnbootstrap

import (
	"net"
	"net/http"
	"sync"
	"time"

	"kairisei.local/server/internal/accountstore"
	adminapi "kairisei.local/server/internal/admin"
)

// Bound expensive password checks and per-address traffic without trusting
// forwarded headers. Expired buckets are removed and the map is bounded.
type cnAccountGateway struct {
	accounts *accountstore.Accounts
	mu       sync.Mutex
	requests map[string][]time.Time
	workers  chan struct{}
}

func newCNAccountGateway(accounts *accountstore.Accounts) *cnAccountGateway {
	return &cnAccountGateway{accounts: accounts, requests: make(map[string][]time.Time), workers: make(chan struct{}, 4)}
}

func (gateway *cnAccountGateway) allow(address string) bool {
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	now := time.Now()
	for key, times := range gateway.requests {
		for len(times) > 0 && now.Sub(times[0]) > time.Minute {
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

func (gateway *cnAccountGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	address, _, _ := net.SplitHostPort(r.RemoteAddr)
	if !gateway.allow(address) {
		adminapi.WriteAdminError(w, 429, "操作过于频繁，请稍后重试")
		return
	}
	select {
	case gateway.workers <- struct{}{}:
		defer func() { <-gateway.workers }()
	default:
		adminapi.WriteAdminError(w, 429, "服务繁忙，请稍后重试")
		return
	}
	// No query-string credentials, cookies or cross-origin browser form calls.
	if r.URL.RawQuery != "" || r.Header.Get("X-Kairisei-Account") != "1" {
		adminapi.WriteAdminError(w, 400, "账号请求无效")
		return
	}
	var payload struct {
		UUID     string `json:"uuid"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := adminapi.DecodeAdminJSONLimit(r, &payload, 2048); err != nil {
		adminapi.WriteAdminError(w, 400, "账号请求格式无效")
		return
	}
	var result accountstore.AccountBinding
	var err error
	switch r.URL.Path {
	case "/local/account/status":
		result, err = gateway.accounts.AccountBinding(payload.UUID)
	case "/local/account/bind":
		result, err = gateway.accounts.BindAccount(payload.UUID, payload.Username, payload.Password)
	case "/local/account/login":
		result, err = gateway.accounts.LoginAccount(payload.Username, payload.Password)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		adminapi.WriteAdminError(w, 400, err.Error())
		return
	}
	adminapi.WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "account": result})
}
