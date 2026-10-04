package admin

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"slices"
	"strings"
	"time"

	"kairisei.local/server/internal/accountstore"
)

const dungeonScheduleKey = "dungeon-schedule"

type dungeonScheduleOverride struct {
	Catalog string `json:"catalog"`
	GroupID int    `json:"group_id"`
	Name    string `json:"name"`
	Note    string `json:"note"`
}

// Only presentation text is editable here. Opening dates and membership always
// come from the separately published boss policy, including in draft previews.
type dungeonScheduleConfig struct {
	Title       string                    `json:"title"`
	Description string                    `json:"description"`
	Footer      string                    `json:"footer"`
	Entries     []dungeonScheduleOverride `json:"entries"`
}

type dungeonScheduleEntry struct {
	Catalog       string   `json:"catalog"`
	GroupID       int      `json:"group_id"`
	Name          string   `json:"name"`
	DisplayName   string   `json:"display_name"`
	Note          string   `json:"note"`
	Category      string   `json:"category"`
	CategoryLabel string   `json:"category_label"`
	Difficulties  []string `json:"difficulties"`
	Schedule      string   `json:"schedule"`
	Status        string   `json:"status"`
	StatusCode    string   `json:"status_code"`
	Published     bool     `json:"published"`
	StartUnix     int64    `json:"start_unix"`
	EndUnix       int64    `json:"end_unix"`
	Weekdays      []int    `json:"weekdays"`
}

type dungeonScheduleSection struct {
	Title   string
	Entries []dungeonScheduleEntry
}

type dungeonScheduleView struct {
	Config   dungeonScheduleConfig
	Sections []dungeonScheduleSection
	Updated  string
	Count    int
	Open     int
}

//go:embed web/dungeon_schedule.html
var dungeonScheduleHTML string

var dungeonScheduleTemplate = template.Must(template.New("dungeon-schedule").Parse(dungeonScheduleHTML))

var dungeonScheduleLimits = map[string]int{
	"title": 80, "description": 4000, "footer": 4000,
	"entry_name": 100, "entry_note": 1000, "entries": 2000,
}

func defaultDungeonScheduleConfig() dungeonScheduleConfig {
	return dungeonScheduleConfig{
		Title:       "副本日程表",
		Description: "以下副本与服务器已发布的 Boss 目录同步，开放日期及每周开放日以当前发布设置为准。所有时间均为北京时间（UTC+8）。",
		Footer:      "普通副本按账号通关进度解锁；副本难度及进入条件以游戏内显示为准。",
		Entries:     []dungeonScheduleOverride{},
	}
}

func (o *Operations) readDungeonScheduleConfig() (dungeonScheduleConfig, int, error) {
	doc, err := o.storage.ReadDocument(dungeonScheduleKey)
	if err != nil {
		return dungeonScheduleConfig{}, 0, err
	}
	config := defaultDungeonScheduleConfig()
	if doc.Revision > 0 {
		if err := json.Unmarshal(doc.Payload, &config); err != nil {
			return dungeonScheduleConfig{}, 0, fmt.Errorf("读取副本日程表：%w", err)
		}
	}
	if config.Entries == nil {
		config.Entries = []dungeonScheduleOverride{}
	}
	return config, doc.Revision, nil
}

func (o *Operations) normalizeDungeonScheduleConfig(config *dungeonScheduleConfig) error {
	config.Title = strings.TrimSpace(config.Title)
	config.Description = strings.TrimSpace(config.Description)
	config.Footer = strings.TrimSpace(config.Footer)
	if config.Title == "" || len([]rune(config.Title)) > dungeonScheduleLimits["title"] {
		return errors.New("日程表标题须为1至80字")
	}
	if len([]rune(config.Description)) > dungeonScheduleLimits["description"] || len([]rune(config.Footer)) > dungeonScheduleLimits["footer"] {
		return errors.New("日程表说明及页尾各最多4000字")
	}
	if config.Entries == nil || len(config.Entries) > dungeonScheduleLimits["entries"] {
		return errors.New("请提交完整文字覆盖清单，最多2000项；清空请提交空数组")
	}
	seen := map[string]bool{}
	entries := make([]dungeonScheduleOverride, 0, len(config.Entries))
	for _, entry := range config.Entries {
		entry.Name, entry.Note = strings.TrimSpace(entry.Name), strings.TrimSpace(entry.Note)
		if entry.Catalog != "activity" && entry.Catalog != "past" {
			return errors.New("日程表条目目录须为activity或past")
		}
		key := dungeonScheduleEntryKey(entry.Catalog, entry.GroupID)
		if entry.GroupID <= 0 || seen[key] {
			return errors.New("日程表包含无效或重复的副本组")
		}
		seen[key] = true
		known := slices.ContainsFunc(o.dungeonScheduleGroups[entry.Catalog], func(g AdminBattleGroup) bool { return g.GroupID == entry.GroupID })
		if !known {
			return fmt.Errorf("日程表条目引用未知副本组 %s/%d", entry.Catalog, entry.GroupID)
		}
		if len([]rune(entry.Name)) > dungeonScheduleLimits["entry_name"] || len([]rune(entry.Note)) > dungeonScheduleLimits["entry_note"] {
			return errors.New("副本展示名称最多100字，备注最多1000字")
		}
		if entry.Name != "" || entry.Note != "" {
			entries = append(entries, entry)
		}
	}
	config.Entries = entries
	return nil
}

