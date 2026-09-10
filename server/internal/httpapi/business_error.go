package httpapi

import (
	"errors"
	"net/http"

	"kairisei.local/server/internal/release"
)

// Codes from the original CN SV_ERR enum. Keep expected balance failures out
// of HTTP transport errors, which the original client treats as disconnection.
type businessError struct {
	code    int
	message string
}

func (e *businessError) Error() string { return e.message }

var (
	errInsufficientCrystals     = &businessError{-1060, "水晶不足，请补充后再试。"}
	errInsufficientPaidCrystals = &businessError{-1060, "付费水晶不足，请补充后再试。"}
	errInsufficientGold         = &businessError{-1030, "金币不足。"}
	errInsufficientMaterials    = &businessError{-1200, "素材或道具不足，请重新选择。"}
	errCardUnavailable          = &businessError{-1, "所选卡牌已不存在，请刷新后重新选择。"}
	errCardInDeck               = &businessError{-1, "所选卡牌正在卡组中使用，请先从卡组中移除。"}
	errCardLocked               = &businessError{-1, "所选卡牌已锁定，请先解锁。"}
	errSphereUnavailable        = &businessError{-1, "所选秘石已不存在，请刷新后重新选择。"}
	errSphereLocked             = &businessError{-1, "所选秘石已锁定，请先解锁。"}
	errBuddyUnavailable         = &businessError{-1, "所选传承卡已不存在，请刷新后重新选择。"}
	errBuddyLocked              = &businessError{-1, "所选传承卡已锁定，请先解锁。"}
	errInsufficientFriendPoints = &businessError{-1, "友情点不足。"}
	errInsufficientStive        = &businessError{-1, "名声训练点数不足。"}
	errItemExpired              = &businessError{-1201, "道具已过期。"}
	errCardCapacity             = &businessError{-2900, "卡牌容量不足，请整理卡牌后再试。"}
	errSphereCapacity           = &businessError{-2902, "圣剑容量不足，请整理后再试。"}
	errBuddyCapacity            = &businessError{-2903, "传承卡容量不足，请整理后再试。"}
	errItemCapacity             = &businessError{-1, "道具持有数量已达上限。"}
	errGachaUnavailable         = &businessError{-3100, "这个扭蛋暂不可用，请重新选择。"}
	errCrystalCapacity          = &businessError{-1061, "水晶持有数量已达上限。"}
)

func (a *API) writeStoreError(writer http.ResponseWriter, err error) {
	if !a.writeBusinessError(writer, err) {
		writeError(writer, http.StatusBadRequest, err.Error())
	}
}

// Returns false for transport, persistence and malformed-input failures. They
// retain their original status at the caller rather than becoming success.
func (a *API) writeBusinessError(writer http.ResponseWriter, err error) bool {
	var expected *businessError
	if !errors.As(err, &expected) {
		return false
	}
	// Original ProtoMgr.onCommon only displays errors for actions 1 (title)
	// and 2 (home). Action 0 silently invokes the endpoint's default callback;
	// in BuyNavi that callback even treats default res=0 as a navigator ID.
	// Use the supported home return to refresh the page without losing login.
	a.writeProtocolResponse(writer, map[string]any{}, []release.PopupProfile{}, expected.code, expected.message, 2)
	return true
}
