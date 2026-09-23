package game

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"kairisei.local/server/internal/gamestate"
)

const noticePopupIDBase = 1000000000

// Home.CheckNotice appends update_info_url to Settings.info_url. The signed
// relative URL grants only acknowledgement of this account/publication.
func (s *Account) UnreadNoticePath(userID int) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := s.noticePublication
	if !n.Enabled || n.Revision <= 0 || n.Revision > 1000000000 {
		return ""
	}
	id := noticePopupIDBase + n.Revision
	if _, read := s.popupReadIDs[id]; read {
		return ""
	}
	return fmt.Sprintf("auto?user=%d&revision=%d&token=%s", userID, n.Revision, NoticeReadToken(n.SigningKey, userID, n.Revision))
}

func NoticeReadToken(key [32]byte, userID, revision int) string {
	mac := hmac.New(sha256.New, key[:])
	fmt.Fprintf(mac, "notice-read:%d:%d", userID, revision)
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Account) UnreadPopupState(profile gamestate.PopupProfile) []gamestate.PopupProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if profile.PopupID <= 0 {
		return []gamestate.PopupProfile{}
	}
	if _, read := s.popupReadIDs[profile.PopupID]; read {
		return []gamestate.PopupProfile{}
	}
	return []gamestate.PopupProfile{profile}
}

func (s *Account) AcknowledgePopup(profile gamestate.PopupProfile, popupID int, isSystem int8) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if popupID > noticePopupIDBase && popupID <= noticePopupIDBase+1000000000 && isSystem == 1 {
		version := popupID - noticePopupIDBase
		if version <= s.noticePublication.Revision {
			// Closing an older publication while an admin publishes a newer one
			// must succeed, but must not mark the newer publication as read.
			if version == s.noticePublication.Revision {
				if s.popupReadIDs == nil {
					s.popupReadIDs = map[int]struct{}{}
				}
				for id := range s.popupReadIDs {
					if id > noticePopupIDBase && id <= noticePopupIDBase+1000000000 {
						delete(s.popupReadIDs, id)
					}
				}
				s.popupReadIDs[popupID] = struct{}{}
			}
			return nil
		}
	}
	if profile.PopupID <= 0 || popupID != profile.PopupID || isSystem != profile.IsSystem {
		return errors.New("popup acknowledgement does not match the published local profile")
	}
	if s.popupReadIDs == nil {
		s.popupReadIDs = map[int]struct{}{}
	}
	s.popupReadIDs[popupID] = struct{}{}
	return nil
}
