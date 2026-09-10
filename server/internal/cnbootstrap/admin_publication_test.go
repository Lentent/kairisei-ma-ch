package cnbootstrap

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"kairisei.local/server/internal/release"
)

func TestCNAdminPublicationRejectsStalePages(t *testing.T) {
	for _, kind := range []string{"boss", "gacha"} {
		t.Run(kind, func(t *testing.T) {
			accounts := newFriendCapacityTestAccounts(t)
			operations, err := newCNOperationStore(accounts.storage, []release.GachaProfile{
				{GachaID: 1, GroupID: 1, PublicationKey: "water_coin"},
				{GachaID: 2, GroupID: 2, PublicationKey: "event"},
			})
			if err != nil {
				t.Fatal(err)
			}
			admin := &cnAdmin{operations: operations, knownGroups: map[int]struct{}{1: {}, 2: {}}, knownGachaGroups: operations.managedGachaGroups}
			get, put := admin.bossPolicy, admin.setBossPolicy
			key := cnTeamBattlePublicationKey
			if kind == "gacha" {
				get, put = admin.gachaPolicy, admin.setGachaPolicy
				key = cnGachaPublicationKey
			}
			call := func(method string, body map[string]any) *httptest.ResponseRecorder {
				content, err := json.Marshal(body)
				if err != nil {
					panic(err)
				}
				request := httptest.NewRequest(method, "http://localhost/api/"+kind+"-policy", bytes.NewReader(content))
				request.RemoteAddr = "127.0.0.1:12345"
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("X-Kairisei-Admin-Action", "apply")
				response := httptest.NewRecorder()
				if method == http.MethodGet {
					get(response, request)
				} else {
					put(response, request)
				}
				return response
			}
			body := func(id, revision int) map[string]any {
				value := map[string]any{"group_ids": []int{id}, "expected_revision": revision}
				if kind == "boss" {
					value["mode"] = "allowlist"
				}
				return value
			}
			read := func(response *httptest.ResponseRecorder) cnAdminPublicationState {
				t.Helper()
				var result struct {
					Publication cnAdminPublicationState `json:"publication"`
				}
				if response.Code != http.StatusOK {
					t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
				}
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				return result.Publication
			}
			initial := read(call(http.MethodGet, nil))
			if initial.Revision != 0 || (kind == "gacha" && !slices.Equal(initial.GroupIDs, []int{1})) {
				t.Fatalf("initial publication=%+v", initial)
			}
			missing := body(1, 0)
			delete(missing, "expected_revision")
			if response := call(http.MethodPut, missing); response.Code != http.StatusBadRequest {
				t.Fatalf("missing revision status=%d", response.Code)
			}
			var wg sync.WaitGroup
			responses := make([]*httptest.ResponseRecorder, 2)
			for index := range responses {
				wg.Add(1)
				go func() {
					defer wg.Done()
					responses[index] = call(http.MethodPut, body(index+1, 0))
				}()
			}
			wg.Wait()
			winner := -1
			for index, response := range responses {
				if response.Code == http.StatusOK {
					if winner != -1 {
						t.Fatal("both stale pages published")
					}
					winner = index
					published := read(response)
					if published.Revision != 1 || !slices.Equal(published.GroupIDs, []int{index + 1}) || published.UpdatedUTC == "" {
						t.Fatalf("response differs from accepted publication: %+v", published)
					}
				} else if response.Code != http.StatusConflict {
					t.Fatalf("concurrent status=%d body=%s", response.Code, response.Body.String())
				}
			}
			if winner == -1 {
				t.Fatal("neither page published")
			}
			first, err := operations.readDocument(key)
			if err != nil {
				t.Fatal(err)
			}
			secondID := 2 - winner
			second := read(call(http.MethodPut, body(secondID, 1)))
			current := read(call(http.MethodGet, nil))
			if second.Revision != 2 || current.Revision != 2 || current.UpdatedUTC != second.UpdatedUTC || !slices.Equal(current.GroupIDs, []int{secondID}) {
				t.Fatalf("current=%+v published=%+v", current, second)
			}
			// Rendering the committed snapshot must not read a later publication.
			var firstIDs []int
			if kind == "boss" {
				state, decodeErr := cnAdminPublicationFromDocument(first)
				firstIDs, err = state.GroupIDs, decodeErr
			} else {
				state, decodeErr := admin.gachaPublicationFromDocument(first)
				firstIDs, err = state.GroupIDs, decodeErr
			}
			if err != nil || !slices.Equal(firstIDs, []int{winner + 1}) {
				t.Fatalf("committed snapshot changed: ids=%v err=%v", firstIDs, err)
			}
			db, err := accounts.storage.open()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var audits int
			if err := db.QueryRow("SELECT count(*) FROM cn_admin_audit WHERE operation=? AND target=?", kind+"-policy", key).Scan(&audits); err != nil || audits != 2 {
				t.Fatalf("audit count=%d err=%v", audits, err)
			}
		})
	}
}
