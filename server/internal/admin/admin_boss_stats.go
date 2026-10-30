package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

const bossStatsKey = "boss-stats"

// Persist only fields the operator actually overrides. Everything else comes
// from the currently installed package, including after a resource upgrade.
type bossStatsTarget struct {
	BattleIndex    int                                      `json:"battle_index"`
	EnemyIndex     int                                      `json:"enemy_index"`
	EnemyID        int                                      `json:"enemy_id"`
	Values         map[string]int                           `json:"values,omitempty"`
	Attribute      *string                                  `json:"attribute,omitempty"`
	OriginalSkills []gamestate.TeamBattleEnemyOriginalSkill `json:"original_skills,omitempty"`
}

func (c *contentStore) originalBossSource(id int) (DropBoss, error) {
	b, ok := c.bosses[id]
	if !ok || b.SourceBossID != 0 || c.rewardBossID(id) != id || len(b.Targets) == 0 {
		return b, errors.New("请选择有完整敌人资料的原版 BOSS 难度；复制品请到自定义 Boss 编辑")
	}
	var replay *gamestate.TeamBattleReplay
	for i := range c.base.TeamBattleReplays {
		if c.base.TeamBattleReplays[i].BossID == id {
			replay = &c.base.TeamBattleReplays[i]
			break
		}
	}
	if replay == nil {
		return b, errors.New("该难度缺少服务端战斗编队")
	}
	if replay.EnemyType == 4 || slices.ContainsFunc(replay.Battles, func(w gamestate.TeamBattleReplayBattle) bool { return w.EnemyType == 4 }) {
		return b, errors.New("亚瑟敌方编队暂不支持直接修改数值")
	}
	b = c.bossWithName(b)
	b.Targets = cloneBossTargets(b.Targets)
	for i := range b.Targets {
		t := &b.Targets[i]
		if t.Stats == nil {
			return b, errors.New("该难度缺少完整敌人属性")
		}
		// A packaged runtime override is part of the package default too.
		for _, row := range replay.EnemyOverrides {
			if row.BattleIndex != t.BattleIndex || row.EnemyIndex != t.EnemyIndex {
				continue
			}
			if row.Stats.EnemyID != t.Stats.EnemyID {
				return b, errors.New("资源包的敌人属性与编队身份不一致")
			}
			s := gamestate.CloneTeamBattleEnemyOverrides([]gamestate.TeamBattleEnemyOverride{row})[0].Stats
			if s.AttributeRates == nil {
				s.AttributeRates = t.Stats.AttributeRates
			}
			if s.StatusResistances == nil {
				s.StatusResistances = t.Stats.StatusResistances
			}
			if s.DOTReductions == nil {
				s.DOTReductions = t.Stats.DOTReductions
			}
			t.Stats = &s
		}
	}
	return b, nil
}

func bossStatValue(s *enemyStats, key string) *int {
	fields := map[string]*int{"hp": &s.HP, "attack": &s.Attack, "magic": &s.Magic, "recovery": &s.Recovery, "defense": &s.Defense, "magic_defense": &s.MagicDefense, "damage_reduction": &s.DamageReduction}
	if p := fields[key]; p != nil {
		return p
	}
	kind, slot, ok := strings.Cut(key, ":")
	i, err := strconv.Atoi(slot)
	if !ok || err != nil || i < 0 || strconv.Itoa(i) != slot {
		return nil
	}
	switch kind {
	case "attribute_fixed":
		if i < len(s.AttributeFixed) {
			return &s.AttributeFixed[i]
		}
	case "attribute_rates":
		if s.AttributeRates != nil && i < len(s.AttributeRates) {
			return &s.AttributeRates[i]
		}
	case "status_resistances":
		if s.StatusResistances != nil && i > 0 && i < len(s.StatusResistances) {
			return &s.StatusResistances[i]
		}
	case "dot_reductions":
		if s.DOTReductions != nil && i < len(s.DOTReductions) {
			return &s.DOTReductions[i]
		}
	}
	return nil
}

func applyBossStatValues(base enemyStats, patch bossStatsTarget) (enemyStats, error) {
	s := gamestate.CloneTeamBattleEnemyOverrides([]gamestate.TeamBattleEnemyOverride{{Stats: base}})[0].Stats
	for key, n := range patch.Values {
		p := bossStatValue(&s, key)
		if p == nil {
			return s, fmt.Errorf("不支持的属性字段 %q", key)
		}
		*p = n
	}
	if patch.Attribute != nil {
		if *patch.Attribute != base.Attribute && strings.Contains(*patch.Attribute, "_") {
			return s, errors.New("新属性请选择单一属性；资源包的复合属性可原样保留")
		}
		s.Attribute = *patch.Attribute
	}
	return s, gamestate.ValidateTeamBattleEnemyStats(s)
}

