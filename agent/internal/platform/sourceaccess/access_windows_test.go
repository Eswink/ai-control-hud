//go:build windows

package sourceaccess

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureServiceReadable(t *testing.T) {
	root := t.TempDir()
	db := filepath.Join(root, "db.sqlite")
	wal := db + "-wal"
	if err := os.WriteFile(db, []byte("db"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wal, []byte("wal"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureServiceReadable(db); err != nil {
		t.Fatal(err)
	}
}
