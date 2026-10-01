package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/testfixture"
)

func TestHomeNoticeReadRestartAndNewPublication(t *testing.T) {
	state := testfixture.RuntimeState(t)
	account, err := game.New(state)
	if err != nil {
		t.Fatal(err)
	}
	a := &API{initialState: state, account: account, baseURL: "http://localhost:26020"}
	policy := game.PlayerConfiguration{Revision: 1, LoginBonus: state.LoginBonusPolicy, Notice: game.NoticePublication{Revision: 1, Enabled: true}}
	account.ApplyPlayerConfiguration(policy)
	path := func() string {
		t.Helper()
		w := httptest.NewRecorder()
		a.homeShow(w, httptest.NewRequest("POST", "/HomeShow", nil))
		parts := bytes.Split(w.Body.Bytes(), []byte{'\n'})
		if w.Code != 200 || len(parts) != 3 {
			t.Fatalf("HomeShow: %s", w.Body.String())
		}
		var body struct {
			URL string `json:"update_info_url"`
		}
		if err := json.Unmarshal(parts[1], &body); err != nil {
			t.Fatal(err)
		}
		return body.URL
	}
	first := path()
	if first == "" || path() != first {
		t.Fatal("unopened notice was consumed")
	}
	ack := func(relative string) {
		t.Helper()
		u, err := url.Parse(relative)
		if err != nil {
			t.Fatal(err)
		}
		revision, _ := strconv.Atoi(u.Query().Get("revision"))
		w := httptest.NewRecorder()
		a.popupExec(w, httptest.NewRequest("POST", "/PopupExec", bytes.NewBufferString(fmt.Sprintf(`{"popupid":%d,"is_system":1}`, 1000000000+revision))))
		if w.Code != 200 {
			t.Fatalf("ack: %s", w.Body.String())
		}
	}
	ack(first)
	if path() != "" {
		t.Fatal("read notice repeated")
	}
	restored, err := game.New(account.Snapshot(state))
	if err != nil {
		t.Fatal(err)
	}
	restored.ApplyPlayerConfiguration(policy)
	a.account = restored
	if path() != "" {
		t.Fatal("read state lost on restart")
	}
	other, err := game.New(state)
	if err != nil {
		t.Fatal(err)
	}
	other.ApplyPlayerConfiguration(policy)
	if other.UnreadNoticePath(state.User.UserID+1) == "" {
		t.Fatal("another player inherited read state")
	}
	policy.Revision++
	policy.Notice.Revision = 2
	restored.ApplyPlayerConfiguration(policy)
	second := path()
	if second == "" || second == first {
		t.Fatal("new publication did not reopen")
	}
	ack(first)
	if path() != second {
		t.Fatal("old acknowledgement consumed new publication")
	}
	ack(second)
	if path() != "" {
		t.Fatal("second publication repeats")
	}
	policy.Revision++
	policy.Notice.Revision = 3
	policy.Notice.Enabled = false
	restored.ApplyPlayerConfiguration(policy)
	if path() != "" {
		t.Fatal("disabled notice opened")
	}
	state.Onboarding.ConfigVersion = 2
	state.Onboarding.Step = 0
	newbie, err := game.New(state)
	if err != nil {
		t.Fatal(err)
	}
	policy.Revision++
	policy.Notice.Enabled = true
	newbie.ApplyPlayerConfiguration(policy)
	a.account = newbie
	if path() != "" {
		t.Fatal("notice interrupted onboarding")
	}
}