func (c *contentStore) validateBossStats(id int, rows []bossStatsTarget) ([]bossStatsTarget, error) {
	b, err := c.originalBossSource(id)
	if err != nil {
		return nil, err
	}
	if len(rows) > len(b.Targets) {
		return nil, errors.New("不能增加波次或部位")
	}
	seen := map[[2]int]bool{}
	next := []bossStatsTarget{}
	for _, row := range rows {
		key := [2]int{row.BattleIndex, row.EnemyIndex}
		i := slices.IndexFunc(b.Targets, func(t dropTarget) bool { return t.BattleIndex == row.BattleIndex && t.EnemyIndex == row.EnemyIndex })
		if i < 0 || seen[key] || row.EnemyID != b.Targets[i].Stats.EnemyID {
			return nil, errors.New("部位重复或敌人身份不匹配，不能改变编队结构")
		}
		seen[key] = true
		if _, err := applyBossStatValues(*b.Targets[i].Stats, row); err != nil {
			return nil, fmt.Errorf("第%d波 %s：%w", row.BattleIndex+1, b.Targets[i].Name, err)
		}
		if len(row.OriginalSkills) > 0 {
			if c.customSkillCatalog == nil {
				return nil, errors.New("资源包缺少技能资料，不能修改招式")
			}
			if err := c.customSkillCatalog.ValidateOriginalEnemySkills(c.customSkillCatalog.EnemyLevels[row.EnemyID], row.OriginalSkills); err != nil {
				return nil, err
			}
		}
		row.OriginalSkills = slices.DeleteFunc(gamestate.CloneTeamBattleEnemyOriginalSkills(row.OriginalSkills), func(s gamestate.TeamBattleEnemyOriginalSkill) bool {
			return s.Power == nil && s.PowerRate == nil && s.Hits == nil
		})
		if len(row.Values) > 0 || row.Attribute != nil || len(row.OriginalSkills) > 0 {
			next = append(next, row)
		}
	}
	return next, nil
}

func (o *Operations) loadBossStats(c *contentStore) error {
	doc, err := o.storage.ReadDocument(bossStatsKey)
	if err != nil {
		return err
	}
	c.bossStatsRevision = doc.Revision
	c.bossStats = map[int][]bossStatsTarget{}
	if doc.Revision > 0 {
		if err := json.Unmarshal(doc.Payload, &c.bossStats); err != nil {
			return err
		}
	}
	for id, rows := range c.bossStats {
		next, err := c.validateBossStats(id, rows)
		if err != nil {
			return fmt.Errorf("BOSS %d 数值配置：%w", id, err)
		}
		c.bossStats[id] = next
	}
	return nil
}

func (c *contentStore) projectBossStats(state *gamestate.State) error {
	for i := range state.TeamBattleReplays {
		r := &state.TeamBattleReplays[i]
		id := c.rewardBossID(r.BossID)
		patches := c.bossStats[id]
		if len(patches) == 0 {
			continue
		}
		source, err := c.originalBossSource(id)
		if err != nil {
			return err
		}
		r.EnemyOverrides = gamestate.CloneTeamBattleEnemyOverrides(r.EnemyOverrides)
		for _, patch := range patches {
			t := slices.IndexFunc(source.Targets, func(t dropTarget) bool { return t.BattleIndex == patch.BattleIndex && t.EnemyIndex == patch.EnemyIndex })
			if t < 0 {
				return errors.New("BOSS 数值配置引用未知部位")
			}
			j := slices.IndexFunc(r.EnemyOverrides, func(v gamestate.TeamBattleEnemyOverride) bool {
				return v.BattleIndex == patch.BattleIndex && v.EnemyIndex == patch.EnemyIndex
			})
			defaults := *source.Targets[t].Stats
			if j >= 0 {
				packaged := r.EnemyOverrides[j].Stats
				if packaged.EnemyID != patch.EnemyID {
					return errors.New("关联编队的敌人身份与数值配置不匹配")
				}
				if packaged.AttributeRates == nil {
					packaged.AttributeRates = defaults.AttributeRates
				}
				if packaged.StatusResistances == nil {
					packaged.StatusResistances = defaults.StatusResistances
				}
				if packaged.DOTReductions == nil {
					packaged.DOTReductions = defaults.DOTReductions
				}
				defaults = packaged
			}
			s, err := applyBossStatValues(defaults, patch)
			if err != nil {
				return err
			}
			if j < 0 {
				r.EnemyOverrides = append(r.EnemyOverrides, gamestate.TeamBattleEnemyOverride{BattleIndex: patch.BattleIndex, EnemyIndex: patch.EnemyIndex})
				j = len(r.EnemyOverrides) - 1
			}
			row := &r.EnemyOverrides[j]
			row.Stats = s
			for _, edit := range gamestate.CloneTeamBattleEnemyOriginalSkills(patch.OriginalSkills) {
				k := slices.IndexFunc(row.OriginalSkills, func(s gamestate.TeamBattleEnemyOriginalSkill) bool {
					return s.Slot == edit.Slot && s.SkillID == edit.SkillID && s.FunctionID == edit.FunctionID
				})
				if k < 0 {
					row.OriginalSkills = append(row.OriginalSkills, edit)
				} else {
					// Blank fields still inherit a packaged skill override.
					if edit.Power != nil {
						row.OriginalSkills[k].Power = edit.Power
					}
					if edit.PowerRate != nil {
						row.OriginalSkills[k].PowerRate = edit.PowerRate
					}
					if edit.Hits != nil {
						row.OriginalSkills[k].Hits = edit.Hits
					}
				}
			}
		}
	}
	return nil
}

