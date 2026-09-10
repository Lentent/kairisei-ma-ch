package cnbootstrap

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
	}{{1, false}, {1, true}, {2, false}, {2, true}} {
		storage, err := newCNSaveDatabase(filepath.Join(t.TempDir(), "save.json"), filepath.Join("..", "..", "config", "cn602-save-template.json"), slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal(err)
		}
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
		if _, err := storage.loadOrImport(); err == nil || !strings.Contains(err.Error(), "unsupported") {
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
