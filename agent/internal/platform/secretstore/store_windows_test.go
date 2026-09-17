//go:build windows

package secretstore

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsDPAPIRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "commandcode.dpapi")
	record := Record{
		ProviderID: "command",
		BaseURL:    "https://api.commandcode.ai/provider/v1",
		APIKey:     "cc-dpapi-test-secret",
	}
	if err := Write(path, record); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(record.APIKey)) {
		t.Fatal("protected file contains plaintext API key")
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != record {
		t.Fatalf("got %#v, want %#v", got, record)
	}
}

func TestWindowsHubDPAPIRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.dpapi")
	record := HubRecord{
		AgentID: "desktop-main",
		BaseURL: "http://100.64.0.10:8787",
		Token:   "hub-dpapi-test-secret",
	}
	if err := WriteHub(path, record); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(record.Token)) {
		t.Fatal("protected file contains plaintext hub token")
	}
	got, err := ReadHub(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != record {
		t.Fatalf("got %#v, want %#v", got, record)
	}
}
