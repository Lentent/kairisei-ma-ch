package httpapi

import (
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
)

type API struct {
	initialState gamestate.State
	account      *game.Account
	baseURL      string
	bannerPaths  map[string]string

	logger              *slog.Logger
	persistState        game.StatePersister
	multiplayer         *multiplayer.Hub
	battleSV            multiplayer.Endpoint
	pvpAccounts         game.PVPAccountRepository
	pvpConfig           game.PVPConfig
	friendPointAccounts game.FriendPointAccountRepository
	teamBattleResultMu  sync.Mutex
	clientResultMu      sync.Mutex
}

// Config is the dependency boundary for one account's business handler.
// Tests may omit persistence and multiplayer dependencies for isolated account operations.
type Config struct {
	InitialState        gamestate.State
	BaseURL             string
	BannerPaths         map[string]string
	Logger              *slog.Logger
	PersistState        game.StatePersister
	Multiplayer         *multiplayer.Hub
	BattleSV            multiplayer.Endpoint
	PVPAccounts         game.PVPAccountRepository
	PVP                 game.PVPConfig
	FriendPointAccounts game.FriendPointAccountRepository
}

func New(config Config) (http.Handler, error) {
	if config.InitialState.User.UserID <= 0 {
		return nil, errors.New("initial account state is required")
	}
	baseURL := strings.TrimRight(config.BaseURL, "/")
	if baseURL == "" {
		return nil, errors.New("advertised base URL is required")
	}
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}
	cardStore, err := game.New(config.InitialState)
	if err != nil {
		return nil, err
	}
	api := &API{
		initialState:        config.InitialState,
		account:             cardStore,
		baseURL:             baseURL,
		bannerPaths:         maps.Clone(config.BannerPaths),
		logger:              logger,
		persistState:        config.PersistState,
		multiplayer:         config.Multiplayer,
		battleSV:            config.BattleSV,
		pvpAccounts:         config.PVPAccounts,
		pvpConfig:           config.PVP,
		friendPointAccounts: config.FriendPointAccounts,
	}
	if cardStore.RequiresInitialSave() && config.PersistState != nil {
		if err := config.PersistState(cardStore.Snapshot(config.InitialState)); err != nil {
			return nil, fmt.Errorf("persist reconciled initial state: %w", err)
		}
	}
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.Recoverer)
	router.Use(api.logRequest)
	api.register(router)
	return &accountBusinessHandler{Handler: router, api: api}, nil
}
