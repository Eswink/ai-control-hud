package mock

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Eswink/ai-control-hud/agent/internal/domain"
)

func LoadFixture(path string) (domain.HudState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.HudState{}, fmt.Errorf("read fixture: %w", err)
	}

	var state domain.HudState
	if err := json.Unmarshal(data, &state); err != nil {
		return domain.HudState{}, fmt.Errorf("decode fixture: %w", err)
	}
	if err := state.Validate(); err != nil {
		return domain.HudState{}, fmt.Errorf("validate fixture: %w", err)
	}
	return state, nil
}
