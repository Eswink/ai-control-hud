package zcodepath

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Layout is the effective read-only ZCode storage layout for the current
// interactive user. ZCode Desktop may move its v2 data root independently of
// the CLI runtime root, so callers must not reconstruct sibling paths.
type Layout struct {
	ProfileHome         string
	ControlSettingPath  string
	V2Root              string
	CLIRoot             string
	RuntimeDB           string
	LogDir              string
	TaskIndexDB         string
	ProviderConfigPaths []string
	Source              string
}

// Resolve derives the current user's effective ZCode read-only layout.
//
// Precedence:
//   1. HUD_ZCODE_HOME (whole .zcode replacement)
//   2. ZCODE_HOME (whole .zcode replacement)
//   3. ~/.zcode/v2/setting.json dataBaseDir (v2 root only)
//   4. ZCODE_DATA_BASE_DIR (v2 root only)
//   5. ~/.zcode default
//
// Existing leaf overrides remain highest precedence for their individual
// sources: HUD_ZCODE_RUNTIME_DB, HUD_ZCODE_DB, HUD_ZCODE_LOG_DIR and
// HUD_ZCODE_CONFIG.
func Resolve() (Layout, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Layout{}, err
	}
	return resolve(home, os.Getenv, os.ReadFile), nil
}

func resolve(home string, getenv func(string) string, readFile func(string) ([]byte, error)) Layout {
	home = cleanAbsolute(home)
	profileRoot := filepath.Join(home, ".zcode")
	controlSetting := filepath.Join(profileRoot, "v2", "setting.json")

	layout := Layout{
		ProfileHome:        home,
		ControlSettingPath: controlSetting,
		V2Root:             filepath.Join(profileRoot, "v2"),
		CLIRoot:            filepath.Join(profileRoot, "cli"),
		Source:             "default",
	}

	if root := firstEnv(getenv, "HUD_ZCODE_HOME", "ZCODE_HOME"); root != "" {
		root = cleanAbsolute(expandHome(root, home))
		layout.ControlSettingPath = filepath.Join(root, "v2", "setting.json")
		layout.V2Root = filepath.Join(root, "v2")
		layout.CLIRoot = filepath.Join(root, "cli")
		if strings.TrimSpace(getenv("HUD_ZCODE_HOME")) != "" {
			layout.Source = "hud_zcode_home"
		} else {
			layout.Source = "zcode_home"
		}
	} else if dataBaseDir := dataBaseDirFromSetting(controlSetting, readFile); dataBaseDir != "" {
		base := cleanAbsolute(expandHome(dataBaseDir, home))
		layout.V2Root = filepath.Join(base, ".zcode", "v2")
		layout.Source = "data_base_setting"
	} else if dataBaseDir := strings.TrimSpace(getenv("ZCODE_DATA_BASE_DIR")); dataBaseDir != "" {
		base := cleanAbsolute(expandHome(dataBaseDir, home))
		layout.V2Root = filepath.Join(base, ".zcode", "v2")
		layout.Source = "data_base_env"
	}

	layout.RuntimeDB = filepath.Join(layout.CLIRoot, "db", "db.sqlite")
	layout.LogDir = filepath.Join(layout.CLIRoot, "log")
	layout.TaskIndexDB = filepath.Join(layout.V2Root, "tasks-index.sqlite")

	if value := strings.TrimSpace(getenv("HUD_ZCODE_RUNTIME_DB")); value != "" {
		layout.RuntimeDB = cleanAbsolute(expandHome(value, home))
	}
	if value := strings.TrimSpace(getenv("HUD_ZCODE_LOG_DIR")); value != "" {
		layout.LogDir = cleanAbsolute(expandHome(value, home))
	}
	if value := strings.TrimSpace(getenv("HUD_ZCODE_DB")); value != "" {
		layout.TaskIndexDB = cleanAbsolute(expandHome(value, home))
	}

	if value := strings.TrimSpace(getenv("HUD_ZCODE_CONFIG")); value != "" {
		layout.ProviderConfigPaths = []string{cleanAbsolute(expandHome(value, home))}
	} else {
		candidates := []string{
			filepath.Join(layout.V2Root, "config.json"),
			filepath.Join(profileRoot, "v2", "config.json"),
			filepath.Join(layout.CLIRoot, "config.json"),
		}
		layout.ProviderConfigPaths = uniqueClean(candidates)
	}
	return layout
}

func firstEnv(getenv func(string) string, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

func dataBaseDirFromSetting(path string, readFile func(string) ([]byte, error)) string {
	data, err := readFile(path)
	if err != nil {
		return ""
	}
	data = []byte(strings.TrimPrefix(string(data), "\ufeff"))
	data = stripJSONComments(data)

	var root map[string]any
	if json.Unmarshal(data, &root) != nil {
		return ""
	}
	value, _ := root["dataBaseDir"].(string)
	return strings.TrimSpace(value)
}

func stripJSONComments(data []byte) []byte {
	input := string(data)
	var out strings.Builder
	out.Grow(len(input))

	inString := false
	lineComment := false
	blockComment := false
	escaped := false
	for i := 0; i < len(input); i++ {
		ch := input[i]
		if lineComment {
			if ch == '\n' || ch == '\r' {
				lineComment = false
				out.WriteByte(ch)
			}
			continue
		}
		if blockComment {
			if ch == '*' && i+1 < len(input) && input[i+1] == '/' {
				blockComment = false
				i++
			} else if ch == '\n' || ch == '\r' {
				out.WriteByte(ch)
			}
			continue
		}
		if inString {
			out.WriteByte(ch)
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				inString = false
			}
			continue
		}
		switch {
		case ch == '"':
			inString = true
			out.WriteByte(ch)
		case ch == '/' && i+1 < len(input) && input[i+1] == '/':
			lineComment = true
			i++
		case ch == '/' && i+1 < len(input) && input[i+1] == '*':
			blockComment = true
			i++
		default:
			out.WriteByte(ch)
		}
	}
	return []byte(out.String())
}

func expandHome(path, home string) string {
	path = strings.TrimSpace(path)
	if path == "" || path[0] != '~' {
		return path
	}
	if path == "~" {
		return home
	}
	if len(path) > 1 && (path[1] == '/' || path[1] == '\\') {
		return filepath.Join(home, path[2:])
	}
	return path
}

func cleanAbsolute(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(absolute)
}

func uniqueClean(paths []string) []string {
	result := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	for _, path := range paths {
		path = filepath.Clean(path)
		key := strings.ToLower(path)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, path)
	}
	return result
}
