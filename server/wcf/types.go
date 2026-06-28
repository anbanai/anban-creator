package wcf

import "time"

// Types below mirror the JSON shapes exposed by the wcfLink sidecar HTTP API
// (github.com/lich0821/wcfLink internal/model + internal/httpapi). Verified
// against the wcfLink source — the README only documents the send-text request.

// LoginSession is the wcfLink login-flow record. Status transitions
// "wait" → ("scanned") → "confirmed"; AccountID is populated once confirmed.
type LoginSession struct {
	SessionID   string     `json:"session_id"`
	BaseURL     string     `json:"base_url"`
	QRCodeURL   string     `json:"qr_code_url"` // iLink QR URL string; render via /api/accounts/login/qr
	Status      string     `json:"status"`      // "wait" | "scanned" | "confirmed" | "expired" | error
	AccountID   string     `json:"account_id,omitempty"`
	ILinkUserID string     `json:"ilink_user_id,omitempty"`
	Error       string     `json:"error,omitempty"`
	StartedAt   time.Time  `json:"started_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// Confirmed reports whether the login completed and AccountID is available.
func (s LoginSession) Confirmed() bool { return s.Status == "confirmed" && s.AccountID != "" }

// Account is a logged-in wcfLink WeChat account (one row per user binding).
type Account struct {
	AccountID   string     `json:"account_id"`
	BaseURL     string     `json:"base_url"`
	ILinkUserID string     `json:"ilink_user_id,omitempty"`
	Enabled     bool       `json:"enabled"`
	LoginStatus string     `json:"login_status"`
	LastError   string     `json:"last_error,omitempty"`
	LastPollAt  *time.Time `json:"last_poll_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// Event is a stored inbound/outbound message. The monotonic int64 ID is the
// pagination + dedupe key (GET /api/events?after_id=<id>). For command input we
// filter Direction=="inbound" && EventType=="text".
type Event struct {
	ID           int64     `json:"id"`
	AccountID    string    `json:"account_id"`
	Direction    string    `json:"direction"`  // "inbound" | "outbound"
	EventType    string    `json:"event_type"` // "text" | "image" | "voice" | "file" | "video"
	FromUserID   string    `json:"from_user_id,omitempty"`
	ToUserID     string    `json:"to_user_id,omitempty"`
	MessageID    int64     `json:"message_id,omitempty"`
	ContextToken string    `json:"context_token,omitempty"`
	BodyText     string    `json:"body_text,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// IsInboundText reports whether this event is an inbound text message worth
// parsing as a command.
func (e Event) IsInboundText() bool {
	return e.Direction == "inbound" && e.EventType == "text"
}
