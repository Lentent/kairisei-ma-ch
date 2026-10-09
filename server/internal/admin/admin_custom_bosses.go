package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

const customBossKey = "custom-bosses"
const customBossFirstID = 890000001
const customBossLastID = 890001000
const customBossGroupOffset = -100000000

// SourceGroup, Replay and Reward freeze the original encounter. Editing the
// copy changes only its public identity and per-wave parameters, never masters.
type customBoss struct {
	BossID       int                               `json:"boss_id"`
	GroupID      int                               `json:"group_id"`
	SourceBossID int                               `json:"source_boss_id"`
	Name         string                            `json:"name"`
	Difficulty   string                            `json:"difficulty"`
	BPUse        int                               `json:"bp_use"`
	BPUseHalf    int                               `json:"bp_use_half"`
	Continue     bool                              `json:"continue"`
	Enabled      bool                              `json:"enabled"`
	Targets      []dropTarget                      `json:"targets"`
	SourceGroup  json.RawMessage                   `json:"source_group"`
	Replay       gamestate.TeamBattleReplay        `json:"replay"`
	Reward       gamestate.TeamBattleRewardProfile `json:"reward"`
}

func (c *contentStore) customBossSource(id int) (DropBoss, error) {
	s, ok := c.bosses[id]
	if !ok || s.SourceBossID != 0 || len(s.Targets) == 0 {
		return s, errors.New("请选择有完整部位资料的现有 Boss 难度")
	}
	if rule, ok := c.ruleBases[id]; !ok || rule.BPUse < 1 || rule.BPUseHalf < 1 || rule.BPUseHalf > rule.BPUse {
		return s, errors.New("当前支持复制普通单人／组队 Boss 难度")
	}
	for _, t := range s.Targets {
		if t.Stats == nil {
			return s, errors.New("模板缺少敌人属性")
		}
	}
	for _, p := range c.base.TeamBattleRewards {
		if p.BossID == id && p.ScorePolicy != nil {
			return s, errors.New("圣剑杯计分副本暂不支持复制")
		}
	}
	for _, r := range c.base.TeamBattleReplays {
		if r.BossID != id {
			continue
		}
		if r.EnemyType == 4 || slices.ContainsFunc(r.Battles, func(w gamestate.TeamBattleReplayBattle) bool { return w.EnemyType == 4 }) {
			return s, errors.New("亚瑟敌方编队暂不支持复制")
		}
		return s, nil
	}
	return s, errors.New("模板缺少战斗编队")
}

func cloneBossTargets(rows []dropTarget) []dropTarget {
	if rows == nil {
		return nil
	}
	b, _ := json.Marshal(rows)
	var next []dropTarget
	_ = json.Unmarshal(b, &next)
	return next
}

func validateCustomBoss(b customBoss, original []dropTarget) error {
	if b.BossID < customBossFirstID || b.BossID > customBossLastID || b.GroupID != b.BossID+customBossGroupOffset {
		return errors.New("自定义 Boss 身份无效")
	}
	if len([]rune(strings.TrimSpace(b.Name))) < 1 || len([]rune(b.Name)) > 80 || len([]rune(strings.TrimSpace(b.Difficulty))) < 1 || len([]rune(b.Difficulty)) > 30 {
		return errors.New("名称须为1至80字，难度名须为1至30字")
	}
	if b.BPUse < 1 || b.BPUse > 999 || b.BPUseHalf < 1 || b.BPUseHalf > b.BPUse {
		return errors.New("单人体力须为1至999，组队体力须为1至单人体力")
	}
	if len(b.Targets) != len(original) || len(b.Targets) == 0 {
		return errors.New("必须保留模板的全部波次和部位")
	}
	seen := map[[2]int]bool{}
	for _, t := range b.Targets {
		key := [2]int{t.BattleIndex, t.EnemyIndex}
		if seen[key] || t.Stats == nil {
			return errors.New("部位重复或缺少属性")
		}
		seen[key] = true
		idx := slices.IndexFunc(original, func(v dropTarget) bool { return v.BattleIndex == t.BattleIndex && v.EnemyIndex == t.EnemyIndex })
		if idx < 0 || original[idx].Stats == nil || original[idx].Stats.EnemyID != t.Stats.EnemyID || t.Name != original[idx].Name {
			return errors.New("不能修改部位身份、数量或编队结构")
		}
		if err := gamestate.ValidateTeamBattleEnemyStats(*t.Stats); err != nil {
			return fmt.Errorf("第%d波 %s：%w", t.BattleIndex+1, t.Name, err)
		}
		base := original[idx].Stats
		if t.Stats.Attribute != base.Attribute && strings.Contains(t.Stats.Attribute, "_") {
			return errors.New("新属性请选择单一属性；模板的复合属性可原样保留")
		}
		if (base.AttributeRates == nil) != (t.Stats.AttributeRates == nil) || (base.StatusResistances == nil) != (t.Stats.StatusResistances == nil) || (base.DOTReductions == nil) != (t.Stats.DOTReductions == nil) {
			return errors.New("不能移除模板的抗性资料")
		}
	}
	return nil
}

