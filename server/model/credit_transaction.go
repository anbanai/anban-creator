package model

import "time"

import "gorm.io/datatypes"

type CreditTransactionMetadata struct {
	Provider               string         `json:"provider,omitempty"`
	Model                  string         `json:"model,omitempty"`
	Route                  string         `json:"route,omitempty"`
	InputTokens            int64          `json:"input_tokens,omitempty"`
	CachedInputTokens      int64          `json:"cached_input_tokens,omitempty"`
	OutputTokens           int64          `json:"output_tokens,omitempty"`
	TextInputTokens        int64          `json:"text_input_tokens,omitempty"`
	TextCachedInputTokens  int64          `json:"text_cached_input_tokens,omitempty"`
	ImageInputTokens       int64          `json:"image_input_tokens,omitempty"`
	ImageCachedInputTokens int64          `json:"image_cached_input_tokens,omitempty"`
	ImageOutputTokens      int64          `json:"image_output_tokens,omitempty"`
	TotalTokens            int64          `json:"total_tokens,omitempty"`
	BaseCredits            int            `json:"base_credits,omitempty"`
	TierMultiplier         float64        `json:"tier_multiplier,omitempty"`
	UserMultiplier         float64        `json:"user_multiplier,omitempty"`
	FinalCredits           int            `json:"final_credits,omitempty"`
	PriceSnapshot          map[string]any `json:"price_snapshot,omitempty"`
}

// CreditTransaction represents a single credit balance change (income or expense).
type CreditTransaction struct {
	ID           uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID       string         `gorm:"type:char(36);index:idx_ct_user_created,priority:1;not null" json:"user_id"`
	Type         string         `gorm:"type:varchar(20);not null" json:"type"`
	Amount       int            `gorm:"not null" json:"amount"`        // positive=income, negative=expense
	BalanceAfter int            `gorm:"not null" json:"balance_after"` // balance after this transaction
	TaskID       *string        `gorm:"type:char(36);index" json:"task_id,omitempty"`
	OperationID  *string        `gorm:"type:varchar(100);index" json:"operation_id,omitempty"`
	Description  string         `gorm:"type:varchar(500)" json:"description"`
	Metadata     datatypes.JSON `gorm:"type:json" json:"metadata,omitempty"`
	CreatedAt    time.Time      `gorm:"index:idx_ct_user_created,priority:2" json:"created_at"`
}

// TableName returns the database table name for CreditTransaction.
func (CreditTransaction) TableName() string { return "credit_transactions" }
