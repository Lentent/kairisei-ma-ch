package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"kairisei.local/server/internal/criaudio"
	"kairisei.local/server/internal/masterdata"
)

const customStampDraftKey = "custom-stamp-drafts"
const customStampFirstID = 980001
const customStampLastID = 989999
const customStampMasterPath = "_local/control/server/cn602-stamp-runtime-master.json"

type customStamp struct {
	ID       int    `json:"stamp_id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Category int    `json:"category"`
	Message  string `json:"message"`
	Artwork  []byte `json:"artwork,omitempty"`
	Audio    []byte `json:"audio,omitempty"`
	Volume   int    `json:"volume"`
}
type customStampDraft struct {
	Stamps []customStamp `json:"stamps"`
}

func (a *API) stampPublished() (map[int]bool, map[int]bool, error) {
	root, err := a.collectionRoot()
	if err != nil {
		return nil, nil, err
	}
	if _, err = masterdata.LoadStampRuntimeMaster(filepath.Join(root, filepath.FromSlash(customStampMasterPath))); err != nil {
		return nil, nil, err
	}
	var m struct {
		Source struct {
			IDs []int `json:"admin_custom_stamps"`
		} `json:"source"`
		Stamps []struct {
			ID int `json:"stamp_id"`
		} `json:"stamps"`
	}
	if err = readResourceJSON(root, customStampMasterPath, &m); err != nil {
		return nil, nil, err
	}
	owned, occupied := map[int]bool{}, map[int]bool{}
	for _, s := range m.Stamps {
		occupied[s.ID] = true
	}
	for _, id := range m.Source.IDs {
		if id < customStampFirstID || id > customStampLastID || !occupied[id] || owned[id] {
			return nil, nil, errors.New("自制表情发布记录无效")
		}
		owned[id] = true
	}
	return owned, occupied, nil
}
func (a *API) stampDraft() (customStampDraft, int, error) {
	doc, err := a.operations.storage.ReadDocument(customStampDraftKey)
	d := customStampDraft{Stamps: []customStamp{}}
	if err == nil && doc.Revision > 0 {
		err = json.Unmarshal(doc.Payload, &d)
	}
	return d, doc.Revision, err
}
func (a *API) customStamps(w http.ResponseWriter, r *http.Request) {
	a.operations.configMu.RLock()
	defer a.operations.configMu.RUnlock()
	published, occupied, err := a.stampPublished()
	if err != nil {
		WriteAdminError(w, 503, err.Error())
		return
	}
	d, revision, err := a.stampDraft()
	if err != nil {
		WriteAdminError(w, 500, err.Error())
		return
	}
	used := map[int]bool{}
	for id := range occupied {
		used[id] = true
	}
	for _, s := range d.Stamps {
		used[s.ID] = true
	}
	next := customStampFirstID
	for used[next] && next <= customStampLastID {
		next++
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "config": d, "revision": revision, "next_stamp_id": next, "published": published, "occupied": occupied})
}
func stampText(s string, limit int, required bool) bool {
	return (!required && s == "" || collectionText(s, limit)) && !strings.ContainsAny(s, ",\"") && !strings.ContainsAny(s, "<>")
}
func (a *API) validateCustomStamps(d *customStampDraft) error {
	if d.Stamps == nil || len(d.Stamps) > 200 {
		return errors.New("表情须为数组，最多200项")
	}
	published, occupied, err := a.stampPublished()
	if err != nil {
		return err
	}
	seen, total := map[int]bool{}, 0
	for i := range d.Stamps {
		s := &d.Stamps[i]
		s.Name, s.Message = strings.TrimSpace(s.Name), strings.TrimSpace(s.Message)
		if s.ID < customStampFirstID || s.ID > customStampLastID || seen[s.ID] || occupied[s.ID] && !published[s.ID] {
			return errors.New("表情编号重复、被占用或不在自制范围内")
		}
		seen[s.ID] = true
		if !stampText(s.Name, 40, true) || !stampText(s.Message, 100, s.Kind == "text") || s.Category < 1 || s.Category > 7 || s.Volume < 0 || s.Volume > 100 {
			return errors.New("请填写1–40字名称、有效分类和0–100音量；文字内容最多100字，不支持英文逗号、引号、标签或换行")
		}
		switch s.Kind {
		case "text":
			if len(s.Artwork) > 0 || len(s.Audio) > 0 {
				return errors.New("纯文字表情不能包含图片或声音")
			}
		case "image", "sound":
			if _, err := decodeCustomArtwork(s.Artwork); err != nil {
				return fmt.Errorf("表情图片：%w", err)
			}
			if s.Kind == "sound" {
				if _, err := criaudio.ParseWAV(s.Audio); err != nil {
					return err
				}
			} else if len(s.Audio) > 0 {
				return errors.New("带声音的表情请选择图片加声音类型")
			}
		default:
			return errors.New("表情类型须为纯文字、图片或图片加声音")
		}
		total += len(s.Artwork) + len(s.Audio)
		if total > 16<<20 {
			return errors.New("全部表情图片与转换后的声音合计最多16MB")
		}
	}
	for id := range published {
		if !seen[id] {
			return fmt.Errorf("已发布表情%d不能移除，请保留原设计后修改", id)
		}
	}
	return nil
}
func (a *API) saveCustomStamps(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		Expected *int              `json:"expected_revision"`
		Config   *customStampDraft `json:"config"`
	}
	if err := DecodeAdminJSONLimit(r, &body, 24<<20); err != nil || body.Expected == nil || body.Config == nil {
		WriteAdminError(w, 400, "请提交表情设计和版本，数据最多24MB")
		return
	}
	a.operations.configMu.Lock()
	err := a.validateCustomStamps(body.Config)
	if err != nil {
		a.operations.configMu.Unlock()
		WriteAdminError(w, 400, err.Error())
		return
	}
	_, err = a.operations.writeDocument(customStampDraftKey, *body.Expected, *body.Config)
	a.operations.configMu.Unlock()
	if err != nil {
		writeContentError(w, err)
		return
	}
	a.customStamps(w, r)
}
func (a *API) exportCustomStamps(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		Expected *int `json:"expected_revision"`
	}
	if err := decodeAdminJSON(r, &body); err != nil || body.Expected == nil {
		WriteAdminError(w, 400, "请提交已保存草稿的版本")
		return
	}
	a.operations.configMu.Lock()
	d, revision, err := a.stampDraft()
	if err == nil && revision != *body.Expected {
		a.operations.configMu.Unlock()
		WriteAdminError(w, 409, "草稿已变化，请重新载入")
		return
	}
	if err == nil {
		err = a.validateCustomStamps(&d)
	}
	if err == nil && len(d.Stamps) == 0 {
		err = errors.New("请先新增并保存表情")
	}
	var patch []byte
	if err == nil {
		patch, err = a.buildCustomStampPatch(d)
	}
	a.operations.configMu.Unlock()
	if err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=custom-stamps-v%d.zip", revision))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(patch)
}
