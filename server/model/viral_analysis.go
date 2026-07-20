package model

import (
	"encoding/json"
	"time"
)

// ViralAnalysis represents a viral content analysis report.
type ViralAnalysis struct {
	ID                    string          `gorm:"type:char(36);primaryKey" json:"id"`
	UserID                string          `gorm:"type:char(36);not null;index" json:"user_id"`
	SourceType            string          `gorm:"type:varchar(20);not null" json:"source_type"`
	SourceURL             string          `gorm:"type:varchar(500);not null" json:"source_url"`
	SourceData            json.RawMessage `gorm:"type:json" json:"source_data"`
	AnalysisResult        json.RawMessage `gorm:"type:json" json:"analysis_result"`
	Status                string          `gorm:"type:varchar(20);not null;default:'pending'" json:"status"`
	ErrorMessage          string          `gorm:"type:text" json:"error_message,omitempty"`
	BillingQuoteID        string          `gorm:"type:char(36);index;not null" json:"billing_quote_id"`
	BillingCatalogID      string          `gorm:"type:varchar(128);index;not null" json:"billing_catalog_id"`
	BillingSKUID          string          `gorm:"type:varchar(128);index;not null" json:"billing_sku_id"`
	BillingChargeID       *string         `gorm:"type:char(36);uniqueIndex;not null" json:"billing_charge_id"`
	BillingPriceCredits   int64           `gorm:"not null" json:"billing_price_credits"`
	BillingTerminalReason string          `gorm:"type:varchar(64);index" json:"billing_terminal_reason,omitempty"`
	CreatedAt             time.Time       `gorm:"index" json:"created_at"`
	UpdatedAt             time.Time       `json:"updated_at"`
}

func (ViralAnalysis) TableName() string { return "viral_analyses" }
