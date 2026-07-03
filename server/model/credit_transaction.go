package model

import "time"

// CreditTransaction represents a single credit balance change (income or expense).
type CreditTransaction struct {
	ID           uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID       string    `gorm:"type:char(36);index:idx_ct_user_created,priority:1;not null" json:"user_id"`
	Type         string    `gorm:"type:varchar(20);not null" json:"type"`
	Amount       int       `gorm:"not null" json:"amount"`        // positive=income, negative=expense
	BalanceAfter int       `gorm:"not null" json:"balance_after"` // balance after this transaction
	TaskID       *string   `gorm:"type:char(36);index" json:"task_id,omitempty"`
	OperationID  *string   `gorm:"type:varchar(100);index" json:"operation_id,omitempty"`
	Description  string    `gorm:"type:varchar(500)" json:"description"`
	CreatedAt    time.Time `gorm:"index:idx_ct_user_created,priority:2" json:"created_at"`
}

// TableName returns the database table name for CreditTransaction.
func (CreditTransaction) TableName() string { return "credit_transactions" }
