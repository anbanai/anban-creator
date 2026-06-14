package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestImageAPIConfig_DesignerOrder_PreservesYAMLInsertionOrder(t *testing.T) {
	yamlText := `
designer:
  openai:
    alias: "GPT"
  wangcai:
    alias: "Wangcai"
  gemini:
    alias: "Gemini"
  seedream:
    alias: "Seedream"
`
	var cfg ImageAPIConfig
	if err := yaml.Unmarshal([]byte(yamlText), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	got := cfg.DesignerOrder()
	want := []string{"openai", "wangcai", "gemini", "seedream"}
	if len(got) != len(want) {
		t.Fatalf("DesignerOrder len = %d, want %d (got=%v)", len(got), len(want), got)
	}
	for i, k := range want {
		if got[i] != k {
			t.Fatalf("DesignerOrder[%d] = %q, want %q (full=%v)", i, got[i], k, got)
		}
	}
}

func TestImageAPIConfig_DesignerOrder_EmptyWhenDesignerAbsent(t *testing.T) {
	yamlText := `
cover:
  provider: openai
`
	var cfg ImageAPIConfig
	if err := yaml.Unmarshal([]byte(yamlText), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if order := cfg.DesignerOrder(); len(order) != 0 {
		t.Fatalf("DesignerOrder = %v, want empty", order)
	}
}

func TestImageAPIConfig_DesignerOrder_NilReceiver(t *testing.T) {
	var cfg *ImageAPIConfig
	if order := cfg.DesignerOrder(); order != nil {
		t.Fatalf("nil receiver DesignerOrder = %v, want nil", order)
	}
}

// Verify the unmarshal helper tolerates whitespace/formatting variation in YAML.
func TestImageAPIConfig_DesignerOrder_StableAcrossReparse(t *testing.T) {
	yamlText := strings.Join([]string{
		"designer:",
		"  zeta:",
		"    alias: Z",
		"  alpha:",
		"    alias: A",
		"  mu:",
		"    alias: M",
	}, "\n")

	var first, second ImageAPIConfig
	if err := yaml.Unmarshal([]byte(yamlText), &first); err != nil {
		t.Fatalf("first unmarshal: %v", err)
	}
	if err := yaml.Unmarshal([]byte(yamlText), &second); err != nil {
		t.Fatalf("second unmarshal: %v", err)
	}
	if len(first.DesignerOrder()) != len(second.DesignerOrder()) {
		t.Fatalf("order length drift between parses")
	}
	for i := range first.DesignerOrder() {
		if first.DesignerOrder()[i] != second.DesignerOrder()[i] {
			t.Fatalf("order not stable across parses: %v vs %v", first.DesignerOrder(), second.DesignerOrder())
		}
	}
}
