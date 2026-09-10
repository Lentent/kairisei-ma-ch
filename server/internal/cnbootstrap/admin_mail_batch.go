package cnbootstrap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/release"
)

const cnAdminBatchPrefix = "mail-batch:"

type cnAdminMailBatch struct {
	ID      string               `json:"batch_id"`
	UserIDs []int                `json:"user_ids"`
	Title   string               `json:"title"`
	Message string               `json:"message"`
	Rewards []cnAdminMailRequest `json:"rewards"`
}

func validCNBatchID(id string) bool {
	if len(id) < 8 || len(id) > 80 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

func (admin *cnAdmin) validateMailBatch(batch *cnAdminMailBatch) ([]cnAdminCatalogEntry, error) {
	batch.Title, batch.Message = strings.TrimSpace(batch.Title), strings.TrimSpace(batch.Message)
	if !validCNBatchID(batch.ID) || len(batch.UserIDs) < 1 || len(batch.UserIDs) > 500 || len(batch.Rewards) < 1 || len(batch.Rewards) > 120 || len([]rune(batch.Title)) < 1 || len([]rune(batch.Title)) > 40 || len([]rune(batch.Message)) < 1 || len([]rune(batch.Message)) > 200 {
		return nil, errors.New("请填写标题、正文，选择1–500名玩家和1–120种奖励")
	}
	slices.Sort(batch.UserIDs)
	batch.UserIDs = slices.Compact(batch.UserIDs)
	db, err := admin.accounts.storage.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	args := make([]any, 0, len(batch.UserIDs))
	for _, id := range batch.UserIDs {
		if id < cnPrimaryUserID || id >= cnSystemPartnerUserIDBase {
			return nil, fmt.Errorf("玩家 #%d 无效或属于系统伙伴", id)
		}
		args = append(args, id)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM cn_local_account WHERE user_id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")+`)`, args...).Scan(&count); err != nil {
		return nil, err
	}
	if count != len(batch.UserIDs) {
		return nil, errors.New("收件名单包含不存在的玩家，请重新选择")
	}
	catalog := make([]cnAdminCatalogEntry, 0, len(batch.Rewards))
	seen := make(map[string]bool)
	for i, input := range batch.Rewards {
		// Title, content and IDs belong to the batch, never to an item.
		if input.Discardable || input.IdempotencyKey != "" || input.Title != "" || input.Message != "" {
			return nil, errors.New("批量奖励只能配置物品、数量及卡牌属性")
		}
		reward, entry, err := admin.mailReward(input)
		if err != nil {
			return nil, fmt.Errorf("第%d种奖励：%w", i+1, err)
		}
		key := cnAdminCatalogKey(reward.Type, reward.RewardTypeID)
		if seen[key] {
			return nil, errors.New("同一种奖励请合并数量后发放")
		}
		seen[key] = true
		input.CardLevel, input.CardFame, input.CardLove = int(reward.CardLevel), int(reward.CardFame), reward.CardLove
		batch.Rewards[i] = input
		catalog = append(catalog, entry)
	}
	return catalog, nil
}

func (admin *cnAdmin) previewMailBatch(w http.ResponseWriter, r *http.Request) {
	if err := requireCNAdminMutation(r); err != nil {
		writeCNAdminError(w, 403, err.Error())
		return
	}
	var batch cnAdminMailBatch
	if err := decodeCNAdminJSON(r, &batch); err != nil {
		writeCNAdminError(w, 400, err.Error())
		return
	}
	catalog, err := admin.validateMailBatch(&batch)
	if err != nil {
		writeCNAdminError(w, 400, err.Error())
		return
	}
	writeCNAdminJSON(w, 200, map[string]any{"state": "PASS", "batch": batch, "catalog": catalog, "recipient_count": len(batch.UserIDs), "mail_count": len(batch.UserIDs) * len(batch.Rewards)})
}

func (admin *cnAdmin) createMailBatch(w http.ResponseWriter, r *http.Request) {
	if err := requireCNAdminMutation(r); err != nil {
		writeCNAdminError(w, 403, err.Error())
		return
	}
	var batch cnAdminMailBatch
	if err := decodeCNAdminJSON(r, &batch); err != nil {
		writeCNAdminError(w, 400, err.Error())
		return
	}
	if _, err := admin.validateMailBatch(&batch); err != nil {
		writeCNAdminError(w, 400, err.Error())
		return
	}
	content, _ := json.Marshal(batch)
	doc, err := admin.operations.writeDocument(cnAdminBatchPrefix+batch.ID, 0, batch)
	if errors.Is(err, errCNAdminConflict) {
		doc, err = admin.operations.readDocument(cnAdminBatchPrefix + batch.ID)
		if err == nil && !bytes.Equal(doc.Payload, content) {
			writeCNAdminError(w, 409, "批次编号已用于其他内容，请重新预览")
			return
		}
	}
	if err != nil {
		writeCNAdminError(w, 500, err.Error())
		return
	}
	writeCNAdminJSON(w, 200, map[string]any{"state": "PASS", "batch_id": batch.ID, "created_utc": doc.UpdatedUTC})
}

func (admin *cnAdmin) readMailBatch(id string) (cnAdminMailBatch, cnAdminDocument, error) {
	if !validCNBatchID(id) {
		return cnAdminMailBatch{}, cnAdminDocument{}, errors.New("批次编号无效")
	}
	doc, err := admin.operations.readDocument(cnAdminBatchPrefix + id)
	var batch cnAdminMailBatch
	if err == nil && doc.Revision == 0 {
		err = errors.New("批次不存在")
	}
	if err == nil {
		err = json.Unmarshal(doc.Payload, &batch)
	}
	return batch, doc, err
}

func (admin *cnAdmin) mailBatchStatus(w http.ResponseWriter, r *http.Request) {
	batch, doc, err := admin.readMailBatch(chi.URLParam(r, "batchID"))
	if err != nil {
		writeCNAdminError(w, 404, err.Error())
		return
	}
	db, err := admin.operations.storage.open()
	if err != nil {
		writeCNAdminError(w, 500, err.Error())
		return
	}
	defer db.Close()
	rows, err := db.Query(`SELECT user_id,request_sha256 FROM cn_admin_action_receipt WHERE operation_key=?`, cnAdminBatchPrefix+batch.ID)
	if err != nil {
		writeCNAdminError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	seen := map[int]bool{}
	for rows.Next() {
		var id int
		var sha string
		if err := rows.Scan(&id, &sha); err != nil {
			writeCNAdminError(w, 500, err.Error())
			return
		}
		if sha != doc.SHA256 {
			writeCNAdminError(w, 500, "批次收据与请求不符")
			return
		}
		seen[id] = true
	}
	if err := rows.Err(); err != nil {
		writeCNAdminError(w, 500, err.Error())
		return
	}
	done, pending := []int{}, []int{}
	for _, id := range batch.UserIDs {
		if seen[id] {
			done = append(done, id)
		} else {
			pending = append(pending, id)
		}
	}
	writeCNAdminJSON(w, 200, map[string]any{"state": "PASS", "batch": batch, "created_utc": doc.UpdatedUTC, "done": done, "pending": pending})
}

func (admin *cnAdmin) deliverMailBatchUser(batch cnAdminMailBatch, doc cnAdminDocument, userID int) (json.RawMessage, error) {
	lock := admin.business.accountLock(userID)
	lock.Lock()
	defer lock.Unlock()
	key := cnAdminBatchPrefix + batch.ID
	if receipt, err := admin.operations.actionReceipt(key, userID, doc.SHA256); err != nil || receipt != nil {
		return receipt, err
	}
	state, err := admin.loadAccountState(userID)
	if err != nil {
		return nil, err
	}
	if len(state.Engagement.Presents)+len(batch.Rewards) > 30000 {
		return nil, errors.New("礼物箱过满，请先领取后重试")
	}
	presentIDs := make([]int64, 0, len(batch.Rewards))
	issued := time.Now().Unix()
	for i, input := range batch.Rewards {
		reward, _, err := admin.mailReward(input)
		if err != nil {
			return nil, err
		}
		itemKey := fmt.Sprintf("%s:%d", batch.ID, i)
		presentID := cnAdminMailPresentID(userID, itemKey)
		for _, list := range [][]release.Present{state.Engagement.Presents, state.Engagement.Histories} {
			for _, existing := range list {
				if existing.PresentID == presentID {
					return nil, errors.New("礼物编号冲突，请核对原批次")
				}
			}
		}
		empty := release.Reward{CardSkillLevels: []int16{}}
		state.Engagement.Presents = append(state.Engagement.Presents, release.Present{PresentID: presentID, IssuedAtUnix: issued, Reward: reward, Reward0: empty, Reward1: empty, Reward2: empty, Title: batch.Title, Comment: batch.Message, AdminIdempotencyKey: itemKey})
		presentIDs = append(presentIDs, presentID)
	}
	result := map[string]any{"user_id": userID, "present_ids": presentIDs, "batch_id": batch.ID}
	if err := admin.accounts.persistStateWithAudit(userID, state, &cnAdminAudit{
		Operation: "mail-delivery", Target: strconv.Itoa(userID), Payload: result,
		Receipt: &cnAdminActionReceipt{OperationKey: key, UserID: userID, RequestSHA256: doc.SHA256, Result: result},
	}); err != nil {
		return nil, err
	}
	admin.business.invalidate(userID)
	content, err := json.Marshal(result)
	return content, err
}

func (admin *cnAdmin) runMailBatch(w http.ResponseWriter, r *http.Request) {
	if err := requireCNAdminMutation(r); err != nil {
		writeCNAdminError(w, 403, err.Error())
		return
	}
	batch, doc, err := admin.readMailBatch(chi.URLParam(r, "batchID"))
	if err != nil {
		writeCNAdminError(w, 404, err.Error())
		return
	}
	var body struct {
		UserIDs []int `json:"user_ids"`
	}
	if err := decodeCNAdminJSON(r, &body); err != nil || len(body.UserIDs) < 1 || len(body.UserIDs) > 10 {
		writeCNAdminError(w, 400, "每次推进1–10名玩家")
		return
	}
	for _, id := range body.UserIDs {
		if !slices.Contains(batch.UserIDs, id) {
			writeCNAdminError(w, 400, "玩家不属于该批次")
			return
		}
	}
	results := make([]map[string]any, 0, len(body.UserIDs))
	for _, id := range body.UserIDs {
		if r.Context().Err() != nil {
			break
		}
		result, err := admin.deliverMailBatchUser(batch, doc, id)
		if err != nil {
			results = append(results, map[string]any{"user_id": id, "ok": false, "error": err.Error()})
		} else {
			results = append(results, map[string]any{"user_id": id, "ok": true, "result": result})
		}
	}
	writeCNAdminJSON(w, 200, map[string]any{"state": "PASS", "results": results})
}

func (admin *cnAdmin) listMailBatches(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := cnAdminPage(r, 20, 100)
	if err != nil {
		writeCNAdminError(w, 400, err.Error())
		return
	}
	db, err := admin.operations.storage.open()
	if err != nil {
		writeCNAdminError(w, 500, err.Error())
		return
	}
	defer db.Close()
	var total int
	if err := db.QueryRow(`SELECT count(*) FROM cn_global_operation WHERE operation_key LIKE 'mail-batch:%'`).Scan(&total); err != nil {
		writeCNAdminError(w, 500, err.Error())
		return
	}
	rows, err := db.Query(`SELECT operation_key,updated_utc,payload_json,
		(SELECT count(*) FROM cn_admin_action_receipt WHERE operation_key=op.operation_key)
		FROM cn_global_operation op WHERE operation_key LIKE 'mail-batch:%' ORDER BY updated_utc DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		writeCNAdminError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var key, created string
		var raw []byte
		var done int
		var batch cnAdminMailBatch
		if err := rows.Scan(&key, &created, &raw, &done); err != nil {
			writeCNAdminError(w, 500, err.Error())
			return
		}
		if err := json.Unmarshal(raw, &batch); err != nil {
			writeCNAdminError(w, 500, err.Error())
			return
		}
		result = append(result, map[string]any{"batch_id": batch.ID, "title": batch.Title, "created_utc": created, "recipients": len(batch.UserIDs), "rewards": len(batch.Rewards), "done": done})
	}
	if err := rows.Err(); err != nil {
		writeCNAdminError(w, 500, err.Error())
		return
	}
	writeCNAdminJSON(w, 200, map[string]any{"state": "PASS", "batches": result, "total": total, "limit": limit, "offset": offset})
}
