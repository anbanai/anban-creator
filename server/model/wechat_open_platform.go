package model

import "time"

const (
	WechatAuthorizationStatusConnected    = "connected"
	WechatAuthorizationStatusRevoked      = "revoked"
	WechatAuthorizationStatusDisconnected = "disconnected"
)

// WechatOpenPlatformAuthorization stores the durable link between one project
// and its authorized official account. Access tokens remain in shared cache.
type WechatOpenPlatformAuthorization struct {
	ID                     string     `gorm:"type:char(36);primaryKey" json:"id"`
	ProjectID              string     `gorm:"type:char(36);not null;uniqueIndex" json:"project_id"`
	UserID                 string     `gorm:"type:char(36);not null;index" json:"user_id"`
	AuthorizerAppID        string     `gorm:"type:varchar(100);not null;index" json:"authorizer_app_id"`
	AccountName            string     `gorm:"type:varchar(200);not null;default:''" json:"account_name"`
	AvatarURL              string     `gorm:"type:varchar(1000);not null;default:''" json:"avatar_url"`
	AccountType            int        `gorm:"not null;default:0" json:"account_type"`
	GrantedScopes          []int      `gorm:"serializer:json;type:json" json:"granted_scopes"`
	Status                 string     `gorm:"type:varchar(20);not null;index" json:"status"`
	RefreshTokenCiphertext string     `gorm:"type:text;not null" json:"-"`
	TokenVersion           uint64     `gorm:"not null;default:1" json:"-"`
	AuthorizedAt           time.Time  `gorm:"not null" json:"authorized_at"`
	RevokedAt              *time.Time `json:"revoked_at,omitempty"`
	LastEventAt            *time.Time `json:"last_event_at,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

func (WechatOpenPlatformAuthorization) TableName() string {
	return "wechat_open_platform_authorizations"
}

// WechatOpenPlatformAuthorizationState binds an opaque, single-use OAuth state
// to its project and user. Only the state hash is persisted.
type WechatOpenPlatformAuthorizationState struct {
	StateHash  string     `gorm:"type:char(64);primaryKey" json:"-"`
	ProjectID  string     `gorm:"type:char(36);not null;index" json:"project_id"`
	UserID     string     `gorm:"type:char(36);not null;index" json:"user_id"`
	ExpiresAt  time.Time  `gorm:"not null;index" json:"expires_at"`
	ConsumedAt *time.Time `json:"consumed_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

func (WechatOpenPlatformAuthorizationState) TableName() string {
	return "wechat_open_platform_authorization_states"
}
