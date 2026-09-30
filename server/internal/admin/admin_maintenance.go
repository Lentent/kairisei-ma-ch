package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"kairisei.local/server/internal/accountstore"
)

const maintenanceKey = "server-maintenance"

type maintenanceState struct {
	Enabled       bool        `json:"enabled"`
	Revision      int         `json:"revision"`
	GameRequests  int         `json:"game_requests"`
	AdminRequests int         `json:"admin_requests"`
	Busy          bool        `json:"busy"`
	Cleanup       *cleanupJob `json:"cleanup,omitempty"`
}

type cleanupJob struct {
	GroupID     int                                    `json:"group_id,omitempty"`
	Scope       string                                 `json:"scope"`
	Mode        string                                 `json:"mode"`
	State       string                                 `json:"state"`
	Error       string                                 `json:"error,omitempty"`
	Report      *accountstore.OperationalCleanupReport `json:"report,omitempty"`
	AuditReport *accountstore.AuditCleanupReport       `json:"audit_report,omitempty"`
}

type maintenanceGate struct {
	mu     sync.Mutex
	state  maintenanceState
	cancel context.CancelFunc
	done   chan struct{}
}

func (o *Operations) loadMaintenance() error {
	doc, err := o.storage.ReadDocument(maintenanceKey)
	if err != nil {
		return err
	}
	var saved struct {
		Enabled bool `json:"enabled"`
	}
	if doc.Revision > 0 {
		if err := json.Unmarshal(doc.Payload, &saved); err != nil {
			return err
		}
	}
	o.maintenance.state = maintenanceState{Enabled: saved.Enabled, Revision: doc.Revision}
	return nil
}

func (o *Operations) MaintenanceState() maintenanceState {
	o.maintenance.mu.Lock()
	defer o.maintenance.mu.Unlock()
	return o.maintenance.state
}

// Admission and its active count share a lock: cleanup cannot race the last
// accepted request, including registration before an account lock exists.
func (o *Operations) AdmitGameRequest() (func(), bool) {
	m := &o.maintenance
	m.mu.Lock()
	if m.state.Enabled {
		m.mu.Unlock()
		return nil, false
	}
	m.state.GameRequests++
	m.mu.Unlock()
	return func() { m.mu.Lock(); m.state.GameRequests--; m.mu.Unlock() }, true
}

func (a *API) maintenanceWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.URL.Path == "/api/maintenance" || r.URL.Path == "/api/maintenance/cleanup" {
			next.ServeHTTP(w, r)
			return
		}
		m := &a.operations.maintenance
		m.mu.Lock()
		if m.state.Busy {
			m.mu.Unlock()
			WriteAdminError(w, 409, "正在检查或清理运营状态，请完成后再修改配置／账号")
			return
		}
		m.state.AdminRequests++
		m.mu.Unlock()
		defer func() { m.mu.Lock(); m.state.AdminRequests--; m.mu.Unlock() }()
		next.ServeHTTP(w, r)
	})
}

func (a *API) getMaintenance(w http.ResponseWriter, _ *http.Request) {
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "maintenance": a.operations.MaintenanceState(), "rooms": a.multiplayerHub.RoomCount(), "gacha_trash": a.operations.gachaTrash()})
}

func (a *API) setMaintenance(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		Enabled  bool `json:"enabled"`
		Expected *int `json:"expected_revision"`
	}
	if decodeAdminJSON(r, &body) != nil || body.Expected == nil {
		WriteAdminError(w, 400, "缺少维护状态或版本，请刷新后重试")
		return
	}
	m := &a.operations.maintenance
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Busy || *body.Expected != m.state.Revision {
		WriteAdminError(w, 409, "维护状态已变化或清理正在进行，请刷新后重试")
		return
	}
	if err := a.multiplayerHub.SetMaintenance(body.Enabled); err != nil {
		WriteAdminError(w, 409, err.Error())
		return
	}
	doc, err := a.operations.storage.WriteDocument(maintenanceKey, m.state.Revision, struct {
		Enabled bool `json:"enabled"`
	}{body.Enabled}, maintenanceKey)
	if err != nil {
		_ = a.multiplayerHub.SetMaintenance(m.state.Enabled)
		writeContentError(w, err)
		return
	}
	m.state.Enabled, m.state.Revision = body.Enabled, doc.Revision
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "maintenance": m.state})
}

func (o *Operations) beginCleanup() (func(), error) {
	m := &o.maintenance
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.state.Enabled {
		return nil, errors.New("请先开启服务器维护模式")
	}
	if m.state.Busy || m.state.GameRequests != 0 || m.state.AdminRequests != 0 {
		return nil, errors.New("仍有请求正在处理，请稍后刷新再试")
	}
	m.state.Busy = true
	return func() { m.mu.Lock(); m.state.Busy = false; m.mu.Unlock() }, nil
}

