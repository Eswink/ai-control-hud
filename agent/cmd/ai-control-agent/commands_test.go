package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyExecutableReplacesExistingAndHandlesSamePath(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.exe")
	target := filepath.Join(root, "installed", "agent.exe")
	if err := os.WriteFile(source, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyExecutable(source, target); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-binary" {
		t.Fatalf("target = %q", got)
	}
	if err := copyExecutable(target, target); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != "new-binary" {
		t.Fatalf("same-path copy corrupted target: %q", after)
	}
	if _, err := os.Stat(target + ".new"); !os.IsNotExist(err) {
		t.Fatalf("staging file remains: %v", err)
	}
	if _, err := os.Stat(target + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("backup file remains: %v", err)
	}
}
