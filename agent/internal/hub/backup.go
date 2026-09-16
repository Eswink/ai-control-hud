package hub

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

func BackupDatabase(ctx context.Context, sourcePath, destinationPath string) error {
	source, err := filepath.Abs(sourcePath)
	if err != nil {
		return fmt.Errorf("resolve source database: %w", err)
	}
	destination, err := filepath.Abs(destinationPath)
	if err != nil {
		return fmt.Errorf("resolve backup destination: %w", err)
	}
	if filepath.Clean(source) == filepath.Clean(destination) {
		return errors.New("backup source and destination must differ")
	}
	info, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("inspect source database: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("source database is not a regular file")
	}
	if _, err := os.Stat(destination); err == nil {
		return errors.New("backup destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect backup destination: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}

	temporary := fmt.Sprintf("%s.tmp-%d-%d", destination, os.Getpid(), time.Now().UnixNano())
	defer os.Remove(temporary)

	db, err := sql.Open("sqlite", source)
	if err != nil {
		return fmt.Errorf("open source database: %w", err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		return fmt.Errorf("configure backup timeout: %w", err)
	}
	statement := "VACUUM INTO " + sqliteString(temporary)
	if _, err := db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("create SQLite online backup: %w", err)
	}
	if err := verifyDatabase(ctx, temporary); err != nil {
		return err
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		return fmt.Errorf("protect backup permissions: %w", err)
	}
	file, err := os.OpenFile(temporary, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open backup for fsync: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("fsync backup: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close backup after fsync: %w", err)
	}
	if _, err := os.Stat(destination); err == nil {
		return errors.New("backup destination appeared during backup")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("recheck backup destination: %w", err)
	}
	if err := os.Rename(temporary, destination); err != nil {
		return fmt.Errorf("publish backup atomically: %w", err)
	}
	return nil
}

func verifyDatabase(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("open backup for integrity check: %w", err)
	}
	defer db.Close()
	var result string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return fmt.Errorf("run backup integrity check: %w", err)
	}
	if strings.ToLower(strings.TrimSpace(result)) != "ok" {
		return fmt.Errorf("backup integrity check failed: %s", result)
	}
	return nil
}

func sqliteString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
