package httpapi

import (
	"encoding/json"
	"testing"
)

func TestPastBossUsesAccountProgress(t *testing.T) {
	source := []json.RawMessage{json.RawMessage(`{"0":7,"13":[{"0":101,"10":0},{"0":102,"10":0}]}`)}
	progress := json.RawMessage(`{"0":0,"9":[],"10":[{"10":[{"0":101,"10":2},{"0":102,"10":1}]}],"11":[],"12":[]}`)
	got, err := pastBossProgress(source, progress)
	if err != nil {
		t.Fatal(err)
	}
	var group struct {
		Bosses []struct {
			State int `json:"10"`
		} `json:"13"`
	}
	if err := json.Unmarshal(got[0], &group); err != nil {
		t.Fatal(err)
	}
	if group.Bosses[0].State != 2 || group.Bosses[1].State != 1 {
		t.Fatalf("progress %+v", group)
	}
	if string(source[0]) != `{"0":7,"13":[{"0":101,"10":0},{"0":102,"10":0}]}` {
		t.Fatal("shared archive changed")
	}
}