func (o *Operations) CloseMaintenance() {
	m := &o.maintenance
	m.mu.Lock()
	cancel, done := m.cancel, m.done
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (a *API) cleanupReferences(ctx context.Context) (accountstore.OperationalReferences, error) {
	refs := accountstore.OperationalReferences{Gachas: map[int]bool{}, Items: map[int]bool{}, Events: map[int]bool{}, Trades: map[int]bool{}}
	o := a.operations
	catalog, err := o.storage.CatalogState()
	if err != nil {
		return refs, err
	}
	for _, g := range catalog.Gachas {
		refs.Gachas[g.GachaID] = true
	}
	for id := range o.gachaBases {
		refs.Gachas[id] = true
	}
	ids, err := o.storage.RetainedGachaDocumentIDs(ctx)
	if err != nil {
		return refs, err
	}
	for _, id := range ids {
		refs.Gachas[id] = true
	}
	for _, tab := range catalog.ItemShopTabs {
		for _, line := range tab.Lineup {
			refs.Items[line.LineupID] = true
		}
	}
	for _, line := range o.runtimeSettings.ItemShop {
		refs.Items[line.LineupID] = true
	}
	for _, shop := range catalog.EventShopProfiles {
		for _, line := range shop.Lineups {
			refs.Events[line.LineupID] = true
		}
	}
	for _, shop := range catalog.TradeShopProfiles {
		for _, line := range shop.Lineups {
			refs.Trades[line.LineupID] = true
		}
	}
	if o.content == nil {
		return refs, errors.New("运营目录尚未完整载入，不能判断无引用状态")
	}
	for _, shop := range o.content.shops {
		for _, line := range shop.Lineups {
			refs.Trades[line.LineupID] = true
		}
	}
	return refs, nil
}

func (a *API) cleanupOperationalState(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		Scope    string `json:"scope"`
		Mode     string `json:"mode"`
		Digest   string `json:"digest"`
		StartUTC string `json:"start_utc"`
		EndUTC   string `json:"end_utc"`
		GroupID  int    `json:"group_id"`
	}
	if decodeAdminJSON(r, &body) != nil || (body.Mode != "preview" && body.Mode != "apply") || (body.Mode == "apply" && len(body.Digest) != 64) {
		WriteAdminError(w, 400, "请先预览并提交对应清理摘要")
		return
	}
	if body.Scope == "" {
		body.Scope = "operational"
	}
	if body.Scope != "operational" && body.Scope != "audit" && body.Scope != "gacha" {
		WriteAdminError(w, 400, "不支持的清理范围")
		return
	}
	if body.Scope == "gacha" && body.GroupID <= 0 {
		WriteAdminError(w, 400, "请选择回收站中的卡池")
		return
	}
	if body.Scope == "audit" {
		var err error
		body.StartUTC, body.EndUTC, err = accountstore.AuditCleanupRange(body.StartUTC, body.EndUTC)
		if err != nil {
			WriteAdminError(w, 400, err.Error())
			return
		}
	}
	runtime, ok := a.business.(interface {
		WithAccountMaintenance(context.Context, func() error) error
	})
	if !ok {
		WriteAdminError(w, 503, "当前运行入口不支持维护清理")
		return
	}
	finish, err := a.operations.beginCleanup()
	if err != nil {
		WriteAdminError(w, 409, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	m := &a.operations.maintenance
	m.mu.Lock()
	m.cancel = cancel
	m.done = make(chan struct{})
	done := m.done
	m.state.Cleanup = &cleanupJob{Scope: body.Scope, Mode: body.Mode, State: "running", GroupID: body.GroupID}
	m.mu.Unlock()
	go func() {
		defer close(done)
		defer cancel()
		defer finish()
		job := &cleanupJob{Scope: body.Scope, Mode: body.Mode, State: "complete", GroupID: body.GroupID}
		err := runtime.WithAccountMaintenance(ctx, func() error {
			expected := ""
			if body.Mode == "apply" {
				expected = body.Digest
			}
			if body.Scope == "gacha" {
				report, err := a.purgeGacha(ctx, body.GroupID, expected)
				job.Report = &report
				return err
			}
			a.operations.configMu.RLock()
			defer a.operations.configMu.RUnlock()
			if body.Scope == "audit" {
				report, err := a.operations.storage.CleanupAuditHistory(ctx, body.StartUTC, body.EndUTC, expected)
				job.AuditReport = &report
				return err
			}
			refs, err := a.cleanupReferences(ctx)
			if err != nil {
				return err
			}
			report, err := a.operations.storage.CleanupOperationalState(ctx, refs, expected)
			job.Report = &report
			return err
		})
		if err != nil {
			job.State = "failed"
			job.Error = err.Error()
		}
		m.mu.Lock()
		m.state.Cleanup = job
		m.mu.Unlock()
	}()
	WriteAdminJSON(w, http.StatusAccepted, map[string]any{"state": "PASS", "maintenance": a.operations.MaintenanceState()})
}
