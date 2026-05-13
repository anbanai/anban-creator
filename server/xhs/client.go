package xhs

import (
	"context"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client communicates with the xiaohongshu-mcp Docker sidecar REST API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new XHS sidecar client.
func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// HealthCheck verifies the sidecar is reachable.
func (c *Client) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("create health check request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("health check request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned status %d", resp.StatusCode)
	}
	return nil
}

// CheckLoginStatus returns whether the XHS sidecar has an active login session.
func (c *Client) CheckLoginStatus(ctx context.Context) (bool, error) {
	var result LoginStatusResponse
	if err := c.get(ctx, "/api/v1/login/status", &result); err != nil {
		return false, fmt.Errorf("check login status: %w", err)
	}
	if !result.Success {
		return false, fmt.Errorf("check login status failed: %s", result.Message)
	}
	return result.LoggedIn, nil
}

// GetLoginQRCode returns a base64-encoded PNG QR code image for XHS login.
func (c *Client) GetLoginQRCode(ctx context.Context) (string, error) {
	var result QRCodeResponse
	if err := c.get(ctx, "/api/v1/login/qrcode", &result); err != nil {
		return "", fmt.Errorf("get login qrcode: %w", err)
	}
	if !result.Success {
		return "", fmt.Errorf("get login qrcode failed: %s", result.Message)
	}
	return result.Data.QRCodeImage, nil
}

// DeleteCookies clears the XHS login session.
func (c *Client) DeleteCookies(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/api/v1/login/cookies", nil)
	if err != nil {
		return fmt.Errorf("create delete cookies request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete cookies request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("delete cookies returned status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// get performs a GET request and decodes the JSON response.
func (c *Client) get(ctx context.Context, path string, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("create GET request: %w", err)
	}
	return c.doRequest(req, result)
}

// post performs a POST request with a JSON body and decodes the response.
func (c *Client) post(ctx context.Context, path string, body any, result any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create POST request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return c.doRequest(req, result)
}

// doRequest sends the request and decodes the response into result.
func (c *Client) doRequest(req *http.Request, result any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("request returned status %d: %s", resp.StatusCode, string(body))
	}

	if err := json.Unmarshal(body, result); err != nil {
		return fmt.Errorf("decode response: %w (body: %s)", err, string(body))
	}

	return nil
}
