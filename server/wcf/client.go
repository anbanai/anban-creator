// Package wcf is the HTTP client for the wcfLink WeChat-bot sidecar.
// wcfLink (github.com/lich0821/wcfLink) is a local iLink WeChat channel service
// that the server drives to push task notifications and receive WeChat commands.
// It is NOT the Windows-only WeChatFerry PC-hook — it runs headless in Docker.
package wcf

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Client communicates with the wcfLink sidecar HTTP API.
// Structure mirrors server/seednote/client.go.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new wcfLink sidecar client.
func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// BaseURL returns the configured sidecar base URL (used by callers for logging).
func (c *Client) BaseURL() string { return c.baseURL }

// HealthCheck verifies the sidecar is reachable (GET /health/live).
func (c *Client) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health/live", nil)
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

// StartLogin starts a wcfLink QR login flow. baseURL is the iLink gateway URL
// (pass "" to use wcfLink's configured default). Returns the login session,
// whose QRCodeURL is rendered into a PNG via GetLoginQR.
func (c *Client) StartLogin(ctx context.Context, baseURL string) (LoginSession, error) {
	var session LoginSession
	body := map[string]string{"base_url": baseURL}
	if err := c.post(ctx, "/api/accounts/login/start", body, &session); err != nil {
		return LoginSession{}, fmt.Errorf("start login: %w", err)
	}
	return session, nil
}

// GetLoginStatus polls a login session. On iLink "confirmed" wcfLink finalizes
// the account and starts its poller; the returned session carries the AccountID.
func (c *Client) GetLoginStatus(ctx context.Context, sessionID string) (LoginSession, error) {
	var session LoginSession
	if err := c.get(ctx, "/api/accounts/login/status?session_id="+url.QueryEscape(sessionID), &session); err != nil {
		return LoginSession{}, fmt.Errorf("get login status: %w", err)
	}
	return session, nil
}

// GetLoginQR returns the QR code PNG bytes for a login session. The bytes are a
// raw image/png body — callers base64-encode them into a data URI for display.
func (c *Client) GetLoginQR(ctx context.Context, sessionID string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/api/accounts/login/qr?session_id="+url.QueryEscape(sessionID), nil)
	if err != nil {
		return nil, fmt.Errorf("create qr request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("qr request failed: %w", err)
	}
	defer resp.Body.Close()
	png, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read qr body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("qr returned status %d: %s", resp.StatusCode, string(png))
	}
	return png, nil
}

// ListAccounts returns all logged-in wcfLink accounts.
func (c *Client) ListAccounts(ctx context.Context) ([]Account, error) {
	var wrapper struct {
		Items []Account `json:"items"`
	}
	if err := c.get(ctx, "/api/accounts", &wrapper); err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	return wrapper.Items, nil
}

// ListEvents returns events strictly after afterID (monotonic int64). Pass the
// highest ID previously seen to page incrementally; the server dedupes by ID.
func (c *Client) ListEvents(ctx context.Context, afterID int64, limit int) ([]Event, error) {
	q := url.Values{}
	q.Set("after_id", strconv.FormatInt(afterID, 10))
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var wrapper struct {
		Items []Event `json:"items"`
	}
	if err := c.get(ctx, "/api/events?"+q.Encode(), &wrapper); err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	return wrapper.Items, nil
}

// SendText sends a text message from accountID to toUserID. The context_token
// is omitted; wcfLink auto-resolves it from its peer_contexts table, which is
// seeded by a prior inbound message from that peer. Sending fails with
// "context token not found" until the peer has sent at least one message.
func (c *Client) SendText(ctx context.Context, accountID, toUserID, text string) error {
	body := map[string]string{
		"account_id": accountID,
		"to_user_id": toUserID,
		"text":       text,
	}
	if err := c.post(ctx, "/api/messages/send-text", body, nil); err != nil {
		return fmt.Errorf("send text: %w", err)
	}
	return nil
}

// get performs a GET request and decodes the JSON response (no-op when result is nil).
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

// doRequest sends the request and decodes the response into result (nil = ignore body).
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

	if result == nil || len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, result); err != nil {
		return fmt.Errorf("decode response: %w (body: %s)", err, string(body))
	}
	return nil
}
