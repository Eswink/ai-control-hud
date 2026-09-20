package commandcode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigPathsFollowZCodeDataBaseDirAndPreferEffectiveConfig(t *testing.T) {
	home := t.TempDir()
	customBase := filepath.Join(t.TempDir(), "自定义 provider data")
	setProviderTestHome(t, home)

	settingDir := filepath.Join(home, ".zcode", "v2")
	if err := os.MkdirAll(settingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	setting := `{"dataBaseDir":` + quoteJSON(customBase) + `}`
	if err := os.WriteFile(filepath.Join(settingDir, "setting.json"), []byte(setting), 0o600); err != nil {
		t.Fatal(err)
	}

	customConfig := filepath.Join(customBase, ".zcode", "v2", "config.json")
	if err := os.MkdirAll(filepath.Dir(customConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	customBody := "\xef\xbb\xbf{\n" +
		"  // current ZCode provider config may be JSONC\n" +
		"  \"provider\": {\n" +
		"    \"custom-commandcode\": {\n" +
		"      \"name\": \"CommandCode custom root\",\n" +
		"      \"kind\": \"openai\",\n" +
		"      \"enabled\": true,\n" +
		"      \"options\": {\n" +
		"        \"baseURL\": \"https://api.commandcode.ai/provider/v1\",\n" +
		"        \"apiKey\": \"unit-test-only\"\n" +
		"      },\n" +
		"      \"models\": {\"deepseek/deepseek-v4.1-flash\": {}}\n" +
		"    }\n" +
		"  } /* trailing block comment */\n" +
		"}\n"
	if err := os.WriteFile(customConfig, []byte(customBody), 0o600); err != nil {
		t.Fatal(err)
	}

	// A fallback config exists too; the effective custom root must win.
	defaultConfig := filepath.Join(home, ".zcode", "v2", "config.json")
	defaultBody := `{"provider":{"fallback-commandcode":{"name":"fallback","kind":"openai","enabled":true,"options":{"baseURL":"https://api.commandcode.ai/provider/v1","apiKey":"fallback-test-only"},"models":{}}}}`
	if err := os.WriteFile(defaultConfig, []byte(defaultBody), 0o600); err != nil {
		t.Fatal(err)
	}

	paths := DefaultConfigPaths()
	if len(paths) < 2 {
		t.Fatalf("provider config candidates = %v", paths)
	}
	if filepath.Clean(paths[0]) != filepath.Clean(customConfig) {
		t.Fatalf("first provider config = %q, want %q", paths[0], customConfig)
	}
	if filepath.Clean(paths[1]) != filepath.Clean(defaultConfig) {
		t.Fatalf("fallback provider config = %q, want %q", paths[1], defaultConfig)
	}

	foundPath, provider, err := FindProviderAcrossConfigs(paths, "")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(foundPath) != filepath.Clean(customConfig) {
		t.Fatalf("selected provider config = %q, want %q", foundPath, customConfig)
	}
	if provider == nil || provider.ID != "custom-commandcode" {
		t.Fatalf("provider = %#v", provider)
	}
	if provider.Host() != "api.commandcode.ai" || provider.APIKey != "unit-test-only" {
		t.Fatalf("unexpected provider metadata: id=%q host=%q", provider.ID, provider.Host())
	}
	if err := ValidateProvider(provider, false); err != nil {
		t.Fatal(err)
	}
}

func TestExplicitHUDZCodeConfigRemainsHighestPrecedence(t *testing.T) {
	home := t.TempDir()
	setProviderTestHome(t, home)
	explicit := filepath.Join(t.TempDir(), "explicit-provider.json")
	t.Setenv("HUD_ZCODE_CONFIG", explicit)

	paths := DefaultConfigPaths()
	if len(paths) != 1 || filepath.Clean(paths[0]) != filepath.Clean(explicit) {
		t.Fatalf("explicit provider config did not win: %v", paths)
	}
}

func TestLoadProviderJSONCDoesNotTreatURLSlashesAsComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{
		// comment
		"provider": {
			"command": {
				"name": "CommandCode",
				"kind": "openai",
				"enabled": true,
				"options": {
					"baseURL": "https://api.commandcode.ai/provider/v1?next=//keep",
					"apiKey": "unit-test-only"
				},
				"models": {}
			}
		}
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	provider, err := LoadProvider(path, "command")
	if err != nil {
		t.Fatal(err)
	}
	if provider == nil || !strings.Contains(provider.BaseURL, "?next=//keep") {
		t.Fatalf("URL was corrupted by JSONC parsing: %#v", provider)
	}
}

func setProviderTestHome(t *testing.T, home string) {
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

func quoteJSON(value string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"\"", "\\\"",
		"\n", "\\n",
		"\r", "\\r",
		"\t", "\\t",
	)
	return "\"" + replacer.Replace(value) + "\""
}
