package admin

import (
	"fmt"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// Copies publish in the standalone catalogue. A source also used by a story
// quest/tower must not retain that source's settlement context. Normalize the
// projection too, so existing copies can start without editing saved documents.
func standaloneCustomBossReward(p gamestate.TeamBattleRewardProfile) gamestate.TeamBattleRewardProfile {
	p.StageQuestAreaID, p.StageQuestStageID, p.TowerID, p.TowerFloor = 0, 0, 0, 0
	return p
}

func (c *contentStore) validateCustomBossActions(b customBoss) error {
	source, err := c.customBossSource(b.SourceBossID)
	if err != nil {
		return err
	}
	allowedAnimation := map[int]bool{}
	for _, entry := range c.customBossSkillEntries(source) {
		allowedAnimation[entry.FunctionID] = true
	}
	for _, t := range b.Targets {
		for _, a := range t.Actions {
			if a.AnimationFunctionID > 0 && !allowedAnimation[a.AnimationFunctionID] {
				return fmt.Errorf("演出须从原 Boss 的招式中选择")
			}
			if _, _, err := c.customSkillCatalog.CustomEnemySkill(a); err != nil {
				return fmt.Errorf("第%d波 %s：%w", t.BattleIndex+1, t.Name, err)
			}
		}
	}
	return nil
}

func (a *API) customBossSkills(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.URL.Query().Get("source_boss_id"))
	if err != nil {
		WriteAdminError(w, 400, "请选择招式来源 Boss")
		return
	}
	o := a.operations
	o.configMu.RLock()
	defer o.configMu.RUnlock()
	c := o.content
	if c == nil || c.customSkillCatalog == nil {
		WriteAdminError(w, 503, "资源集缺少敌人技能资料")
		return
	}
	source, err := c.customBossSource(id)
	if err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "skills": c.customBossSkillEntries(source)})
}

type customBossSkillEntry struct {
	SkillID      int                                 `json:"skill_id"`
	FunctionID   int                                 `json:"function_id"`
	Name         string                              `json:"name"`
	Part         string                              `json:"part"`
	Attack       bool                                `json:"attack"`
	PlayerBuff   bool                                `json:"player_buff"`
	Effects      []string                            `json:"effects"`
	BuffEditors  []multiplayer.CustomEnemyBuffEditor `json:"buff_editors"`
	Effect2D     string                              `json:"effect_2d"`
	Effect3D     string                              `json:"effect_3d"`
	RoleFamilies []string                            `json:"role_families"`
}

func (c *contentStore) customBossSkillEntries(source DropBoss) []customBossSkillEntry {
	rows := []customBossSkillEntry{}
	if c.customSkillCatalog == nil {
		return rows
	}
	seen := map[[2]int]bool{}
	for _, t := range source.Targets {
		for _, action := range c.customSkillCatalog.EnemyLevels[t.Stats.EnemyID].Actions {
			if action.Category == "death" {
				continue
			}
			for _, skill := range c.customSkillCatalog.EnemySkills[action.SkillID] {
				key := [2]int{skill.ID, skill.FunctionID}
				if seen[key] {
					continue
				}
				_, roles, err := c.customSkillCatalog.CustomEnemySkill(gamestate.TeamBattleEnemyAction{SkillID: skill.ID, FunctionID: skill.FunctionID})
				if err != nil {
					continue
				}
				seen[key] = true
				attack := false
				for _, role := range roles {
					attack = attack || role.Function == "ATTACK_AA"
				}
				effects, playerBuff := multiplayer.DescribeCustomEnemySkill(skill, roles)
				families := []string{}
				for _, role := range roles {
					families = append(families, multiplayer.CustomEnemyAnimationFamily(role.Function))
				}
				rows = append(rows, customBossSkillEntry{SkillID: skill.ID, FunctionID: skill.FunctionID, Name: skill.Name, Part: t.Name, Attack: attack, PlayerBuff: playerBuff, Effects: effects, BuffEditors: multiplayer.CustomEnemyBuffEditors(skill, roles), Effect2D: roles[0].Effect2D, Effect3D: roles[0].Effect3D, RoleFamilies: families})
			}
		}
	}
	return rows
}

func (a *API) customBossBuffs(w http.ResponseWriter, _ *http.Request) {
	o := a.operations
	o.configMu.RLock()
	defer o.configMu.RUnlock()
	c := o.content
	if c == nil || c.customSkillCatalog == nil {
		WriteAdminError(w, 503, "资源集缺少敌人技能资料")
		return
	}
	ids := []int{40320002, 30860102, 30860103}
	for id := range c.bosses {
		ids = append(ids, id)
	}
	sort.Ints(ids[3:])
	type preset struct {
		customBossSkillEntry
		SourceBossID int    `json:"source_boss_id"`
		SourceName   string `json:"source_name"`
		Label        string `json:"label"`
	}
	rows := []preset{}
	seen := map[int]bool{}
	for _, id := range ids {
		source, err := c.customBossSource(id)
		if err != nil {
			continue
		}
		for _, s := range c.customBossSkillEntries(source) {
			if !s.PlayerBuff || seen[s.FunctionID] {
				continue
			}
			roles := c.customSkillCatalog.EnemySkillRoles[s.FunctionID]
			if !multiplayer.CustomEnemyPlayerSupportOnly(c.customSkillCatalog.EnemySkills[s.SkillID], s.FunctionID, roles) {
				continue
			}
			seen[s.FunctionID] = true
			label := strings.Join(s.Effects, "；") + " · " + source.Name + " · " + source.Difficulty
			rows = append(rows, preset{s, id, source.Name + " · " + source.Difficulty, label})
		}
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "buffs": rows})
}
