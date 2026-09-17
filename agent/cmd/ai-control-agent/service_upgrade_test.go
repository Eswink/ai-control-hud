package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUpgradeFileTransactionCommitAndRollback(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "new.exe")
	target := filepath.Join(root, "installed.exe")
	if err := os.WriteFile(source, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	staged, err := stageUpgradeExecutable(source, target)
	if err != nil {
		t.Fatal(err)
	}
	assertFileContents(t, target, "old-binary")
	assertFileContents(t, staged, "new-binary")

	backup, err := swapUpgradeExecutable(target, staged)
	if err != nil {
		t.Fatal(err)
	}
	assertFileContents(t, target, "new-binary")
	assertFileContents(t, backup, "old-binary")

	if err := rollbackUpgradeExecutable(target, backup); err != nil {
		t.Fatal(err)
	}
	assertFileContents(t, target, "old-binary")
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("backup still exists after rollback: %v", err)
	}
}

func TestSwapUpgradeExecutableRestoresOldTargetWhenStagedFileMissing(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "installed.exe")
	missing := filepath.Join(root, "missing.new")
	if err := os.WriteFile(target, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := swapUpgradeExecutable(target, missing); err == nil {
		t.Fatal("swap unexpectedly succeeded with missing staged file")
	}
	assertFileContents(t, target, "old-binary")
	if _, err := os.Stat(target + ".upgrade.bak"); !os.IsNotExist(err) {
		t.Fatalf("rollback backup unexpectedly remains after failed commit: %v", err)
	}
}

func TestStageUpgradeExecutableRejectsInstalledSourceAndStaleBackup(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "installed.exe")
	if err := os.WriteFile(target, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := stageUpgradeExecutable(target, target); err == nil {
		t.Fatal("installed executable was accepted as its own upgrade source")
	}

	source := filepath.Join(root, "new.exe")
	if err := os.WriteFile(source, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target+".upgrade.bak", []byte("recovery-copy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := stageUpgradeExecutable(source, target); err == nil {
		t.Fatal("stale upgrade backup was overwritten")
	}
}

func assertFileContents(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, string(data), want)
	}
}