func (o *Operations) loadCustomBosses(c *contentStore) error {
	doc, err := o.storage.ReadDocument(customBossKey)
	if err != nil {
		return err
	}
	c.customBossRevision = doc.Revision
	if doc.Revision > 0 {
		if err = json.Unmarshal(doc.Payload, &c.customBosses); err != nil {
			return err
		}
	}
	seen := map[int]bool{}
	for _, b := range c.customBosses {
		s, err := c.customBossSource(b.SourceBossID)
		if err != nil {
			return err
		}
		if seen[b.BossID] {
			return errors.New("自定义 Boss ID 重复")
		}
		seen[b.BossID] = true
		if err = validateCustomBoss(b, s.Targets); err != nil {
			return err
		}
		if b.Replay.BossID != b.BossID || b.Reward.BossID != b.BossID || len(b.SourceGroup) == 0 {
			return errors.New("自定义 Boss 战斗配置无效")
		}
	}
	c.installCustomBossMetadata()
	return nil
}

func (c *contentStore) installCustomBossMetadata() {
	for _, b := range c.customBosses {
		c.bosses[b.BossID] = DropBoss{BossID: b.BossID, GroupID: b.GroupID, SourceBossID: b.SourceBossID, Name: b.Name, Difficulty: b.Difficulty, Category: "boss", Targets: cloneBossTargets(b.Targets)}
	}
}

func (c *contentStore) customBossGroup(b customBoss) (json.RawMessage, AdminBattleGroup, error) {
	var group map[string]json.RawMessage
	if err := json.Unmarshal(b.SourceGroup, &group); err != nil {
		return nil, AdminBattleGroup{}, err
	}
	var bosses []map[string]json.RawMessage
	if err := json.Unmarshal(group["10"], &bosses); err != nil || len(bosses) != 1 {
		return nil, AdminBattleGroup{}, errors.New("模板难度身份无效")
	}
	set := func(fields map[string]json.RawMessage, key string, value any) { fields[key], _ = json.Marshal(value) }
	set(group, "0", b.GroupID)
	set(group, "4", b.Name)
	set(group, "9", 0)
	boss := bosses[0]
	set(boss, "0", b.BossID)
	set(boss, "4", b.Difficulty)
	set(boss, "5", b.BPUse)
	set(boss, "6", b.BPUseHalf)
	revive := 0
	if b.Continue {
		revive = 1
	}
	set(boss, "7", revive)
	set(boss, "1", 0)
	// Native offline solo loads unchanged client CSVs. Server-driven multiplayer
	// and AI rooms receive the independent stats in ApiGameStart instead.
	set(boss, "24", 3)
	set(boss, "10", 0)
	set(boss, "17", 0)
	set(boss, "16", 0)
	set(boss, "26", 0)
	set(group, "10", bosses)
	var pictureID, model int
	_ = json.Unmarshal(group["7"], &pictureID)
	_ = json.Unmarshal(boss["14"], &model)
	category := "2d"
	if model == 1 {
		category = "3d"
	}
	segments := max(1, len(b.Replay.Battles))
	meta := AdminBattleGroup{Custom: true, Enabled: b.Enabled, GroupID: b.GroupID, Name: b.Name, PictureID: pictureID, BossCount: 1, BossIDs: []int{b.BossID}, Category: category, Difficulties: []string{b.Difficulty}, SegmentCounts: []int{segments}, MaxSegments: segments, ImageURL: fmt.Sprintf("/assets/boss/%d.webp", pictureID), Bosses: []DropBoss{c.bosses[b.BossID]}}
	raw, err := json.Marshal(group)
	return raw, meta, err
}