func dungeonScheduleEntryKey(catalog string, id int) string {
	return fmt.Sprintf("%s/%d", catalog, id)
}

func (o *Operations) dungeonScheduleEntries(config dungeonScheduleConfig, now time.Time) ([]dungeonScheduleEntry, error) {
	overrides := make(map[string]dungeonScheduleOverride, len(config.Entries))
	for _, entry := range config.Entries {
		overrides[dungeonScheduleEntryKey(entry.Catalog, entry.GroupID)] = entry
	}
	entries := []dungeonScheduleEntry{}
	for _, catalog := range []string{"activity", "past"} {
		key := teamBattlePublicationKey
		if catalog == "past" {
			key = PastBattlePublicationKey
		}
		doc, err := o.storage.ReadDocument(key)
		if err != nil {
			return nil, err
		}
		policy := TeamBattlePublication{Mode: "all"}
		if doc.Revision != 0 {
			if err := json.Unmarshal(doc.Payload, &policy); err != nil {
				return nil, fmt.Errorf("读取副本发布设置：%w", err)
			}
		}
		if policy.Mode != "all" && policy.Mode != "allowlist" {
			return nil, errors.New("副本发布模式无效")
		}
		groupSchedules := map[int]BattleGroupSchedule{}
		for _, schedule := range policy.GroupSchedules {
			groupSchedules[schedule.GroupID] = schedule
		}
		for _, group := range o.dungeonScheduleGroups[catalog] {
			override, edited := overrides[dungeonScheduleEntryKey(catalog, group.GroupID)]
			published := policy.Mode == "all" || slices.Contains(policy.GroupIDs, group.GroupID)
			if !published && !edited {
				continue
			}
			entry := dungeonScheduleEntry{
				Catalog: catalog, GroupID: group.GroupID, Name: group.Name, DisplayName: group.Name,
				Note: override.Note, Category: group.Category, Published: published,
				Difficulties: append([]string{}, group.Difficulties...),
			}
			if override.Name != "" {
				entry.DisplayName = override.Name
			}
			entry.CategoryLabel = map[string]string{"2d": "2D Boss", "3d": "3D Boss", "material": "素材副本"}[entry.Category]
			if entry.CategoryLabel == "" {
				entry.CategoryLabel = "Boss 副本"
			}
			entry.StartUnix, entry.EndUnix, entry.Weekdays = dungeonScheduleWindow(policy, groupSchedules[group.GroupID])
			entry.Schedule = dungeonScheduleWindowText(entry.StartUnix, entry.EndUnix, entry.Weekdays)
			entry.Status, entry.StatusCode = dungeonScheduleStatus(entry.StartUnix, entry.EndUnix, entry.Weekdays, now)
			if !published {
				entry.Status, entry.StatusCode = "未发布", "unpublished"
			}
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

// Directory and per-group dates are simultaneous constraints, so the effective
// window is their intersection. An empty weekday list means every day.
func dungeonScheduleWindow(policy TeamBattlePublication, group BattleGroupSchedule) (int64, int64, []int) {
	start := max(policy.StartUnix, group.StartUnix)
	end := policy.EndUnix
	if end == 0 || group.EndUnix != 0 && group.EndUnix < end {
		end = group.EndUnix
	}
	return start, end, append([]int{}, group.Weekdays...)
}

func dungeonScheduleWindowText(start, end int64, weekdays []int) string {
	days := "每天"
	if len(weekdays) > 0 && len(weekdays) < 7 {
		names := []string{"周一", "周二", "周三", "周四", "周五", "周六", "周日"}
		selected := []string{}
		for _, day := range weekdays {
			if day >= 1 && day <= 7 {
				selected = append(selected, names[day-1])
			}
		}
		days = strings.Join(selected, "、")
	}
	if start == 0 && end == 0 {
		return days + " · 全天开放"
	}
	format := func(unix int64) string { return time.Unix(unix, 0).In(battleScheduleZone).Format("2006-01-02 15:04") }
	window := "不限开始"
	if start > 0 {
		window = format(start) + " 起"
	}
	if end > 0 {
		window += " ～ " + format(end) + " 止（不含结束时刻）"
	} else {
		window += " · 不限结束"
	}
	return window + " · " + days
}

func dungeonScheduleStatus(start, end int64, weekdays []int, now time.Time) (string, string) {
	if end != 0 && end <= start {
		return "排期无交集", "closed"
	}
	if now.Unix() < start {
		return "未开始", "upcoming"
	}
	if end != 0 && now.Unix() >= end {
		return "已结束", "closed"
	}
	day := (int(now.In(battleScheduleZone).Weekday())+6)%7 + 1
	if len(weekdays) > 0 && !slices.Contains(weekdays, day) {
		return "本日关闭", "closed"
	}
	return "开放中", "open"
}

func renderDungeonSchedule(config dungeonScheduleConfig, entries []dungeonScheduleEntry, now time.Time) (string, error) {
	view := dungeonScheduleView{Config: config, Updated: now.In(battleScheduleZone).Format("2006-01-02 15:04")}
	for _, catalog := range []string{"activity", "past"} {
		section := dungeonScheduleSection{Title: "活动 / 素材副本", Entries: []dungeonScheduleEntry{}}
		if catalog == "past" {
			section.Title = "往期 Boss"
		}
		for _, entry := range entries {
			if entry.Catalog == catalog && entry.Published {
				section.Entries = append(section.Entries, entry)
				view.Count++
				if entry.StatusCode == "open" {
					view.Open++
				}
			}
		}
		if len(section.Entries) > 0 {
			view.Sections = append(view.Sections, section)
		}
	}
	var out bytes.Buffer
	if err := dungeonScheduleTemplate.Execute(&out, view); err != nil {
		return "", err
	}
	return out.String(), nil
}

func (o *Operations) dungeonScheduleResponse(config dungeonScheduleConfig, revision int) (map[string]any, error) {
	now := time.Now()
	entries, err := o.dungeonScheduleEntries(config, now)
	if err != nil {
		return nil, err
	}
	preview, err := renderDungeonSchedule(config, entries, now)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"state": "OK", "revision": revision, "config": config,
		"defaults": defaultDungeonScheduleConfig(), "entries": entries,
		"preview_html": preview, "limits": dungeonScheduleLimits,
	}, nil
}

func (a *API) dungeonSchedule(w http.ResponseWriter, _ *http.Request) {
	config, revision, err := a.operations.readDungeonScheduleConfig()
	if err != nil {
		WriteAdminError(w, 500, err.Error())
		return
	}
	response, err := a.operations.dungeonScheduleResponse(config, revision)
	if err != nil {
		WriteAdminError(w, 500, err.Error())
		return
	}
	WriteAdminJSON(w, 200, response)
}

func (a *API) saveDungeonSchedule(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		Expected *int                   `json:"expected_revision"`
		Config   *dungeonScheduleConfig `json:"config"`
	}
	if err := DecodeAdminJSONLimit(r, &body, 16*1024*1024); err != nil || body.Expected == nil || body.Config == nil {
		WriteAdminError(w, 400, "请提交完整日程表内容和页面版本")
		return
	}
	if err := a.operations.normalizeDungeonScheduleConfig(body.Config); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	// Resolve and render before persisting, so a failed preview never saves a
	// document that the operator cannot see on this page.
	response, err := a.operations.dungeonScheduleResponse(*body.Config, *body.Expected+1)
	if err != nil {
		WriteAdminError(w, 500, err.Error())
		return
	}
	if _, err := a.operations.writeDocument(dungeonScheduleKey, *body.Expected, *body.Config); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, accountstore.ErrDocumentConflict) {
			status = http.StatusConflict
		}
		WriteAdminError(w, status, err.Error())
		return
	}
	WriteAdminJSON(w, 200, response)
}

