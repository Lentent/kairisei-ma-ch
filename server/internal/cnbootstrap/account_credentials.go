package cnbootstrap

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const cnPasswordIterations = 600000

var errCNAccountCredentials = errors.New("账号或密码不正确")

// Credentials identify the existing user and are stored separately from
// player state and UUID session identities.
func initializeCNAccountCredentials(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS cn_account_credentials (
		user_id INTEGER PRIMARY KEY REFERENCES cn_local_account(user_id) ON DELETE CASCADE,
		username TEXT NOT NULL UNIQUE,
		password_salt BLOB NOT NULL CHECK(length(password_salt) = 16),
		password_hash BLOB NOT NULL CHECK(length(password_hash) = 32),
		iterations INTEGER NOT NULL CHECK(iterations = 600000),
		updated_utc TEXT NOT NULL)`)
	return err
}

func normalizeCNUsername(name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if len(name) < 3 || len(name) > 32 {
		return "", errors.New("账号须为 3–32 位字母、数字或下划线")
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return "", errors.New("账号须为 3–32 位字母、数字或下划线")
		}
	}
	return name, nil
}

func cnPasswordHash(password string) ([]byte, []byte, error) {
	if len(password) < 8 || len(password) > 128 {
		return nil, nil, errors.New("密码须为 8–128 字节")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, nil, err
	}
	hash, err := pbkdf2.Key(sha256.New, password, salt, cnPasswordIterations, 32)
	return salt, hash, err
}

type cnAccountBinding struct {
	UserID    int    `json:"user_id"`
	Username  string `json:"username"`
	LoginUUID string `json:"uuid,omitempty"`
}

func (accounts *cnAccountStore) accountBinding(uuid string) (cnAccountBinding, error) {
	uuid = strings.ToLower(strings.TrimSpace(uuid))
	if !validCNLoginUUID(uuid) {
		return cnAccountBinding{}, errors.New("本机登录凭据无效")
	}
	db, err := accounts.storage.open()
	if err != nil {
		return cnAccountBinding{}, err
	}
	defer db.Close()
	var result cnAccountBinding
	err = db.QueryRow(`SELECT a.user_id, COALESCE(c.username, '') FROM cn_local_account a
		LEFT JOIN cn_account_credentials c ON c.user_id = a.user_id WHERE a.login_uuid = ? AND a.user_id < ?`, uuid, cnSystemPartnerUserIDBase).Scan(&result.UserID, &result.Username)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	return result, err
}

// Binding requires possession of the same random UUID already used by the
// game's guest login. Public callers cannot choose a user ID or rebind a user.
func (accounts *cnAccountStore) bindAccount(uuid, name, password string) (cnAccountBinding, error) {
	name, err := normalizeCNUsername(name)
	if err != nil {
		return cnAccountBinding{}, err
	}
	salt, hash, err := cnPasswordHash(password)
	if err != nil {
		return cnAccountBinding{}, err
	}
	uuid = strings.ToLower(strings.TrimSpace(uuid))
	if !validCNLoginUUID(uuid) {
		return cnAccountBinding{}, errors.New("本机登录凭据无效")
	}
	accounts.mu.Lock()
	defer accounts.mu.Unlock()
	db, err := accounts.storage.open()
	if err != nil {
		return cnAccountBinding{}, err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return cnAccountBinding{}, err
	}
	defer tx.Rollback()
	identity, err := accounts.resolveLoginTransaction(tx, uuid, false)
	if err != nil {
		return cnAccountBinding{}, err
	}
	if err := writeCNCredentialsTransaction(tx, identity.UserID, name, salt, hash, false); err != nil {
		return cnAccountBinding{}, err
	}
	if err := tx.Commit(); err != nil {
		return cnAccountBinding{}, err
	}
	return cnAccountBinding{UserID: identity.UserID, Username: name, LoginUUID: identity.LoginUUID}, nil
}

func (accounts *cnAccountStore) writeCredentials(userID int, name string, salt, hash []byte, admin bool) error {
	if userID < cnPrimaryUserID || userID >= cnSystemPartnerUserIDBase {
		return errors.New("不能绑定系统伙伴")
	}
	accounts.mu.Lock()
	defer accounts.mu.Unlock()
	db, err := accounts.storage.open()
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := writeCNCredentialsTransaction(tx, userID, name, salt, hash, admin); err != nil {
		return err
	}
	return tx.Commit()
}

// Only the loopback Admin exposes unbinding. Keep the character, UUID and save;
// match the displayed username so a stale page cannot remove a newer binding.
func (accounts *cnAccountStore) unbindAccount(userID int, name string) error {
	if userID < cnPrimaryUserID || userID >= cnSystemPartnerUserIDBase {
		return errors.New("请选择玩家账号")
	}
	accounts.mu.Lock()
	defer accounts.mu.Unlock()
	db, err := accounts.storage.open()
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`DELETE FROM cn_account_credentials WHERE user_id = ? AND username = ?`, userID, name)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return errors.New("绑定已变化或已解除，请刷新账号列表")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := (&cnAdminAudit{Operation: "account-unbind", Target: fmt.Sprint(userID), Payload: map[string]any{"username": name}}).appendTo(tx, now); err != nil {
		return err
	}
	return tx.Commit()
}

func writeCNCredentialsTransaction(tx *sql.Tx, userID int, name string, salt, hash []byte, admin bool) error {
	if userID < cnPrimaryUserID || userID >= cnSystemPartnerUserIDBase {
		return errors.New("不能绑定系统伙伴")
	}
	var existingID int
	err := tx.QueryRow(`SELECT user_id FROM cn_account_credentials WHERE username = ?`, name).Scan(&existingID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && existingID != userID {
		return errors.New("该账号名已使用")
	}
	var existingName string
	err = tx.QueryRow(`SELECT username FROM cn_account_credentials WHERE user_id = ?`, userID).Scan(&existingName)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && !admin {
		return errors.New("当前角色已经绑定账号，请使用账号登录或联系管理员重置密码")
	}
	if err == nil && existingName != name {
		return errors.New("已绑定的账号名不可修改")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.Exec(`INSERT INTO cn_account_credentials(user_id, username, password_salt, password_hash, iterations, updated_utc)
		VALUES(?, ?, ?, ?, ?, ?) ON CONFLICT(user_id) DO UPDATE SET
		password_salt = excluded.password_salt, password_hash = excluded.password_hash, updated_utc = excluded.updated_utc`, userID, name, salt, hash, cnPasswordIterations, now)
	if err != nil {
		return fmt.Errorf("保存账号绑定失败: %w", err)
	}
	op := "account-bind"
	if admin {
		op = "account-credentials-admin"
	}
	if err := (&cnAdminAudit{Operation: op, Target: fmt.Sprint(userID), Payload: map[string]any{"username": name}}).appendTo(tx, now); err != nil {
		return err
	}
	return nil
}

func (accounts *cnAccountStore) loginAccount(name, password string) (cnAccountBinding, error) {
	name, err := normalizeCNUsername(name)
	if err != nil || len(password) < 8 || len(password) > 128 {
		return cnAccountBinding{}, errCNAccountCredentials
	}
	db, err := accounts.storage.open()
	if err != nil {
		return cnAccountBinding{}, err
	}
	defer db.Close()
	var result cnAccountBinding
	var salt, expected []byte
	var iterations int
	err = db.QueryRow(`SELECT a.user_id, c.username, a.login_uuid, c.password_salt, c.password_hash, c.iterations
		FROM cn_account_credentials c JOIN cn_local_account a ON a.user_id = c.user_id WHERE c.username = ?`, name).
		Scan(&result.UserID, &result.Username, &result.LoginUUID, &salt, &expected, &iterations)
	missing := errors.Is(err, sql.ErrNoRows)
	if err != nil && !missing {
		return cnAccountBinding{}, err
	}
	if missing {
		salt, expected, iterations = make([]byte, 16), make([]byte, 32), cnPasswordIterations
	}
	if iterations != cnPasswordIterations || len(salt) != 16 || len(expected) != 32 {
		return cnAccountBinding{}, errCNAccountCredentials
	}
	actual, err := pbkdf2.Key(sha256.New, password, salt, iterations, 32)
	if err != nil {
		return cnAccountBinding{}, err
	}
	if subtle.ConstantTimeCompare(actual, expected) != 1 || missing {
		return cnAccountBinding{}, errCNAccountCredentials
	}
	return result, nil
}

// Bound expensive password checks and per-address traffic without trusting
// forwarded headers. Expired buckets are removed and the map is bounded.
type cnAccountGateway struct {
	accounts *cnAccountStore
	mu       sync.Mutex
	requests map[string][]time.Time
	workers  chan struct{}
}

func newCNAccountGateway(accounts *cnAccountStore) *cnAccountGateway {
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
		writeCNAdminError(w, 429, "操作过于频繁，请稍后重试")
		return
	}
	select {
	case gateway.workers <- struct{}{}:
		defer func() { <-gateway.workers }()
	default:
		writeCNAdminError(w, 429, "服务繁忙，请稍后重试")
		return
	}
	// No query-string credentials, cookies or cross-origin browser form calls.
	if r.URL.RawQuery != "" || r.Header.Get("X-Kairisei-Account") != "1" {
		writeCNAdminError(w, 400, "账号请求无效")
		return
	}
	var payload struct {
		UUID     string `json:"uuid"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeCNAdminJSONLimit(r, &payload, 2048); err != nil {
		writeCNAdminError(w, 400, "账号请求格式无效")
		return
	}
	var result cnAccountBinding
	var err error
	switch r.URL.Path {
	case "/local/account/status":
		result, err = gateway.accounts.accountBinding(payload.UUID)
	case "/local/account/bind":
		result, err = gateway.accounts.bindAccount(payload.UUID, payload.Username, payload.Password)
	case "/local/account/login":
		result, err = gateway.accounts.loginAccount(payload.Username, payload.Password)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeCNAdminError(w, 400, err.Error())
		return
	}
	writeCNAdminJSON(w, 200, map[string]any{"state": "PASS", "account": result})
}
