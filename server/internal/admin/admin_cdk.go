package admin

import (
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/gamestate"
)

//go:embed web/admin_cdk.js
var adminCDKJS []byte

var errCDKInboxFull = errors.New("礼物箱过满，请先领取后重试")

type CDK struct {
	accountstore.CDKPolicy
	Code    string             `json:"code"`
	Mode    string             `json:"mode"`
	Title   string             `json:"title"`
	Message string             `json:"message"`
	Rewards []AdminMailRequest `json:"rewards"`
}

func normalizeCDK(code string) (string, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) < 6 || len(code) > 64 {
		return "", errors.New("兑换码须为6–64位字母、数字或短横线")
	}
	for _, c := range code {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return "", errors.New("兑换码须为6–64位字母、数字或短横线")
		}
	}
	return code, nil
}

func (admin *API) validateCDK(cdk *CDK) error {
	cdk.Title = strings.TrimSpace(cdk.Title)
	cdk.Message = strings.TrimSpace(cdk.Message)
	if len([]rune(cdk.Title)) < 1 || len([]rune(cdk.Title)) > 40 || len([]rune(cdk.Message)) < 1 || len([]rune(cdk.Message)) > 200 || len(cdk.Rewards) < 1 || len(cdk.Rewards) > 120 {
		return errors.New("填写1–40字标题、1–200字正文，选择1–120种奖励")
	}
	if cdk.Mode != "shared" && cdk.Mode != "single" {
		return errors.New("请选择通用码或一次性码")
	}
	if cdk.Mode == "single" {
		cdk.MaxUses = 1
	}
	if cdk.MaxUses < 0 || cdk.MaxUses > 10000000 || cdk.StartUnix < 0 || cdk.ExpiresUnix < 0 || cdk.ExpiresUnix != 0 && (cdk.ExpiresUnix <= cdk.StartUnix || cdk.ExpiresUnix <= time.Now().Unix()) {
		return errors.New("使用次数或有效期无效，结束时间须晚于开始时间及当前时间")
	}
	seen := map[string]bool{}
	for i, input := range cdk.Rewards {
		if input.IdempotencyKey != "" || input.Title != "" || input.Message != "" || input.Discardable {
			return errors.New("奖励仅可设置物品、数量和卡牌属性")
		}
		reward, _, err := admin.mailReward(input)
		if err != nil {
			return fmt.Errorf("奖励%d：%w", i+1, err)
		}
		key := adminCatalogKey(input.RewardType, input.RewardTypeID)
		if seen[key] {
			return errors.New("同一种奖励请合并数量")
		}
		seen[key] = true
		input.CardLevel = int(reward.CardLevel)
		input.CardFame = int(reward.CardFame)
		input.CardLove = reward.CardLove
		cdk.Rewards[i] = input
	}
	return nil
}

func (admin *API) createCDK(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var input struct {
		CDK
		Count int `json:"count"`
	}
	if err := decodeAdminJSON(r, &input); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	if input.Count == 0 {
		input.Count = 1
	}
	if input.Count < 1 || input.Count > 100 || input.Count > 1 && strings.TrimSpace(input.Code) != "" {
		WriteAdminError(w, 400, "每批生成1–100个码；指定兑换码时只能创建一个")
		return
	}
	input.Enabled = true
	if err := admin.validateCDK(&input.CDK); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	values := map[string]any{}
	codes := []string{}
	for i := 0; i < input.Count; i++ {
		code := input.Code
		if strings.TrimSpace(code) == "" {
			var bytes [16]byte
			if _, err := rand.Read(bytes[:]); err != nil {
				WriteAdminError(w, 500, "生成兑换码失败")
				return
			}
			code = strings.ToUpper(hex.EncodeToString(bytes[:]))
		}
		code, err := normalizeCDK(code)
		if err != nil {
			WriteAdminError(w, 400, err.Error())
			return
		}
		cdk := input.CDK
		cdk.Code = code
		values[code] = cdk
		codes = append(codes, code)
	}
	if err := admin.accounts.Database().CreateCDKDocuments(values); err != nil {
		if errors.Is(err, accountstore.ErrDocumentConflict) {
			WriteAdminError(w, 409, "兑换码已存在，请换一个码")
			return
		}
		WriteAdminError(w, 500, err.Error())
		return
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "codes": codes})
}

func (admin *API) readCDK(code string) (CDK, accountstore.Document, error) {
	code, err := normalizeCDK(code)
	if err != nil {
		return CDK{}, accountstore.Document{}, accountstore.ErrCDKUnavailable
	}
	doc, err := admin.accounts.Database().ReadDocument(accountstore.CDKPrefix + code)
	var cdk CDK
	if err == nil && doc.Revision == 0 {
		err = accountstore.ErrCDKUnavailable
	}
	if err == nil {
		err = json.Unmarshal(doc.Payload, &cdk)
	}
	if err == nil && cdk.Code != code {
		err = errors.New("兑换码配置不一致")
	}
	return cdk, doc, err
}

