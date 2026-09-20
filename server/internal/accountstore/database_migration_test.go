package accountstore

import (
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOldDatabaseIsRejectedWithoutReset(t *testing.T) {
	for _, old := range []struct {
		version    int
		legacyName bool
	}{{0, false}, {1, false}, {1, true}, {2, false}, {2, true}} {
		storage, err := OpenDatabase(filepath.Join(t.TempDir(), "save.json"), filepath.Join("..", "..", "config", "cn602-save-template.json"), slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := storage.Close(); err != nil {
				t.Error(err)
			}
		})
		path := storage.databasePath
		if old.legacyName {
			path = storage.legacyDatabasePath
		}
		database, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(fmt.Sprintf(`PRAGMA user_version = %d; CREATE TABLE preserved(value TEXT); INSERT INTO preserved VALUES ('keep')`, old.version)); err != nil {
			t.Fatal(err)
		}
		if err := database.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := storage.LoadOrImport(); err == nil || !strings.Contains(err.Error(), "unsupported") {
			t.Fatalf("old database error = %v", err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
		database, err = sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		var value string
		if err := database.QueryRow(`SELECT value FROM preserved`).Scan(&value); err != nil || value != "keep" {
			t.Fatalf("old database was altered: %q %v", value, err)
		}
		database.Close()
	}
}

func TestCurrentSchemaIsValidatedAtStartupNotRepaired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "save.json")
	storage, err := OpenDatabase(path, "unused-seed.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	if err := storage.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DROP TABLE cn_account_credentials"); err != nil {
		t.Fatal(err)
	}
	// Once initialized, requests do not scan or recreate schema behind the
	// operator's back. A fresh owner must detect the broken current schema.
	if err := storage.EnsureSchema(); err != nil {
		t.Fatalf("schema was rescanned: %v", err)
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenDatabase(path, "unused-seed.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if err := reopened.EnsureSchema(); err == nil || !strings.Contains(err.Error(), "cn_account_credentials") {
		t.Fatalf("missing schema was accepted: %v", err)
	}
	db, err = reopened.Open()
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='cn_account_credentials'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("missing table was recreated: %d %v", count, err)
	}
}
