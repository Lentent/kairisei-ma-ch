package cnbootstrap

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/accounthttp"
	"kairisei.local/server/internal/accountstore"
	adminapi "kairisei.local/server/internal/admin"
	"kairisei.local/server/internal/gamestate"
)

// application wires long-lived services. Protocol handlers never construct
// databases or reload resource masters while registering their routes.
type application struct {
	config      Config
	resources   *clientResources
	accounts    *accountstore.Accounts
	operations  *adminapi.Operations
	business    *accounthttp.Router
	recorder    *recorder
	progression gamestate.PlayerProgressionPolicy
}

func (app *application) router() chi.Router {
	router := chi.NewRouter()
	router.Use(cnNetworkCompression(app.config.Logger))
	router.Use(app.recorder.middleware(app.config.Logger))
	router.Use(normalizeLeadingSlashes)
	router.Use(authenticateCNSessions(app.accounts))
	router.Get("/healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"state":          "PASS",
			"client_profile": "cn602-bootstrap",
			"battle_rooms":   app.config.Multiplayer.RoomCount(),
			"battle_port":    app.config.Network.BattleSV.Port,
		})
	})
	for _, name := range []string{"default", "apple-review", "qa"} {
		router.Get("/local/server/"+name+".list", cnBootstrapServerList(app.config.Network.AdvertiseHost, app.config.Network.HTTPPort))
	}
	app.resources.BannerAssets.register(router)
	emptyJSON := func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.Header().Set("Cache-Control", "no-store")
		_, _ = writer.Write([]byte("{}"))
	}
	emptyBody := func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writer.Header().Set("Cache-Control", "no-store")
		writer.WriteHeader(http.StatusOK)
	}
	router.Get("/xxxx/g0/apk.config", emptyJSON)
	router.Get("/xxxx/g0/dex.config", emptyJSON)
	router.Get("/xxxx/feature/*", emptyBody)
	router.Get("/xxxx/html/latest_default.json", emptyBody)
	router.Post("/disabled/envsdk/optsdk/*", emptyJSON)
	// Fixed-width DEX redirects for longer RFC1918 hosts omit the emulator-only
	// prefixes above. Keep both representations at the HTTP boundary.
	router.Get("/g0/apk.config", emptyJSON)
	router.Get("/g0/dex.config", emptyJSON)
	router.Get("/feature/*", emptyBody)
	router.Get("/html/latest_default.json", emptyBody)
	router.Post("/d/*", emptyJSON)
	router.Post("/log.php", acknowledgeClientLog)
	router.Post("/mods_switch.php", cnBootstrapModuleSwitches)
	router.Post("/loginSDK.php", cnBootstrapLogin(app.accounts, app.config.Network.AdvertiseHost, app.config.Network.HTTPPort, app.config.CDN, resourceVersionNamespace(app.resources.Catalog, app.resources.CPKList, app.resources.CPKState), app.resources.Images.Namespace))
	accountGateway := newCNAccountGateway(app.accounts)
	for _, action := range []string{"status", "bind", "login"} {
		router.Post("/local/account/"+action, accountGateway.ServeHTTP)
	}
	router.Post("/subcribe_push.php", cnBootstrapPushRegistration)
	app.registerGameRoutes(router)
	router.Get("/disabled/products", cnBootstrapProducts)
	router.Get("/disabled/web", app.operations.LocalNotice)
	router.Get("/disabled/web/auto", app.autoNotice)
	router.Get("/disabled/web/deck-guide", cnBootstrapDeckGuide)
	router.Get("/disabled/web/information/2015/7/kechengbiao", cnBootstrapDungeonSchedule)
	introHandler, err := newCNIntroHandler(app.resources.GachaBanner)
	if err != nil {
		app.config.Logger.Warn("local download guide unavailable", "error", err)
		introHandler = cnBootstrapIntroPlaceholder
	}
	router.Get("/disabled/web/netease/fourplusone/20160626/Intro_{index}.png", introHandler)
	registerResourceVersions(router, app.resources.PatchRoots, app.resources.Catalog, app.resources.CPKList, app.resources.CPKState)
	versionedPatch := cnBootstrapVersionedPatchFile(app.resources.PatchRoots, app.resources.Patch)
	router.Get("/local/resources/patch/Android/patch/*", versionedPatch)
	router.Head("/local/resources/patch/Android/patch/*", versionedPatch)
	cpkResource := cnBootstrapCPKResource(app.resources.CPK)
	router.Get("/local/resources/cpk/*", cpkResource)
	router.Head("/local/resources/cpk/*", cpkResource)
	router.Get("/local/resources/image/*", app.resources.Images.serveHTTP)
	router.Head("/local/resources/image/*", app.resources.Images.serveHTTP)
	router.NotFound(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "CN route adapter not implemented", http.StatusNotImplemented)
	})
	return router
}

func (app *application) handler() (http.Handler, error) {
	adminHandler, err := adminapi.New(adminapi.Config{
		Accounts: app.accounts, Runtime: app.business, Operations: app.operations,
		BattleMaster: app.config.Masters.Battle, CardMaster: app.config.Masters.Cards, ItemMaster: app.config.Masters.Items,
		Multiplayer: app.config.Multiplayer, AdvertiseHost: app.config.Network.AdvertiseHost, GamePort: app.config.Network.HTTPPort,
		Logger: app.config.Logger, Progression: app.progression,
		GachaBanners: app.resources.GachaBanners, AssetMaps: []string{app.config.Resources.AssetMap},
	})
	if err != nil {
		return nil, fmt.Errorf("initialize CN admin handler: %w", err)
	}
	return &cnDeploymentHandler{Handler: app.router(), admin: adminHandler, database: app.accounts.Database()}, nil
}
