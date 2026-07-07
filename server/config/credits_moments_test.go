package config

import "testing"

func TestCreditsDefaultsIncludeMomentsTaskCost(t *testing.T) {
	cfg := &Config{}
	cfg.applyDefaults()
	if got := cfg.Credits.TaskCosts["moments"]; got != 3000 {
		t.Fatalf("credits.task_costs.moments = %d, want 3000", got)
	}

	cfg = &Config{Credits: CreditsConfig{TaskCosts: map[string]int{"article": 4000}}}
	cfg.applyDefaults()
	if got := cfg.Credits.TaskCosts["moments"]; got != 3000 {
		t.Fatalf("configured task_costs should receive missing moments default, got %d", got)
	}
}