func (admin *API) listCDK(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := adminPage(r, 20, 100)
	if err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	docs, total, err := admin.accounts.Database().ListDocuments(accountstore.CDKPrefix, limit, offset)
	if err != nil {
		WriteAdminError(w, 500, err.Error())
		return
	}
	rows := []map[string]any{}
	for _, summary := range docs {
		cdk, doc, err := admin.readCDK(strings.TrimPrefix(summary.Key, accountstore.CDKPrefix))
		if err != nil {
			WriteAdminError(w, 500, err.Error())
			return
		}
		rows = append(rows, map[string]any{"cdk": cdk, "revision": doc.Revision, "used": summary.ReceiptCount, "updated_utc": doc.UpdatedUTC})
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "codes": rows, "total": total})
}

func (admin *API) setCDKEnabled(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var input struct {
		Enabled  bool `json:"enabled"`
		Revision int  `json:"revision"`
	}
	if err := decodeAdminJSON(r, &input); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	cdk, doc, err := admin.readCDK(chi.URLParam(r, "code"))
	if err != nil {
		WriteAdminError(w, 404, err.Error())
		return
	}
	if input.Revision != doc.Revision {
		WriteAdminError(w, 409, accountstore.ErrDocumentConflict.Error())
		return
	}
	cdk.Enabled = input.Enabled
	if _, err = admin.accounts.Database().WriteDocument(accountstore.CDKPrefix+cdk.Code, input.Revision, cdk, "cdk-status"); err != nil {
		status := 500
		if errors.Is(err, accountstore.ErrDocumentConflict) {
			status = 409
		}
		WriteAdminError(w, status, err.Error())
		return
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS"})
}

func (admin *API) cdkRecords(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := adminPage(r, 20, 100)
	if err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	cdk, _, err := admin.readCDK(chi.URLParam(r, "code"))
	if err != nil {
		WriteAdminError(w, 404, err.Error())
		return
	}
	rows, total, err := admin.accounts.Database().CDKRecords(cdk.Code, limit, offset)
	if err != nil {
		WriteAdminError(w, 500, err.Error())
		return
	}
	catalog := []AdminCatalogEntry{}
	for _, reward := range cdk.Rewards {
		catalog = append(catalog, admin.catalogByKey[adminCatalogKey(reward.RewardType, reward.RewardTypeID)])
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "records": rows, "total": total, "cdk": cdk, "catalog": catalog})
}

func (admin *API) redeemCDK(code string, userID int) (map[string]any, error) {
	if userID < accountstore.PrimaryUserID || userID >= accountstore.SystemPartnerUserIDBase {
		return nil, accountstore.ErrCDKUnavailable
	}
	cdk, doc, err := admin.readCDK(code)
	if err != nil {
		return nil, err
	}
	lock := admin.business.AccountLock(userID)
	lock.Lock()
	defer lock.Unlock()
	used, err := admin.accounts.Database().CDKUsedBy(cdk.Code, userID)
	if err != nil {
		return nil, err
	}
	if used {
		return nil, accountstore.ErrCDKUsed
	}
	state, err := admin.loadAccountState(userID)
	if err != nil {
		return nil, err
	}
	if len(state.Engagement.Presents)+len(cdk.Rewards) > 30000 {
		return nil, errCDKInboxFull
	}
	ids := []int64{}
	issued := time.Now().Unix()
	for i, input := range cdk.Rewards {
		reward, _, err := admin.mailReward(input)
		if err != nil {
			return nil, err
		}
		key := fmt.Sprintf("cdk:%s:%d", cdk.Code, i)
		id := adminMailPresentID(userID, key)
		for _, group := range [][]gamestate.Present{state.Engagement.Presents, state.Engagement.Histories} {
			for _, p := range group {
				if p.PresentID == id {
					return nil, errors.New("奖励邮件编号冲突，请联系管理员")
				}
			}
		}
		empty := gamestate.Reward{CardSkillLevels: []int16{}}
		state.Engagement.Presents = append(state.Engagement.Presents, gamestate.Present{PresentID: id, IssuedAtUnix: issued, Reward: reward, Reward0: empty, Reward1: empty, Reward2: empty, Title: cdk.Title, Comment: cdk.Message, AdminIdempotencyKey: key})
		ids = append(ids, id)
	}
	result := map[string]any{"user_id": userID, "code": cdk.Code, "present_ids": ids, "title": cdk.Title}
	key := accountstore.CDKPrefix + cdk.Code
	err = admin.accounts.PersistStateWithAudit(userID, state, &accountstore.AdminAudit{Operation: "cdk-redeem", Target: strconv.Itoa(userID), Payload: result, CDKClaim: &accountstore.CDKClaim{Key: key, SHA256: doc.SHA256}, Receipt: &accountstore.AdminActionReceipt{OperationKey: key, UserID: userID, RequestSHA256: doc.SHA256, Result: result}})
	if err != nil {
		return nil, err
	}
	admin.business.Invalidate(userID)
	return result, nil
}

type cdkAdminHandler struct {
	http.Handler
	public  http.Handler
	service *API
}

func (handler *cdkAdminHandler) CDKHandler() http.Handler { return handler.public }

// Only the authenticated game adapter supplies userID; this is not an HTTP route.
func (handler *cdkAdminHandler) RedeemCDK(code string, userID int) error {
	_, err := handler.service.redeemCDK(code, userID)
	return err
}
