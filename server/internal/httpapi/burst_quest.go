package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"kairisei.local/server/internal/game"
)

func (a *API) storyTeamBattleStart(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		StoryID int `json:"story_teambattleid"`
	}
	if err := decodeExact(request, []string{"story_teambattleid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if err := a.account.BeginBurstStory(payload.StoryID); err != nil {
		a.writeProtocolResult(writer, map[string]any{}, -6800, "请先完成训练及前一段圣剑解放剧情。")
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"seed": 0, "hp": 68000, "hp_max": 68000,
		"cost_initial": 3, "burst_gauge_initial": 0, "hold_max": 5,
	})
}

func (a *API) storyTeamBattleEnd(writer http.ResponseWriter, _ *http.Request) {
	if cached := a.account.BurstStoryResponse(); len(cached) > 0 {
		a.writeProtocol(writer, cached)
		return
	}
	result, err := a.account.EndBurstStory()
	if err != nil {
		storyID := a.account.BurstStoryID()
		a.logger.Warn("reject sword-release story settlement", "story_id", storyID, "error", err)
		message := "学习剧情奖励结算失败，请稍后重试。"
		if errors.Is(err, game.ErrNoActiveBurstStory) {
			message = "学习剧情记录已失效，请重新进入本段剧情。"
		}
		a.writeProtocolResult(writer, map[string]any{}, -6800, message)
		return
	}
	payload := a.storyEndPayload(result.Clear)
	payload["burst_unlock"] = []any{}
	if result.Arthur != 0 {
		payload["burst_unlock"] = []any{map[string]any{
			"arthur_type": result.Arthur,
			"rewards":     battleResultRewardsWire(result.Unlock.Rewards),
			"new_buddys":  toWireBuddies(result.Unlock.Buddies),
			"decks":       toWireDecks(result.Decks),
		}}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	a.account.SetBurstStoryResponse(encoded)
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, payload)
}
