package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProviderConfigEvidenceReturnsCountsOnlyForJSONC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	secret := "PRIVATE-PROVIDER-KEY-MUST-NOT-LEAK"
	body := "ï»¿{\n" +
		" // comment\n" +
		" \"provider\": {\n" +
		"   \"one\": {\"options\": {\"apiKey\": \"" + secret + "\"}},\n" +
		"   \"two\": {\"options\": {\"baseURL\": \"https://example.test/a//b\"}}\n" +
		" }\n" +
		"}\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	parsed, entries := providerConfigEvidence(path)
	if !parsed || entries != 2 {
		t.Fatalf("provider evidence parsed=%t entries=%d", parsed, entries)
	}
	result := strings.TrimSpace(
		"parsed=" + map[bool]string{true: "true", false: "false"}[parsed],
	)
	if strings.Contains(result, secret) {
		t.Fatal("provider evidence leaked secret")
	}
}

func TestProviderConfigEvidenceRejectsMalformedOrOversizedFiles(t *testing.T) {
	malformed := filepath.Join(t.TempDir(), "malformed.json")
	if err := os.WriteFile(malformed, []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if parsed, entries := providerConfigEvidence(malformed); parsed || entries != 0 {
		t.Fatalf("malformed config unexpectedly parsed: %t %d", parsed, entries)
	}

	oversized := filepath.Join(t.TempDir(), "oversized.json")
	file, err := os.Create(oversized)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxProviderEvidenceBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if parsed, entries := providerConfigEvidence(oversized); parsed || entries != 0 {
		t.Fatalf("oversized config unexpectedly parsed: %t %d", parsed, entries)
	}
}
