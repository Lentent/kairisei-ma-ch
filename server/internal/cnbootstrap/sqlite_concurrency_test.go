package cnbootstrap

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// A write transaction must wait before reading, not take a stale WAL snapshot
// and then fail on its first write. Readers must remain independent of it.
func TestCNSQLiteWriterWaitsBeforeSnapshot(t *testing.T) {
	storage := &cnSaveDatabase{databasePath: filepath.Join(t.TempDir(), "存档 #1.sqlite3")}
	first, err := storage.open()
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := storage.open()
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	reader, err := storage.open()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if _, err = first.Exec("CREATE TABLE counter (n INTEGER); INSERT INTO counter VALUES (0)"); err != nil {
		t.Fatal(err)
	}
	tx, err := first.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE counter SET n=1"); err != nil {
		t.Fatal(err)
	}
	ro, err := reader.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err = ro.QueryRow("SELECT n FROM counter").Scan(&n); err != nil || n != 0 {
		t.Fatalf("concurrent reader: n=%d err=%v", n, err)
	}
	ro.Rollback()
	type begun struct {
		tx  *sql.Tx
		err error
	}
	ready := make(chan begun, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() { next, e := second.BeginTx(ctx, nil); ready <- begun{next, e} }()
	select {
	case result := <-ready:
		if result.tx != nil {
			result.tx.Rollback()
		}
		t.Fatalf("second writer began before first commit: %v", result.err)
	case <-time.After(30 * time.Millisecond):
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	result := <-ready
	if result.err != nil {
		t.Fatal(result.err)
	}
	defer result.tx.Rollback()
	if err = result.tx.QueryRow("SELECT n FROM counter").Scan(&n); err != nil || n != 1 {
		t.Fatalf("writer read stale snapshot: n=%d err=%v", n, err)
	}
	if _, err = result.tx.Exec("UPDATE counter SET n=n+1"); err != nil {
		t.Fatal(err)
	}
	if err = result.tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
