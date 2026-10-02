package httpapi

import (
	"kairisei.local/server/internal/game"
)

func (h *accountBusinessHandler) ApplyRuntimeSettings(settings game.RuntimeSettings) {
	h.api.account.ApplyRuntimeSettings(settings)
}

func (h *accountBusinessHandler) ApplyPlayerConfiguration(config game.PlayerConfiguration) {
	h.api.account.ApplyPlayerConfiguration(config)
}

func (h *accountBusinessHandler) ApplyMissionConfiguration(config game.MissionConfiguration) {
	h.api.account.ApplyMissionConfiguration(config)
}

var _ game.MissionConfigurator = (*accountBusinessHandler)(nil)

func (h *accountBusinessHandler) ApplyGachaConfiguration(revision uint64, configs []game.GachaConfiguration) {
	h.api.account.ApplyGachaConfiguration(revision, configs)
}