func (c *contentStore) projectCustomBosses(state *gamestate.State) error {
	if len(c.customBosses) == 0 {
		return nil
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(state.TeamBattleSolo, &top); err != nil {
		return err
	}
	var groups []json.RawMessage
	if len(top["9"]) > 0 {
		if err := json.Unmarshal(top["9"], &groups); err != nil {
			return err
		}
	}
	for _, b := range c.customBosses {
		raw, _, err := c.customBossGroup(b)
		if err != nil {
			return err
		}
		groups = append(groups, raw)
		r := b.Replay
		r.EnemyOverrides = make([]gamestate.TeamBattleEnemyOverride, 0, len(b.Targets))
		for _, t := range b.Targets {
			r.EnemyOverrides = append(r.EnemyOverrides, gamestate.TeamBattleEnemyOverride{BattleIndex: t.BattleIndex, EnemyIndex: t.EnemyIndex, Stats: *t.Stats})
		}
		r.EnemyOverrides = gamestate.CloneTeamBattleEnemyOverrides(r.EnemyOverrides)
		state.TeamBattleReplays = append(state.TeamBattleReplays, r)
		state.TeamBattleRewards = append(state.TeamBattleRewards, b.Reward)
	}
	top["9"], _ = json.Marshal(groups)
	var err error
	state.TeamBattleSolo, err = json.Marshal(top)
	return err
}

func (a *API) customBossCatalog(w http.ResponseWriter, r *http.Request) {
	o := a.operations
	o.configMu.RLock()
	defer o.configMu.RUnlock()
	c := o.content
	if c == nil {
		WriteAdminError(w, 503, "未载入 Boss 目录")
		return
	}
	sources := []DropBoss{}
	for id := range c.bosses {
		if s, err := c.customBossSource(id); err == nil {
			sources = append(sources, s)
		}
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].BossID < sources[j].BossID })
	rows := c.customBosses
	if rows == nil {
		rows = []customBoss{}
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "revision": c.customBossRevision, "sources": sources, "bosses": rows})
}

func (a *API) createCustomBoss(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var input struct {
		Expected *int `json:"expected_revision"`
		Source   int  `json:"source_boss_id"`
	}
	if err := decodeAdminJSON(r, &input); err != nil || input.Expected == nil {
		WriteAdminError(w, 400, "请提交模板与配置版本")
		return
	}
	o := a.operations
	o.configMu.Lock()
	defer o.configMu.Unlock()
	c := o.content
	if c == nil {
		WriteAdminError(w, 503, "未载入 Boss 目录")
		return
	}
	s, err := c.customBossSource(input.Source)
	if err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	id := customBossFirstID
	for _, b := range c.customBosses {
		id = max(id, b.BossID+1)
	}
	baseGroups, err := battlePublicationGroupIDs(c.base)
	if err != nil {
		WriteAdminError(w, 500, err.Error())
		return
	}
	groupOccupied := func(groupID int) bool {
		for _, ids := range baseGroups {
			if slices.Contains(ids, groupID) {
				return true
			}
		}
		return false
	}
	for id <= customBossLastID && (groupOccupied(id+customBossGroupOffset) || slices.ContainsFunc(c.base.TeamBattleReplays, func(r gamestate.TeamBattleReplay) bool { return r.BossID == id })) {
		id++
	}
	if id > customBossLastID {
		WriteAdminError(w, 400, "自定义 Boss 数量已达上限")
		return
	}
	b := customBoss{BossID: id, GroupID: id + customBossGroupOffset, SourceBossID: input.Source, Name: s.Name + "（自定义）", Difficulty: s.Difficulty, BPUse: c.ruleBases[input.Source].BPUse, BPUseHalf: c.ruleBases[input.Source].BPUseHalf, Continue: c.ruleBases[input.Source].Continue, Targets: cloneBossTargets(s.Targets)}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(c.base.TeamBattleSolo, &fields)
	for _, key := range []string{"9", "10", "11", "12"} {
		var groups []json.RawMessage
		_ = json.Unmarshal(fields[key], &groups)
		for _, raw := range groups {
			var g map[string]json.RawMessage
			_ = json.Unmarshal(raw, &g)
			var bosses []map[string]json.RawMessage
			_ = json.Unmarshal(g["10"], &bosses)
			for _, v := range bosses {
				var bid int
				_ = json.Unmarshal(v["0"], &bid)
				if bid == input.Source {
					g["10"], _ = json.Marshal([]map[string]json.RawMessage{v})
					b.SourceGroup, _ = json.Marshal(g)
				}
			}
		}
	}
	for _, replay := range c.base.TeamBattleReplays {
		if replay.BossID == input.Source {
			b.Replay = replay
			b.Replay.BossID = id
		}
	}
	for _, reward := range c.configuration.State.TeamBattleRewards {
		if reward.BossID == input.Source {
			payload, _ := json.Marshal(reward)
			_ = json.Unmarshal(payload, &b.Reward)
			b.Reward.BossID = id
		}
	}
	if b.Reward.BossID != id || len(b.SourceGroup) == 0 {
		WriteAdminError(w, 400, "模板缺少副本或奖励配置")
		return
	}
	if err = validateCustomBoss(b, s.Targets); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	next := append(slices.Clone(c.customBosses), b)
	if err = o.saveCustomBossesLocked(*input.Expected, next); err != nil {
		writeContentError(w, err)
		return
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "revision": c.customBossRevision, "boss": b})
}

