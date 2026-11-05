package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestKubernetesConfigRejectsRemovedServerCA(t *testing.T) {
	var cfg KubernetesConfig
	err := yaml.Unmarshal([]byte("server_ca_secret: obsolete-ca\n"), &cfg)
	if err == nil || !strings.Contains(err.Error(), `unknown claude.kubernetes config field "server_ca_secret"`) {
		t.Fatalf("configuration error = %v, want removed server CA field rejection", err)
	}
}

func TestDshMCPDefaultsToInternalServer(t *testing.T) {
	for _, tc := range []struct {
		name       string
		agentURL   string
		serverURL  string
		mcpURL     string
		wantServer string
		wantMCP    string
	}{
		{
			name:       "Kubernetes Service",
			agentURL:   "http://creator-api-svc.anbanai-prod.svc.cluster.local:8080/",
			wantServer: "http://creator-api-svc.anbanai-prod.svc.cluster.local:8080",
			wantMCP:    "http://creator-api-svc.anbanai-prod.svc.cluster.local:8080/mcp",
		},
		{
			name:       "default port",
			wantServer: "http://localhost:8080",
			wantMCP:    "http://localhost:8080/mcp",
		},
		{
			name:       "explicit DSH server",
			agentURL:   "http://creator-api-svc:8080",
			serverURL:  "http://server:8080/",
			wantServer: "http://server:8080/",
			wantMCP:    "http://server:8080/mcp",
		},
		{
			name:       "explicit MCP endpoint",
			agentURL:   "http://creator-api-svc:8080",
			mcpURL:     "https://creator.example.com/mcp",
			wantServer: "http://creator-api-svc:8080",
			wantMCP:    "https://creator.example.com/mcp",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{
				Claude: ClaudeConfig{AgentServerURL: tc.agentURL},
				Dsh:    DshConfig{ServerURL: tc.serverURL, MCPURL: tc.mcpURL},
			}
			cfg.applyDefaults()
			if cfg.Dsh.ServerURL != tc.wantServer || cfg.Dsh.MCPURL != tc.wantMCP {
				t.Fatalf("DSH server/MCP = %q / %q, want %q / %q", cfg.Dsh.ServerURL, cfg.Dsh.MCPURL, tc.wantServer, tc.wantMCP)
			}
		})
	}
}
