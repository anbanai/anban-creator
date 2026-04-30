package mcp

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// CheckHealth verifies the MCP endpoint is accessible with the given API key.
// It sends a test JSON-RPC initialize request and logs the result.
// This runs asynchronously at startup and does not block server initialization.
func CheckHealth(ctx context.Context, baseURL, apiKey string, log *zerolog.Logger) {
	if baseURL == "" || apiKey == "" {
		log.Warn().
			Bool("base_url_set", baseURL != "").
			Bool("api_key_set", apiKey != "").
			Msg("MCP health check skipped: missing base URL or API key")
		return
	}

	// Wait briefly for the HTTP server to start accepting connections.
	time.Sleep(2 * time.Second)

	mcpURL := strings.TrimRight(baseURL, "/") + "/mcp"
	reqBody := `{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"health-check","version":"1.0"}},"id":1}`

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, strings.NewReader(reqBody))
	if err != nil {
		log.Error().Err(err).Msg("MCP health check: failed to create request")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Error().Err(err).Str("url", mcpURL).Msg("MCP health check: request failed (server may not be listening yet)")
		return
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		log.Info().Str("url", mcpURL).Msg("MCP health check passed: endpoint accessible with system API key")
	case http.StatusUnauthorized:
		log.Error().
			Int("status", resp.StatusCode).
			Str("url", mcpURL).
			Msg("MCP health check FAILED: 401 Unauthorized — system API key cannot authenticate. All agent tasks will fail MCP auth")
	default:
		log.Warn().
			Int("status", resp.StatusCode).
			Str("url", mcpURL).
			Msg("MCP health check: unexpected status code")
	}
}
