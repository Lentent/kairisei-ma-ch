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
	"unicode"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

const bossNamesKey = "boss-names"

func validateBossName(name string) error {
	if len([]rune(name)) > 80 || strings.ContainsFunc(name, func(r rune) bool { return unicode.IsControl(r) || r == '\u2028' || r == '\u2029' }) {
		return errors.New("BOSS名称最多80字，不能包含换行或控制字符；留空可恢复原名")
	}
	return nil
}

func (o *Operations) loadBossNames(c *contentStore) error {
	doc, err := o.storage.ReadDocument(bossNamesKey)
	if err != nil {
		return err
	}
	c.bossNames = map[int]string{}
	c.bossNameRevision = doc.Revision
	if doc.Revision > 0 {
		if err := json.Unmarshal(doc.Payload, &c.bossNames); err != nil {
			return err
		}
	}
	for id, name := range c.bossNames {
		if _, ok := c.bossNameBases[id]; !ok {
			return fmt.Errorf("BOSS名称引用未知副本组 %d", id)
		}
		if err := validateBossName(name); err != nil {
			return err
		}
	}
	return nil
}

func (c *contentStore) bossWithName(b DropBoss) DropBoss {
	if b.SourceBossID == 0 && c.bossNames[b.GroupID] != "" {
		b.Name = c.bossNames[b.GroupID]
	}
	return b
}

// Both client catalogs use the original group ID. Only the group title is
// replaced; difficulty identities, period labels, drops and progress survive.
func (c *contentStore) projectBossNames(state *gamestate.State) error {
	if len(c.bossNames) == 0 {
		return nil
	}
	projectGroups := func(raw json.RawMessage) (json.RawMessage, error) {
		var groups []map[string]json.RawMessage
		if err := json.Unmarshal(raw, &groups); err != nil {
			return nil, err
		}
		for _, group := range groups {
			var id int
			if err := json.Unmarshal(group["0"], &id); err != nil {
				return nil, err
			}
			if name := c.bossNames[id]; name != "" {
				group["4"], _ = json.Marshal(name)
			}
		}
		return json.Marshal(groups)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(state.TeamBattleSolo, &top); err != nil {
		return err
	}
	for _, key := range []string{"9", "10", "11", "12"} {
		if len(top[key]) == 0 {
			continue
		}
		raw, err := projectGroups(top[key])
		if err != nil {
			return err
		}
		top[key] = raw
	}
	var err error
	state.TeamBattleSolo, err = json.Marshal(top)
	if err != nil {
		return err
	}
	if len(state.TeamBattlePastBossGroups) > 0 {
		raw, err := json.Marshal(state.TeamBattlePastBossGroups)
		if err != nil {
			return err
		}
		raw, err = projectGroups(raw)
		if err != nil {
			return err
		}
		var groups []json.RawMessage
		if err := json.Unmarshal(raw, &groups); err != nil {
			return err
		}
		state.TeamBattlePastBossGroups = groups
	}
	return nil
}

func (o *Operations) namedBossGroups(groups []AdminBattleGroup) []AdminBattleGroup {
	o.configMu.RLock()
	defer o.configMu.RUnlock()
	rows := slices.Clone(groups)
	if o.content == nil {
		return rows
	}
	for i := range rows {
		g := &rows[i]
		if g.Custom {
			continue
		}
		if name := o.content.bossNames[g.GroupID]; name != "" {
			g.Name = name
		}
		g.Bosses = slices.Clone(g.Bosses)
		for j, b := range g.Bosses {
			g.Bosses[j] = o.content.bossWithName(b)
		}
	}
	return rows
}

func (a *API) bossName(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.URL.Query().Get("group_id"))
	o := a.operations
	o.configMu.RLock()
	defer o.configMu.RUnlock()
	if o.content == nil {
		WriteAdminError(w, 503, "未载入BOSS目录")
		return
	}
	c := o.content
	base, ok := c.bossNameBases[id]
	if !ok {
		WriteAdminError(w, 404, "未找到原版BOSS副本组；自定义Boss请在编辑页改名")
		return
	}
	name := c.bossNames[id]
	if name == "" {
		name = base
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "revision": c.bossNameRevision, "group_id": id, "name": name, "default_name": base})
}

func (a *API) saveBossName(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var input struct {
		Expected *int    `json:"expected_revision"`
		GroupID  int     `json:"group_id"`
		Name     *string `json:"name"`
	}
	if err := decodeAdminJSON(r, &input); err != nil || input.Expected == nil || input.Name == nil {
		WriteAdminError(w, 400, "请提交副本组、名称与配置版本")
		return
	}
	name := strings.TrimSpace(*input.Name)
	if err := validateBossName(name); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	o := a.operations
	o.configMu.Lock()
	defer o.configMu.Unlock()
	c := o.content
	if c == nil {
		WriteAdminError(w, 503, "未载入BOSS目录")
		return
	}
	base, ok := c.bossNameBases[input.GroupID]
	if !ok {
		WriteAdminError(w, 400, "未找到原版BOSS副本组；自定义Boss请在编辑页改名")
		return
	}
	next := maps.Clone(c.bossNames)
	if next == nil {
		next = map[int]string{}
	}
	if name == "" || name == base {
		delete(next, input.GroupID)
		name = base
	} else {
		next[input.GroupID] = name
	}
	candidate := *c
	candidate.bossNames = next
	state, err := candidate.project(c.drops, c.shops, c.rules)
	if err != nil {
		writeContentError(w, err)
		return
	}
	doc, err := o.writeDocument(bossNamesKey, *input.Expected, next)
	if err != nil {
		writeContentError(w, err)
		return
	}
	c.bossNames, c.bossNameRevision = next, doc.Revision
	c.configuration = game.ContentConfiguration{Revision: c.configuration.Revision + 1, State: state}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "revision": doc.Revision, "group_id": input.GroupID, "name": name, "default_name": base})
}
