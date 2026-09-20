package zcodepath

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveDefaultLayout(t *testing.T) {
	home := t.TempDir()
	layout := resolve(home, envMap(nil), missingReader)

	root := filepath.Join(home, ".zcode")
	if layout.Source != "default" {
		t.Fatalf("source=%q", layout.Source)
	}
	assertPath(t, layout.V2Root, filepath.Join(root, "v2"))
	assertPath(t, layout.CLIRoot, filepath.Join(root, "cli"))
	assertPath(t, layout.RuntimeDB, filepath.Join(root, "cli", "db", "db.sqlite"))
	assertPath(t, layout.LogDir, filepath.Join(root, "cli", "log"))
	assertPath(t, layout.TaskIndexDB, filepath.Join(root, "v2", "tasks-index.sqlite"))
}

func TestResolveDataBaseDirFromJSONCSetting(t *testing.T) {
	home := t.TempDir()
	custom := filepath.Join(t.TempDir(), "自定义 data root")
	setting := filepath.Join(home, ".zcode", "v2", "setting.json")
	jsonPath := strings.ReplaceAll(custom, "\\", "\\\\")
	reader := exactReader(map[string][]byte{
		filepath.Clean(setting): []byte("\ufeff{\n // desktop setting\n \"dataBaseDir\": \"" + jsonPath + "\", /* keep */\n \"unknown\": true\n}\n"),
	})

	layout := resolve(home, envMap(nil), reader)
	if layout.Source != "data_base_setting" {
		t.Fatalf("source=%q", layout.Source)
	}
	assertPath(t, layout.V2Root, filepath.Join(custom, ".zcode", "v2"))
	assertPath(t, layout.CLIRoot, filepath.Join(home, ".zcode", "cli"))

	if len(layout.ProviderConfigPaths) != 3 {
		t.Fatalf("provider candidates=%v", layout.ProviderConfigPaths)
	}
	assertPath(t, layout.ProviderConfigPaths[0], filepath.Join(custom, ".zcode", "v2", "config.json"))
	assertPath(t, layout.ProviderConfigPaths[1], filepath.Join(home, ".zcode", "v2", "config.json"))
	assertPath(t, layout.ProviderConfigPaths[2], filepath.Join(home, ".zcode", "cli", "config.json"))
}

func TestResolveDataBaseDirEnvironmentFallback(t *testing.T) {
	home := t.TempDir()
	custom := t.TempDir()
	layout := resolve(home, envMap(map[string]string{
		"ZCODE_DATA_BASE_DIR": custom,
	}), missingReader)

	if layout.Source != "data_base_env" {
		t.Fatalf("source=%q", layout.Source)
	}
	assertPath(t, layout.V2Root, filepath.Join(custom, ".zcode", "v2"))
	assertPath(t, layout.CLIRoot, filepath.Join(home, ".zcode", "cli"))
}

func TestResolveZCodeHomeReplacesWholeRoot(t *testing.T) {
	home := t.TempDir()
	whole := filepath.Join(t.TempDir(), "zcode-home")
	setting := filepath.Join(home, ".zcode", "v2", "setting.json")
	reader := exactReader(map[string][]byte{
		filepath.Clean(setting): []byte("{\"dataBaseDir\":\"ignored\"}"),
	})
	layout := resolve(home, envMap(map[string]string{
		"ZCODE_HOME":          whole,
		"ZCODE_DATA_BASE_DIR": filepath.Join(t.TempDir(), "ignored-env"),
	}), reader)

	if layout.Source != "zcode_home" {
		t.Fatalf("source=%q", layout.Source)
	}
	assertPath(t, layout.V2Root, filepath.Join(whole, "v2"))
	assertPath(t, layout.CLIRoot, filepath.Join(whole, "cli"))
	assertPath(t, layout.ControlSettingPath, filepath.Join(whole, "v2", "setting.json"))
}

func TestResolveHUDHomeAndLeafOverridesWin(t *testing.T) {
	home := t.TempDir()
	whole := filepath.Join(t.TempDir(), "hud-home")
	runtimeDB := filepath.Join(t.TempDir(), "runtime.sqlite")
	taskDB := filepath.Join(t.TempDir(), "tasks.sqlite")
	logDir := filepath.Join(t.TempDir(), "logs")
	config := filepath.Join(t.TempDir(), "provider.json")

	layout := resolve(home, envMap(map[string]string{
		"HUD_ZCODE_HOME":       whole,
		"ZCODE_HOME":           filepath.Join(t.TempDir(), "ignored"),
		"HUD_ZCODE_RUNTIME_DB": runtimeDB,
		"HUD_ZCODE_DB":         taskDB,
		"HUD_ZCODE_LOG_DIR":    logDir,
		"HUD_ZCODE_CONFIG":     config,
	}), missingReader)

	if layout.Source != "hud_zcode_home" {
		t.Fatalf("source=%q", layout.Source)
	}
	assertPath(t, layout.RuntimeDB, runtimeDB)
	assertPath(t, layout.TaskIndexDB, taskDB)
	assertPath(t, layout.LogDir, logDir)
	if len(layout.ProviderConfigPaths) != 1 {
		t.Fatalf("provider candidates=%v", layout.ProviderConfigPaths)
	}
	assertPath(t, layout.ProviderConfigPaths[0], config)
}

func envMap(values map[string]string) func(string) string {
	return func(name string) string {
		if values == nil {
			return ""
		}
		return values[name]
	}
}

func missingReader(string) ([]byte, error) {
	return nil, os.ErrNotExist
}

func exactReader(files map[string][]byte) func(string) ([]byte, error) {
	return func(path string) ([]byte, error) {
		if data, ok := files[filepath.Clean(path)]; ok {
			return data, nil
		}
		return nil, errors.New("unexpected path")
	}
}

func assertPath(t *testing.T, actual, expected string) {
	t.Helper()
	if filepath.Clean(actual) != filepath.Clean(expected) {
		t.Fatalf("path=%q expected=%q", actual, expected)
	}
}


func TestDecodeJSONCPreservesCommentMarkersInsideStrings(t *testing.T) {
	var got struct {
		URL  string `json:"url"`
		Name string `json:"name"`
	}
	data := []byte("\xef\xbb\xbf{\n // comment\n \"url\": \"https://example.test/a//b\", /* block */\n \"name\": \"x/*literal*/y\"\n}")
	if err := DecodeJSONC(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.URL != "https://example.test/a//b" || got.Name != "x/*literal*/y" {
		t.Fatalf("decoded JSONC = %#v", got)
	}
}
