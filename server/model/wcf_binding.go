package model

import "time"

// WCFBindingStatus is the lifecycle state of a user's WeChat binding.
const (
	WCFBindingStatusActive  = "active"  // account logged in + (optionally) peer captured
	WCFBindingStatusPending = "pending" // login started, not yet confirmed
	WCFBindingStatusUnbound = "unbound" // user unbound; row kept for history
)

// WCFBinding stores a user's WeChat-account binding via the wcfLink sidecar.
// One row per user (UserID unique). The user's own 文件传输助手 self-chat is the
// command/notification channel: WCFAccountID identifies the wcfLink account
// (the user's logged-in WeChat), PeerID is the conversation peer captured from
// the first inbound message (the peer the server replies/notifications to).
//
// ContextToken is NOT stored here — wcfLink auto-resolves it from its own
// peer_contexts table, seeded by a prior inbound message from PeerID. So
// outbound notifications require the user to have sent at least one command
// first (e.g. 帮助) to activate the channel.
type WCFBinding struct {
	ID     string `gorm:"type:char(36);primaryKey" json:"id"`
	UserID string `gorm:"type:char(36);uniqueIndex;not null" json:"user_id"`
	// LoginSessionID is the wcfLink login session this user's pending bind is
	// tied to. Stored server-side at StartBind and re-checked at PollBindStatus
	// so an authenticated user can't poll ANOTHER user's in-flight login by
	// guessing its session id (wcfLink mints session ids as "login_<unixnano>",
	// which is sequential, not random). Never serialized to clients.
	LoginSessionID   string     `gorm:"type:varchar(128)" json:"-"`
	WCFAccountID     string     `gorm:"type:varchar(128);index" json:"wcf_account_id"`
	PeerID           string     `gorm:"type:varchar(128)" json:"peer_id"`              // captured from first inbound message
	DefaultProjectID string     `gorm:"type:char(36);index" json:"default_project_id"` // resolved project for create commands
	Status           string     `gorm:"type:varchar(20);default:pending" json:"status"`
	BoundAt          *time.Time `gorm:"index" json:"bound_at"`
	LastSeenAt       time.Time  `json:"last_seen_at"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// TableName returns the database table name for WCFBinding.
func (WCFBinding) TableName() string { return "wcf_bindings" }