func (a *API) previewDungeonSchedule(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Config *dungeonScheduleConfig `json:"config"`
	}
	if err := DecodeAdminJSONLimit(r, &body, 16*1024*1024); err != nil || body.Config == nil {
		WriteAdminError(w, 400, "请提交完整日程表预览内容")
		return
	}
	if err := a.operations.normalizeDungeonScheduleConfig(body.Config); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	response, err := a.operations.dungeonScheduleResponse(*body.Config, 0)
	if err != nil {
		WriteAdminError(w, 500, err.Error())
		return
	}
	WriteAdminJSON(w, 200, response)
}

// LocalDungeonSchedule is a server-rendered public page for the client's older
// WebView. It never exposes mutation APIs or relies on JavaScript.
func (o *Operations) LocalDungeonSchedule(w http.ResponseWriter, _ *http.Request) {
	config, _, err := o.readDungeonScheduleConfig()
	if err != nil {
		http.Error(w, "副本日程表暂时无法读取", 500)
		return
	}
	now := time.Now()
	entries, err := o.dungeonScheduleEntries(config, now)
	if err != nil {
		http.Error(w, "副本发布设置暂时无法读取", 500)
		return
	}
	page, err := renderDungeonSchedule(config, entries, now)
	if err != nil {
		http.Error(w, "副本日程表暂时无法显示", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(page))
}