func (c *contentStore) bossWithEditedStats(b DropBoss) DropBoss {
	if b.SourceBossID != 0 || len(c.bossStats[b.BossID]) == 0 {
		return b
	}
	source, err := c.originalBossSource(b.BossID)
	if err != nil {
		return b
	}
	b.Targets = source.Targets
	b.StatsEdited = true
	for _, row := range c.bossStats[b.BossID] {
		for i := range b.Targets {
			t := &b.Targets[i]
			if t.BattleIndex == row.BattleIndex && t.EnemyIndex == row.EnemyIndex {
				s, err := applyBossStatValues(*t.Stats, row)
				if err == nil {
					t.Stats = &s
				}
			}
		}
	}
	return b
}

func (a *API) bossStats(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.URL.Query().Get("boss_id"))
	o := a.operations
	o.configMu.RLock()
	defer o.configMu.RUnlock()
	if o.content == nil {
		WriteAdminError(w, 503, "未载入 BOSS 目录")
		return
	}
	writeBossStatsEditor(w, o.content, id)
}

func writeBossStatsEditor(w http.ResponseWriter, c *contentStore, id int) {
	b, err := c.originalBossSource(id)
	if err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	rows := c.bossStats[id]
	if rows == nil {
		rows = []bossStatsTarget{}
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "revision": c.bossStatsRevision, "boss": b, "overrides": rows, "original_actions": c.customBossOriginalActions(b)})
}

func (a *API) saveBossStats(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var input struct {
		Expected  *int               `json:"expected_revision"`
		BossID    int                `json:"boss_id"`
		Overrides *[]bossStatsTarget `json:"overrides"`
	}
	if err := DecodeAdminJSONLimit(r, &input, 2<<20); err != nil || input.Expected == nil || input.Overrides == nil {
		WriteAdminError(w, 400, "请提交难度、数值覆盖列表与配置版本；空列表表示使用资源包原值")
		return
	}
	o := a.operations
	o.configMu.Lock()
	defer o.configMu.Unlock()
	c := o.content
	if c == nil {
		WriteAdminError(w, 503, "未载入 BOSS 目录")
		return
	}
	rows, err := c.validateBossStats(input.BossID, *input.Overrides)
	if err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	next := maps.Clone(c.bossStats)
	if next == nil {
		next = map[int][]bossStatsTarget{}
	}
	if len(rows) == 0 {
		delete(next, input.BossID)
	} else {
		next[input.BossID] = rows
	}
	candidate := *c
	candidate.bossStats = next
	state, err := candidate.project(c.drops, c.shops, c.rules)
	if err != nil {
		writeContentError(w, err)
		return
	}
	doc, err := o.writeDocument(bossStatsKey, *input.Expected, next)
	if err != nil {
		writeContentError(w, err)
		return
	}
	c.bossStats, c.bossStatsRevision = next, doc.Revision
	c.configuration = game.ContentConfiguration{Revision: c.configuration.Revision + 1, State: state}
	writeBossStatsEditor(w, c, input.BossID)
}
