package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/accountstore"
)

// Clearing the body keeps the revision monotonic so an old editor cannot
// recreate a discarded draft with a reused revision after delete/recreate.
func gachaDraftExists(doc accountstore.Document) bool {
	return doc.Revision > 0 && len(doc.Payload) > 0 && !bytes.Equal(bytes.TrimSpace(doc.Payload), []byte("null"))
}

// Variants retain independent contents and payment. The cover, schedule and cumulative
// draw limit are pool-wide; saves/publishes remain one atomic group operation.

type gachaGroupExpectation struct {
	Draft  int    `json:"draft_revision"`
	Live   int    `json:"live_revision"`
	SHA256 string `json:"sha256"`
}

// gachaGroupMembers lists the editable variants of a pool in ID order.
func (operations *Operations) gachaGroupMembers(groupID int) []int {
	members := []int{}
	for id, base := range operations.gachaBases {
		if groupID > 0 && base.GroupID == groupID {
			members = append(members, id)
		}
	}
	slices.Sort(members)
	return members
}

func (operations *Operations) gachaGroupShared(configs []AdminGachaConfig) error {
	first := configs[0]
	for _, config := range configs[1:] {
		if config.PlayCountMax != first.PlayCountMax {
			return errors.New("累计限抽次数由整池共享，各分池须使用同一上限")
		}
		if config.CoverPath != first.CoverPath {
			return errors.New("同一卡池的各抽法必须使用同一封面")
		}
		if config.StartUnix != first.StartUnix || config.EndUnix != first.EndUnix {
			return errors.New("开始与结束时间由整池共享，各分池须使用同一排期")
		}
	}
	return nil
}

// gachaGroupAction saves drafts for, or publishes, every variant of one pool under a single lock: all
// revisions are checked and all variants validated before anything is written.
func (admin *API) gachaGroupAction(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		GroupID  int                           `json:"group_id"`
		Configs  []AdminGachaConfig            `json:"configs"`
		Expected map[int]gachaGroupExpectation `json:"expected"`
	}
	if err := DecodeAdminJSONLimit(r, &body, 8*1024*1024); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	action := chi.URLParam(r, "action")
	publish, discard := action == "publish", action == "discard"
	o := admin.operations
	o.configMu.Lock()
	defer o.configMu.Unlock()
	members := o.gachaGroupMembers(body.GroupID)
	if len(members) == 0 {
		WriteAdminError(w, 400, "不能编辑新手保留卡池或未知卡池")
		return
	}
	configs := make([]AdminGachaConfig, len(members))
	drafts := make(map[int]accountstore.Document, len(members))
	if !publish && !discard && len(body.Configs) != len(members) {
		WriteAdminError(w, 400, "请一次提交此卡池的全部抽法")
		return
	}
	for i, id := range members {
		if pool, custom := o.customGachas[id]; custom && pool.Deleted {
			WriteAdminError(w, 400, "卡池在回收站中，请先恢复后编辑")
			return
		}
		key := strconv.Itoa(id)
		draft, err := o.storage.ReadDocument("gacha-draft:" + key)
		if err != nil {
			WriteAdminError(w, 500, err.Error())
			return
		}
		expected, listed := body.Expected[id]
		if !listed || draft.Revision != expected.Draft {
			WriteAdminError(w, 409, accountstore.ErrDocumentConflict.Error())
			return
		}
		drafts[id] = draft
		if !publish && !discard {
			if body.Configs[i].GachaID != id {
				WriteAdminError(w, 400, "请按卡池抽法顺序提交全部抽法")
				return
			}
			configs[i] = body.Configs[i]
			continue
		}
		live, err := o.storage.ReadDocument("gacha-live:" + key)
		if err != nil {
			WriteAdminError(w, 500, err.Error())
			return
		}
		if live.Revision != expected.Live || (publish && (!gachaDraftExists(draft) || draft.SHA256 != expected.SHA256)) {
			WriteAdminError(w, 409, accountstore.ErrDocumentConflict.Error())
			return
		}
		if discard {
			continue
		}
		if err := json.Unmarshal(draft.Payload, &configs[i]); err != nil {
			WriteAdminError(w, 500, err.Error())
			return
		}
	}
	if discard {
		writes := make([]accountstore.DocumentWrite, len(members))
		for i, id := range members {
			writes[i] = accountstore.DocumentWrite{Key: "gacha-draft:" + strconv.Itoa(id), Expected: drafts[id].Revision, Value: nil, Operation: "gacha-draft-discard"}
		}
		docs, err := o.storage.WriteDocuments(writes)
		if err != nil {
			writeContentError(w, err)
			return
		}
		result := map[int]accountstore.Document{}
		for i, id := range members {
			result[id] = docs[i]
		}
		WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "documents": result})
		return
	}
	if err := o.gachaGroupShared(configs); !discard && err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	if !discard && !slices.ContainsFunc(configs, func(config AdminGachaConfig) bool { return !config.Closed }) {
		WriteAdminError(w, 400, "至少开放一种抽法；要关闭整个卡池请在「扭蛋发布」操作")
		return
	}
	previews := make(map[int]map[string]any, len(members))
	for i, config := range configs {
		preview, err := admin.validateGachaConfig(config)
		if err != nil {
			WriteAdminError(w, 400, fmt.Sprintf("抽法 %d：%v", members[i], err))
			return
		}
		if publish && !preview["publishable"].(bool) {
			WriteAdminError(w, 400, "发布被资源闭包阻止，详见预览中的卡牌 ID")
			return
		}
		previews[members[i]] = preview
	}
	writes := make([]accountstore.DocumentWrite, len(members))
	for i, config := range configs {
		id := members[i]
		key, expected := "gacha-draft:"+strconv.Itoa(id), drafts[id].Revision
		if publish {
			key, expected = "gacha-live:"+strconv.Itoa(id), body.Expected[id].Live
		}
		operation := "gacha-draft"
		if publish {
			operation = "gacha-live"
		}
		writes[i] = accountstore.DocumentWrite{Key: key, Expected: expected, Value: config, Operation: operation}
	}
	if publish {
		for _, id := range members {
			writes = append(writes, accountstore.DocumentWrite{Key: "gacha-draft:" + strconv.Itoa(id), Expected: drafts[id].Revision, Value: nil, Operation: "gacha-draft-consumed"})
		}
	}
	committed, err := o.storage.WriteDocuments(writes)
	if err != nil {
		writeContentError(w, err)
		return
	}
	documents := make(map[int]accountstore.Document, len(members))
	clearedDrafts := map[int]accountstore.Document{}
	for i, config := range configs {
		if publish {
			o.applyPublishedGacha(config)
			clearedDrafts[members[i]] = committed[len(members)+i]
		}
		documents[members[i]] = committed[i]
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "documents": documents, "drafts": clearedDrafts, "previews": previews})
}

// applyPublishedGacha swaps one variant's live configuration while the caller holds configMu.
// No account cache or player save is bulk rewritten; accounts pick it up on their next request.
func (operations *Operations) applyPublishedGacha(config AdminGachaConfig) {
	configured := operations.configuredGacha(config.GachaID, config)
	for i := range operations.gachaConfigurations {
		if operations.gachaConfigurations[i].Profile.GachaID == config.GachaID {
			operations.gachaConfigurations[i] = configured
			operations.gachaRevision++
			return
		}
	}
	operations.gachaConfigurations = append(operations.gachaConfigurations, configured)
	operations.gachaRevision++
}
