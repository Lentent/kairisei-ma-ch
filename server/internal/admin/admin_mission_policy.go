package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

const (
	missionPolicyKey      = "mission-policy"
	maxManagedMissions    = 200
	firstManagedMissionID = 1000000
	maxManagedMissionID   = 2147483647 // CN client mission identities are Int32.
)

type MissionPolicy struct {
	Missions []game.MissionDefinition `json:"missions"`
}

// Deleted identities and the allocation cursor are saved with the config in
// one revision-checked transaction. Recreating a task never inherits a retired
// task's receipt record and never resets a previously claimed task.
type missionPolicyDocument struct {
	Config     MissionPolicy `json:"config"`
	NextID     int           `json:"next_id"`
	RetiredIDs []int         `json:"retired_ids"`
}

type missionPolicySnapshot struct {
	Value    missionPolicyDocument
	Revision int
	Runtime  game.MissionConfiguration
}

func missionSnapshot(value missionPolicyDocument, revision int) *missionPolicySnapshot {
	return &missionPolicySnapshot{Value: value, Revision: revision, Runtime: game.MissionConfiguration{
		Revision: uint64(revision), Missions: value.Config.Missions,
	}}
}

func (o *Operations) loadMissionPolicy() error {
	o.configMu.Lock()
	defer o.configMu.Unlock()
	doc, err := o.storage.ReadDocument(missionPolicyKey)
	if err != nil {
		return err
	}
	var value missionPolicyDocument
	if doc.Revision == 0 {
		legacy := o.playerPolicy.Load()
		if legacy == nil {
			return errors.New("任务迁移前须载入原运营配置")
		}
		// Migration happens only once. An explicit saved [] is authoritative.
		value = missionPolicyDocument{
			Config: MissionPolicy{Missions: append([]game.MissionDefinition{}, legacy.Value.Missions...)},
			NextID: firstManagedMissionID, RetiredIDs: []int{},
		}
		for i := range value.Config.Missions {
			def := &value.Config.Missions[i]
			if len(def.Rewards) == 0 {
				def.Rewards = []gamestate.Reward{{Type: 10, Num: def.Crystals, CardSkillLevels: []int16{}}}
			}
			value.NextID = max(value.NextID, def.ID+1)
		}
		if err := validateMissionDocument(value); err != nil {
			return fmt.Errorf("migrate mission policy: %w", err)
		}
		doc, err = o.writeDocument(missionPolicyKey, 0, value)
		if errors.Is(err, accountstore.ErrDocumentConflict) {
			// Another initializer may have completed the same one-time upgrade.
			doc, err = o.storage.ReadDocument(missionPolicyKey)
			if err == nil {
				err = json.Unmarshal(doc.Payload, &value)
			}
		}
		if err != nil {
			return err
		}
	} else if err := json.Unmarshal(doc.Payload, &value); err != nil {
		return fmt.Errorf("decode mission policy: %w", err)
	}
	if err := validateMissionDocument(value); err != nil {
		return fmt.Errorf("mission policy: %w", err)
	}
	o.missionPolicy.Store(missionSnapshot(value, doc.Revision))
	return nil
}

func validateMissionDefinition(def game.MissionDefinition) error {
	if strings.TrimSpace(def.Title) == "" || len([]rune(def.Title)) > 60 {
		return errors.New("任务标题须为1至60字")
	}
	if len([]rune(def.Description)) > 200 {
		return errors.New("任务说明最多200字")
	}
	if def.Target < 1 || def.Target > 10000000 {
		return errors.New("任务目标须为1至10000000的整数")
	}
	switch def.Kind {
	case "login":
		if def.Daily && def.Target != 1 {
			return errors.New("每日登录的目标固定为1次")
		}
	case "explore":
	case "level", "collection":
		if def.Daily {
			return errors.New("等级和卡牌图鉴任务仅支持成就分类")
		}
	default:
		return errors.New("任务条件仅支持登录、探索、等级和卡牌图鉴")
	}
	if len(def.Rewards) < 1 || len(def.Rewards) > 4 {
		return errors.New("每项任务须配置1至4种奖励")
	}
	return nil
}

