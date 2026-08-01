package config

import "testing"

func TestApplyDefaultsLeavesAgentPackTurnBudgetsAuthoritative(t *testing.T) {
	cfg := Config{}
	cfg.applyDefaults()
	if len(cfg.Claude.MaxTurns) != 0 {
		t.Fatalf("default max-turn overrides = %#v, want Agent Pack defaults", cfg.Claude.MaxTurns)
	}

	cfg = Config{}
	cfg.Claude.MaxTurns = map[string]int{"article": 42}
	cfg.applyDefaults()
	if len(cfg.Claude.MaxTurns) != 1 || cfg.Claude.MaxTurns["article"] != 42 {
		t.Fatalf("explicit max-turn overrides = %#v", cfg.Claude.MaxTurns)
	}
}