func (a *API) saveCustomBoss(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var input struct {
		Expected   *int         `json:"expected_revision"`
		BossID     int          `json:"boss_id"`
		Name       string       `json:"name"`
		Difficulty string       `json:"difficulty"`
		BPUse      int          `json:"bp_use"`
		BPUseHalf  int          `json:"bp_use_half"`
		Continue   *bool        `json:"continue"`
		Enabled    *bool        `json:"enabled"`
		Targets    []dropTarget `json:"targets"`
	}
	if err := decodeAdminJSON(r, &input); err != nil || input.Expected == nil || input.Enabled == nil || input.Continue == nil {
		WriteAdminError(w, 400, "请提交完整配置与版本")
		return
	}
	o := a.operations
	o.configMu.Lock()
	defer o.configMu.Unlock()
	c := o.content
	if c == nil {
		WriteAdminError(w, 503, "未载入 Boss 目录")
		return
	}
	index := slices.IndexFunc(c.customBosses, func(b customBoss) bool { return b.BossID == input.BossID })
	if index < 0 {
		WriteAdminError(w, 404, "未找到自定义 Boss")
		return
	}
	b := c.customBosses[index]
	source, err := c.customBossSource(b.SourceBossID)
	if err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	original := source.Targets
	b.Name, b.Difficulty = strings.TrimSpace(input.Name), strings.TrimSpace(input.Difficulty)
	b.BPUse, b.BPUseHalf, b.Continue, b.Enabled = input.BPUse, input.BPUseHalf, *input.Continue, *input.Enabled
	b.Targets = input.Targets
	if err := validateCustomBoss(b, original); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	next := slices.Clone(c.customBosses)
	next[index] = b
	if err := o.saveCustomBossesLocked(*input.Expected, next); err != nil {
		writeContentError(w, err)
		return
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "revision": c.customBossRevision, "boss": b})
}

func (o *Operations) saveCustomBossesLocked(expected int, next []customBoss) error {
	c := o.content
	candidate := *c
	candidate.customBosses = next
	state, err := candidate.project(c.drops, c.shops, c.rules)
	if err != nil {
		return err
	}
	ids, err := battlePublicationGroupIDs(state)
	if err != nil {
		return err
	}
	doc, err := o.writeDocument(customBossKey, expected, next)
	if err != nil {
		return err
	}
	c.customBosses, c.customBossRevision = next, doc.Revision
	c.installCustomBossMetadata()
	c.configuration = game.ContentConfiguration{Revision: c.configuration.Revision + 1, State: state}
	o.battleGroupIDs = ids
	return nil
}

func (o *Operations) customBossGroups() []AdminBattleGroup {
	o.configMu.RLock()
	defer o.configMu.RUnlock()
	rows := []AdminBattleGroup{}
	if o.content != nil {
		for _, b := range o.content.customBosses {
			_, g, err := o.content.customBossGroup(b)
			if err == nil {
				rows = append(rows, g)
			}
		}
	}
	return rows
}