func validateMissionDocument(value missionPolicyDocument) error {
	if value.Config.Missions == nil || len(value.Config.Missions) > maxManagedMissions {
		return errors.New("任务清单须为数组，最多200项；清空请保存空数组")
	}
	if value.NextID < firstManagedMissionID || value.NextID > maxManagedMissionID+1 {
		return errors.New("任务编号分配记录无效")
	}
	seen := map[int]bool{}
	for _, def := range value.Config.Missions {
		if def.ID <= 0 || def.ID > maxManagedMissionID || def.ID >= value.NextID || seen[def.ID] {
			return errors.New("任务编号重复或分配记录无效")
		}
		seen[def.ID] = true
		if err := validateMissionDefinition(def); err != nil {
			return err
		}
	}
	for _, id := range value.RetiredIDs {
		if id <= 0 || id > maxManagedMissionID || id >= value.NextID || seen[id] {
			return errors.New("已删除任务编号记录无效")
		}
		seen[id] = true
	}
	return nil
}

// Gift-mail validation is the shared authority for catalog availability,
// quantities and card progression. Store only its canonical reward form.
func (a *API) canonicalMissionRewards(def *game.MissionDefinition) error {
	seen := map[string]bool{}
	rewards := make([]gamestate.Reward, 0, len(def.Rewards))
	for _, r := range def.Rewards {
		key := adminCatalogKey(r.Type, r.RewardTypeID)
		if seen[key] {
			return errors.New("同一项任务不能配置重复奖励")
		}
		canonical, _, err := a.mailReward(AdminMailRequest{
			RewardType: r.Type, RewardTypeID: r.RewardTypeID, Quantity: r.Num,
			CardLevel: int(r.CardLevel), CardFame: int(r.CardFame), CardLove: r.CardLove,
		})
		if err != nil {
			return fmt.Errorf("任务奖励 %d:%d: %w", r.Type, r.RewardTypeID, err)
		}
		seen[key] = true
		rewards = append(rewards, canonical)
	}
	def.Rewards = rewards
	// This field exists solely for legacy compatibility. New rewards are
	// authoritative, so a stale crystal value cannot add a hidden reward.
	def.Crystals = 0
	for _, r := range rewards {
		if r.Type == 10 {
			def.Crystals = r.Num
		}
	}
	return nil
}

func (a *API) validateSavedMissionRewards() error {
	policy := a.operations.missionPolicy.Load()
	if policy == nil {
		return nil
	}
	for _, def := range policy.Value.Config.Missions {
		copy := def
		if err := a.canonicalMissionRewards(&copy); err != nil {
			return fmt.Errorf("saved mission %d: %w", def.ID, err)
		}
		if !slices.EqualFunc(copy.Rewards, def.Rewards, func(a, b gamestate.Reward) bool {
			return a.Type == b.Type && a.RewardTypeID == b.RewardTypeID && a.Num == b.Num &&
				a.CardLevel == b.CardLevel && a.CardFame == b.CardFame && a.CardLove == b.CardLove &&
				slices.Equal(a.CardSkillLevels, b.CardSkillLevels)
		}) {
			return fmt.Errorf("saved mission %d has noncanonical rewards", def.ID)
		}
	}
	return nil
}

func (a *API) missions(w http.ResponseWriter, _ *http.Request) {
	policy := a.operations.missionPolicy.Load()
	if policy == nil {
		WriteAdminError(w, 503, "任务目录尚未载入")
		return
	}
	entries := []AdminCatalogEntry{}
	seen := map[string]bool{}
	for _, def := range policy.Value.Config.Missions {
		for _, r := range def.Rewards {
			key := adminCatalogKey(r.Type, r.RewardTypeID)
			if !seen[key] {
				entries = append(entries, a.catalogByKey[key])
				seen[key] = true
			}
		}
	}
	WriteAdminJSON(w, 200, map[string]any{
		"state": "PASS", "revision": policy.Revision, "config": policy.Value.Config,
		"supported_kinds": []map[string]any{
			{"kind": "login", "label": "登录", "daily": true, "growth": true},
			{"kind": "explore", "label": "探索", "daily": true, "growth": true},
			{"kind": "level", "label": "玩家等级", "daily": false, "growth": true},
			{"kind": "collection", "label": "卡牌图鉴数量", "daily": false, "growth": true},
		},
		"limits":         map[string]int{"max_missions": maxManagedMissions, "max_title_length": 60, "max_description_length": 200, "max_target": 10000000, "max_rewards": 4},
		"reward_entries": entries, "reward_delivery": "mail_per_reward",
	})
}

