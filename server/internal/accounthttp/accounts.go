package accounthttp

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
)

const AccountUserHeader = "X-Kairisei-Local-User-ID"

type multiplayerBattleStarter interface {
	ChargeMultiplayerStart(multiplayer.BattleStart, func(gamestate.State) error) error
}

type multiplayerBattleContinuer interface {
	ChargeMultiplayerContinue(multiplayer.BattleContinue) (multiplayer.ContinueBalance, error)
}

const idleAccountCacheLimit = 32

// Read once at startup; inherited by both desktop and headless launchers.
func ConfiguredAccountCacheLimit() (int, error) {
	value := strings.TrimSpace(os.Getenv("KAIRI_ACCOUNT_CACHE_LIMIT"))
	if value == "" {
		return idleAccountCacheLimit, nil
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 0 || limit > 4096 {
		return 0, fmt.Errorf("KAIRI_ACCOUNT_CACHE_LIMIT must be an integer between 0 and 4096")
	}
	return limit, nil
}

const idleAccountCacheTTL = 5 * time.Minute

type handlerEntry struct {
	handler   http.Handler
	active    bool
	lastUsed  time.Time
	lastOrder uint64
	timer     *time.Timer
}

type Router struct {
	activity  map[int]PlayerActivity
	observed  [10]requestBucket
	mu        sync.Mutex
	handlers  map[int]*handlerEntry
	sequence  uint64
	idleLimit int
	// Stable bounded locks also serialize Admin and BattleSv against HTTP.
	locks         [256]sync.Mutex
	build         func(int) (http.Handler, error)
	prepare       func(http.Handler)
	persistStates func([]gamestate.State) error
}

type Config struct {
	Primary       http.Handler
	Build         func(int) (http.Handler, error)
	Prepare       func(http.Handler)
	IdleLimit     int
	PersistStates func([]gamestate.State) error
}

func New(config Config) *Router {
	router := &Router{handlers: make(map[int]*handlerEntry), build: config.Build, prepare: config.Prepare, idleLimit: config.IdleLimit, persistStates: config.PersistStates}
	if config.Primary != nil {
		entry := &handlerEntry{handler: config.Primary, active: true}
		router.handlers[accountstore.PrimaryUserID] = entry
		router.releaseHandler(accountstore.PrimaryUserID, entry, false)
	}
	return router
}

func (router *Router) AccountLock(userID int) *sync.Mutex {
	return &router.locks[uint(userID)%uint(len(router.locks))]
}

// Caller holds router.mu. Active requests retain their local handler until completion.
func (router *Router) removeHandlerLocked(userID int) {
	if entry := router.handlers[userID]; entry != nil && entry.timer != nil {
		entry.timer.Stop()
	}
	delete(router.handlers, userID)
}

func (router *Router) Invalidate(userID int) {
	router.mu.Lock()
	router.removeHandlerLocked(userID)
	router.mu.Unlock()
}

// Caller holds the stable account lock throughout acquire, use, and release.
func (router *Router) acquireHandler(userID int) (*handlerEntry, error) {
	router.mu.Lock()
	entry := router.handlers[userID]
	if entry != nil {
		entry.active = true
		if entry.timer != nil {
			entry.timer.Stop()
		}
	}
	router.mu.Unlock()
	if entry != nil {
		return entry, nil
	}
	handler, err := router.build(userID)
	if err != nil {
		return nil, err
	}
	entry = &handlerEntry{handler: handler, active: true}
	router.mu.Lock()
	router.handlers[userID] = entry
	router.mu.Unlock()
	return entry, nil
}

func (router *Router) releaseHandler(userID int, entry *handlerEntry, discard bool) {
	router.mu.Lock()
	defer router.mu.Unlock()
	if router.handlers[userID] != entry {
		return
	}
	if discard {
		router.removeHandlerLocked(userID)
		return
	}
	entry.active = false
	entry.lastUsed = time.Now()
	router.sequence++
	entry.lastOrder = router.sequence
	// The callback checks both identity and the deadline: a timer racing with
	// a new request cannot evict that request or its refreshed cache entry.
	entry.timer = time.AfterFunc(idleAccountCacheTTL, func() {
		router.expireHandler(userID, entry)
	})
	for {
		idle, oldestID := 0, 0
		var oldest uint64
		for id, candidate := range router.handlers {
			if candidate.active {
				continue
			}
			idle++
			if oldest == 0 || candidate.lastOrder < oldest {
				oldest, oldestID = candidate.lastOrder, id
			}
		}
		if idle <= router.idleLimit {
			break
		}
		router.removeHandlerLocked(oldestID)
	}
}

func (router *Router) expireHandler(userID int, entry *handlerEntry) {
	router.mu.Lock()
	defer router.mu.Unlock()
	if router.handlers[userID] == entry && !entry.active && time.Since(entry.lastUsed) >= idleAccountCacheTTL {
		router.removeHandlerLocked(userID)
	}
}

func (router *Router) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	started := time.Now()
	userID := accountstore.PrimaryUserID
	if text := request.Header.Get(AccountUserHeader); text != "" {
		parsed, err := strconv.Atoi(text)
		if err != nil || parsed < accountstore.PrimaryUserID {
			http.Error(writer, "invalid CN local account context", http.StatusUnauthorized)
			return
		}
		userID = parsed
	}
	accountLock := router.AccountLock(userID)
	accountLock.Lock()
	defer accountLock.Unlock()

	entry, err := router.acquireHandler(userID)
	if err != nil {
		http.Error(writer, "load CN local account", http.StatusInternalServerError)
		return
	}
	handler := entry.handler
	// Handlers mutate an account-local cache before committing its snapshot.
	// An unsuccessful request must never leave that cache available to the next
	// request: reload the last durable state while retaining this account lock.
	response := &responseWriter{ResponseWriter: writer}
	completed := false
	defer func() {
		solo := false
		if completed && response.status < http.StatusBadRequest {
			if source, ok := handler.(interface{ HasUnsettledSoloBattle() bool }); ok {
				solo = source.HasUnsettledSoloBattle()
			}
		}
		router.recordActivity(userID, started, !completed || response.status >= http.StatusBadRequest, solo)
		router.releaseHandler(userID, entry, !completed || response.status >= http.StatusBadRequest)
	}()
	if router.prepare != nil {
		router.prepare(handler)
	}
	handler.ServeHTTP(response, request)
	completed = true
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (writer *responseWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

func (writer *responseWriter) WriteHeader(status int) {
	if writer.status != 0 {
		return
	}
	if status >= 200 {
		writer.status = status
	}
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *responseWriter) Write(body []byte) (int, error) {
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	return writer.ResponseWriter.Write(body)
}

// BattleSv calls without registry/room locks. All participating account stripes
// stay locked until the single database commit, just as for HTTP/Admin writes.
func (router *Router) ChargeMultiplayerStart(start multiplayer.BattleStart) error {
	users := append([]int{start.OwnerUserID}, start.GuestUserIDs...)
	if len(users) > 4 || router.persistStates == nil {
		return errors.New("invalid multiplayer start transaction")
	}
	stripes := make([]int, 0, len(users))
	for i, userID := range users {
		if userID < accountstore.PrimaryUserID || userID >= accountstore.SystemPartnerUserIDBase || slices.Contains(users[:i], userID) {
			return errors.New("invalid multiplayer start account")
		}
		stripes = append(stripes, int(uint(userID)%uint(len(router.locks))))
	}
	// Sort and deduplicate lock indices, not user IDs: distinct accounts can
	// share a stripe, and opposite user orders can otherwise deadlock.
	slices.Sort(stripes)
	stripes = slices.Compact(stripes)
	for _, index := range stripes {
		router.locks[index].Lock()
	}
	defer func() {
		for i := len(stripes) - 1; i >= 0; i-- {
			router.locks[stripes[i]].Unlock()
		}
	}()
	entries := make([]*handlerEntry, 0, len(users))
	committed := false
	defer func() {
		// An uncommitted in-memory debit is discarded for every participant.
		// Their next request reloads the unchanged durable snapshot.
		for i, entry := range entries {
			router.releaseHandler(users[i], entry, !committed)
		}
	}()
	states := make([]gamestate.State, 0, len(users))
	for _, userID := range users {
		entry, err := router.acquireHandler(userID)
		if err != nil {
			return err
		}
		entries = append(entries, entry)
		starter, ok := entry.handler.(multiplayerBattleStarter)
		if !ok {
			return errors.New("CN account handler has no multiplayer start transaction")
		}
		if err := starter.ChargeMultiplayerStart(start, func(next gamestate.State) error {
			states = append(states, next)
			return nil
		}); err != nil {
			return err
		}
	}
	if len(states) > 0 {
		if err := router.persistStates(states); err != nil {
			return err
		}
	}
	committed = true
	return nil
}

// Called outside hub.mu, under the same per-account transaction lock as HTTP.
func (router *Router) ChargeMultiplayerContinue(request multiplayer.BattleContinue) (multiplayer.ContinueBalance, error) {
	lock := router.AccountLock(request.UserID)
	lock.Lock()
	defer lock.Unlock()
	entry, err := router.acquireHandler(request.UserID)
	if err != nil {
		return multiplayer.ContinueBalance{}, err
	}
	completed := false
	defer func() { router.releaseHandler(request.UserID, entry, !completed) }()
	handler := entry.handler
	starter, ok := handler.(multiplayerBattleContinuer)
	if !ok {
		return multiplayer.ContinueBalance{}, errors.New("CN account handler has no multiplayer continue transaction")
	}
	balance, err := starter.ChargeMultiplayerContinue(request)
	completed = err == nil
	return balance, err
}
