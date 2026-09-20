package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestZCodeEvidenceReportsCustomRootProviderShapeWithoutSecretsOrPaths(t *testing.T) {
	home := t.TempDir()
	customBase := filepath.Join(t.TempDir(), "private provider root")
	setZCodeCommandTestHome(t, home)

	settingDir := filepath.Join(home, ".zcode", "v2")
	if err := os.MkdirAll(settingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	encodedBase, err := json.Marshal(customBase)
	if err != nil {
		t.Fatal(err)
	}
	setting := append([]byte("{\"dataBaseDir\":"), encodedBase...)
	setting = append(setting, '}')
	if err := os.WriteFile(filepath.Join(settingDir, "setting.json"), setting, 0o600); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(customBase, ".zcode", "v2", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	secret := "PRIVATE-PROVIDER-KEY-MUST-NOT-LEAK"
	configText := "{\n" +
		" // JSONC comment\n" +
		" \"provider\": {\n" +
		"   \"private-provider-id\": {\"options\": {\"apiKey\": \"" + secret + "\"}},\n" +
		"   \"second-private-provider\": {\"options\": {\"baseURL\": \"https://example.test/a//b\"}}\n" +
		" }\n" +
		"}\n"
	configBytes := append([]byte{0xef, 0xbb, 0xbf}, []byte(configText)...)
	if err := os.WriteFile(configPath, configBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() error {
		return runZCodeEvidence(nil)
	})

	var evidence map[string]any
	if err := json.Unmarshal(output, &evidence); err != nil {
		t.Fatalf("decode evidence: %v\n%s", err, output)
	}
	if evidence["layoutSource"] != "data_base_setting" {
		t.Fatalf("layoutSource=%v", evidence["layoutSource"])
	}
	if evidence["providerConfigCandidates"] != float64(3) ||
		evidence["providerConfigsReadable"] != float64(1) ||
		evidence["providerConfigsParsed"] != float64(1) ||
		evidence["providerEntriesFound"] != float64(2) {
		t.Fatalf("provider evidence=%v", evidence)
	}

	serialized := string(output)
	for _, private := range []string{
		secret,
		"private-provider-id",
		"second-private-provider",
		customBase,
		home,
		"example.test",
	} {
		if strings.Contains(serialized, private) {
			t.Fatalf("zcode evidence leaked %q: %s", private, serialized)
		}
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

func setZCodeCommandTestHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, name := range []string{
		"HUD_ZCODE_HOME",
		"ZCODE_HOME",
		"ZCODE_DATA_BASE_DIR",
		"HUD_ZCODE_RUNTIME_DB",
		"HUD_ZCODE_DB",
		"HUD_ZCODE_LOG_DIR",
		"HUD_ZCODE_CONFIG",
	} {
		t.Setenv(name, "")
	}
}

func captureStdout(t *testing.T, fn func() error) []byte {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = writer
	defer func() {
		os.Stdout = previous
	}()

	callErr := fn()
	closeErr := writer.Close()
	output, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if callErr != nil {
		t.Fatal(callErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if readErr != nil {
		t.Fatal(readErr)
	}
	return output
}