func (a *API) saveMissions(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		Expected *int           `json:"expected_revision"`
		Config   *MissionPolicy `json:"config"`
	}
	if err := DecodeAdminJSONLimit(r, &body, 2*1024*1024); err != nil || body.Expected == nil || body.Config == nil || body.Config.Missions == nil {
		WriteAdminError(w, 400, "请提交完整任务清单和页面版本；移除全部任务请提交空数组")
		return
	}
	if len(body.Config.Missions) > maxManagedMissions {
		WriteAdminError(w, 400, "最多配置200项任务")
		return
	}
	for i := range body.Config.Missions {
		def := &body.Config.Missions[i]
		def.Title, def.Description = strings.TrimSpace(def.Title), strings.TrimSpace(def.Description)
		if err := validateMissionDefinition(*def); err != nil {
			WriteAdminError(w, 400, err.Error())
			return
		}
		if err := a.canonicalMissionRewards(def); err != nil {
			WriteAdminError(w, 400, err.Error())
			return
		}
	}
	o := a.operations
	o.configMu.Lock()
	defer o.configMu.Unlock()
	previous := o.missionPolicy.Load()
	if previous == nil {
		WriteAdminError(w, 503, "任务目录尚未载入")
		return
	}
	if *body.Expected != previous.Revision {
		writeContentError(w, accountstore.ErrDocumentConflict)
		return
	}
	value, err := allocateMissionPolicy(previous.Value, *body.Config)
	if err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	doc, err := o.writeDocument(missionPolicyKey, *body.Expected, value)
	if err != nil {
		writeContentError(w, err)
		return
	}
	o.missionPolicy.Store(missionSnapshot(value, doc.Revision))
	a.missions(w, r)
}

func allocateMissionPolicy(previous missionPolicyDocument, config MissionPolicy) (missionPolicyDocument, error) {
	value := missionPolicyDocument{Config: config, NextID: previous.NextID, RetiredIDs: append([]int{}, previous.RetiredIDs...)}
	active := map[int]game.MissionDefinition{}
	for _, def := range previous.Config.Missions {
		active[def.ID] = def
	}
	seen := map[int]bool{}
	for _, def := range config.Missions {
		if def.ID < 0 || def.ID > maxManagedMissionID || def.ID != 0 && seen[def.ID] {
			return missionPolicyDocument{}, errors.New("任务编号无效或重复，请重新载入")
		}
		if def.ID != 0 {
			if _, ok := active[def.ID]; !ok {
				return missionPolicyDocument{}, errors.New("任务编号已删除或不存在；新增任务请使用编号0")
			}
			seen[def.ID] = true
		}
	}
	for i := range value.Config.Missions {
		def := &value.Config.Missions[i]
		if base, exists := active[def.ID]; exists {
			delete(active, def.ID)
			if base.Kind == def.Kind && base.Daily == def.Daily {
				continue
			}
			value.RetiredIDs = append(value.RetiredIDs, def.ID)
		}
		if value.NextID > maxManagedMissionID {
			return missionPolicyDocument{}, errors.New("任务编号已用尽")
		}
		def.ID = value.NextID
		value.NextID++
	}
	for id := range active {
		value.RetiredIDs = append(value.RetiredIDs, id)
	}
	slices.Sort(value.RetiredIDs)
	if err := validateMissionDocument(value); err != nil {
		return missionPolicyDocument{}, err
	}
	return value, nil
}
