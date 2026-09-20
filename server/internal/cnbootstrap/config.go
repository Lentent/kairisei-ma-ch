package cnbootstrap

import (
	"log/slog"

	"kairisei.local/server/internal/multiplayer"
)

// Config groups deployment inputs by owner. Paths refer to the validated runtime
// resource set; creating a server does not rebuild or mutate those inputs.
type Config struct {
	Persistence PersistenceConfig
	Resources   ResourcesConfig
	Masters     MastersConfig
	Network     NetworkConfig
	Multiplayer *multiplayer.Hub
	CDN         CDNConfig
	Logger      *slog.Logger
}

type PersistenceConfig struct {
	RequestLog string
	SavePath   string
	SeedPath   string
}

type ResourcesConfig struct {
	AssetMap            string
	CPKRoot             string
	ImageRoot           string
	CPKAliases          string
	PatchRoots          []string
	GachaBanner         string
	FiveStarGachaBanner string
	HomeBanner          string
}

type MastersConfig struct {
	Cards             string
	Explore           string
	Story             string
	Battle            string
	Navi              string
	Items             string
	Avatar            string
	Stamps            string
	Honors            string
	PVP               string
	PlayerProgression string
	LoginBonus        string
}

type NetworkConfig struct {
	AdvertiseHost string
	HTTPPort      int
	BattleSV      multiplayer.Endpoint
}
