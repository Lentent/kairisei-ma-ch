package cnbootstrap

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"kairisei.local/server/internal/release"
)

func TestAdminMailBatchAtomicRetryAndRestart(t *testing.T) {
	accounts := newFriendCapacityTestAccounts(t)
	ids := []int{}
	for i := 1; i <= 2; i++ {
		a, err := accounts.resolveLogin(fmt.Sprintf("00000000-0000-4000-8000-%012d", i))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, a.UserID)
	}
	operations, err := newCNOperationStore(accounts.storage, nil)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := loadCNPlayerProgressionRuntimeMaster(filepath.Join("..", "..", "config", "cn602-player-progression-runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	admin := &cnAdmin{progression: policy, accounts: accounts, operations: operations, business: newCNAccountBusinessRouter(nil, nil), catalogByKey: map[string]cnAdminCatalogEntry{
		"4:0": {Kind: "currency", RewardType: 4, Name: "金币"}, "10:0": {Kind: "currency", RewardType: 10, Name: "水晶"},
	}}
	batch := cnAdminMailBatch{ID: "test-mail-batch-1", Title: "赠礼", Message: "感谢游玩", UserIDs: ids, Rewards: []cnAdminMailRequest{{RewardType: 4, Quantity: 100}, {RewardType: 10, Quantity: 50}}}
	if _, err := admin.validateMailBatch(&batch); err != nil {
		t.Fatal(err)
	}
	doc, err := operations.writeDocument(cnAdminBatchPrefix+batch.ID, 0, batch)
	if err != nil {
		t.Fatal(err)
	}
	db, err := accounts.storage.open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// A failure after snapshot/receipt writes must roll the whole player back.
	if _, err := db.Exec(`CREATE TRIGGER fail_batch_audit BEFORE INSERT ON cn_admin_audit WHEN NEW.operation='mail-delivery' BEGIN SELECT RAISE(ABORT,'probe'); END`); err != nil {
		t.Fatal(err)
	}
	before, err := accounts.loadState(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.deliverMailBatchUser(batch, doc, ids[0]); err == nil {
		t.Fatal("expected injected failure")
	}
	after, err := accounts.loadState(ids[0])
	if err != nil || len(after.Engagement.Presents) != len(before.Engagement.Presents) {
		t.Fatal("failed batch partly delivered")
	}
	if receipt, err := operations.actionReceipt(cnAdminBatchPrefix+batch.ID, ids[0], doc.SHA256); err != nil || receipt != nil {
		t.Fatal("failed delivery wrote a receipt")
	}
	if _, err := db.Exec(`DROP TRIGGER fail_batch_audit`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := admin.deliverMailBatchUser(batch, doc, ids[i%len(ids)]); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for _, id := range ids {
		state, err := accounts.loadState(id)
		if err != nil {
			t.Fatal(err)
		}
		if len(state.Engagement.Presents) != len(before.Engagement.Presents)+2 {
			t.Fatal("concurrent retry duplicated or lost rewards")
		}
		// Player collects/deletes all the delivered mail; the durable receipt
		// must still suppress replay, independent of bounded gift history.
		state.Engagement.Presents = []release.Present{}
		state.Engagement.Histories = []release.Present{}
		if err := accounts.persistState(id, state); err != nil {
			t.Fatal(err)
		}
	}
	admin.accounts = &cnAccountStore{storage: accounts.storage}
	admin.business = newCNAccountBusinessRouter(nil, nil)
	admin.operations, err = newCNOperationStore(accounts.storage, nil)
	if err != nil {
		t.Fatal(err)
	}
	loaded, receipt, err := admin.readMailBatch(batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if _, err := admin.deliverMailBatchUser(loaded, receipt, id); err != nil {
			t.Fatal(err)
		}
		state, err := accounts.loadState(id)
		if err != nil || len(state.Engagement.Presents) != 0 {
			t.Fatal("restart replay issued mail again")
		}
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM cn_admin_audit WHERE operation='mail-delivery'`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("delivery audit count=%d err=%v", count, err)
	}
}
