package admin

import (
	"crypto/hmac"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"kairisei.local/server/internal/game"
)

const maxNotices = 50

type noticeEntry struct {
	Enabled bool   `json:"enabled"`
	Pinned  bool   `json:"pinned"`
	Title   string `json:"title"`
	Body    string `json:"body"`
}

type noticePolicy struct {
	Enabled bool   `json:"enabled"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	// nil reads legacy single-notice documents without rewriting the database.
	// An explicit empty array means all entries were deleted; never revive Title/Body.
	Entries []noticeEntry `json:"entries"`
	// Server-owned publication version, independent of unrelated policy edits.
	PublicationRevision int `json:"publication_revision"`
}

func (n noticePolicy) publicContent() string {
	content := []string{}
	if n.Enabled {
		for _, entry := range n.entries() {
			if entry.Enabled {
				encoded, _ := json.Marshal([]string{entry.Title, entry.Body})
				content = append(content, string(encoded))
			}
		}
	}
	slices.Sort(content) // Reordering or pinning does not make the text unread.
	encoded, _ := json.Marshal(content)
	return string(encoded)
}

func (n noticePolicy) entries() []noticeEntry {
	if n.Entries != nil {
		return n.Entries
	}
	return []noticeEntry{{Enabled: true, Title: n.Title, Body: n.Body}}
}

func validateNoticePolicy(n noticePolicy) error {
	entries := n.entries()
	if len(entries) > maxNotices {
		return fmt.Errorf("公告最多保存%d条", maxNotices)
	}
	for i, entry := range entries {
		if strings.TrimSpace(entry.Title) == "" || len([]rune(entry.Title)) > 80 || len([]rune(entry.Body)) > 8000 {
			return fmt.Errorf("第%d条公告标题须为1至80字，正文最多8000字", i+1)
		}
		if n.Enabled && entry.Enabled && strings.TrimSpace(entry.Body) == "" {
			return fmt.Errorf("第%d条已上架公告的正文不能为空", i+1)
		}
	}
	return nil
}

//go:embed web/notices.html
var noticeHTML string

var noticeTemplate = template.Must(template.New("notice").Parse(noticeHTML))

// A valid token grants only marking one publication read, never account login.
// Old process tokens still render the public page without changing read state.
func (o *Operations) NoticeReadClaim(r *http.Request) (userID, popupID int, ok bool) {
	userID, _ = strconv.Atoi(r.URL.Query().Get("user"))
	version, _ := strconv.Atoi(r.URL.Query().Get("revision"))
	p := o.playerPolicy.Load()
	if userID <= 0 || version <= 0 || version > 1000000000 || p == nil || version > p.Runtime.Notice.Revision {
		return 0, 0, false
	}
	expected := game.NoticeReadToken(o.noticeSigningKey, userID, version)
	if !hmac.Equal([]byte(expected), []byte(r.URL.Query().Get("token"))) {
		return 0, 0, false
	}
	return userID, 1000000000 + version, true
}

func (o *Operations) LocalNotice(w http.ResponseWriter, _ *http.Request) {
	visible := []noticeEntry{}
	if p := o.playerPolicy.Load(); p != nil && p.Value.Notice.Enabled {
		for _, entry := range p.Value.Notice.entries() {
			if entry.Enabled {
				visible = append(visible, entry)
			}
		}
	}
	// Sort the response copy only. Equal priorities retain the operator's order.
	slices.SortStableFunc(visible, func(a, b noticeEntry) int {
		if a.Pinned == b.Pinned {
			return 0
		}
		if a.Pinned {
			return -1
		}
		return 1
	})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = noticeTemplate.Execute(w, visible)
}
