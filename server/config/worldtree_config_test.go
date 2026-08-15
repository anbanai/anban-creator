package config

import "testing"

func TestWorldtreeConfigDefaults(t *testing.T) {
	cfg := &Config{}
	cfg.applyDefaults()
	if cfg.Worldtree.BaseURL != "https://www.worldtreetech.cn" {
		t.Fatalf("base URL = %q", cfg.Worldtree.BaseURL)
	}
	if cfg.Worldtree.Timeout != 30 {
		t.Fatalf("timeout = %d", cfg.Worldtree.Timeout)
	}
	if cfg.Worldtree.Key != "" {
		t.Fatal("WorldTree key must not receive a default value")
	}
}
