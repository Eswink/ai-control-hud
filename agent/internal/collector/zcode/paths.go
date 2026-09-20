package zcode

import "github.com/Eswink/ai-control-hud/agent/internal/zcodepath"

// ResolvedPaths returns the ZCode database paths for the current interactive
// user. The shared resolver honors ZCode whole-root and Desktop dataBaseDir
// semantics as well as the existing explicit HUD leaf overrides.
func ResolvedPaths() (runtimeDB, taskIndexDB string, err error) {
	layout, err := zcodepath.Resolve()
	if err != nil {
		return "", "", err
	}
	return layout.RuntimeDB, layout.TaskIndexDB, nil
}
