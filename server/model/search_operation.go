package model

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/datatypes"
)

type SearchOperationStatus string

const (
	SearchOperationRunning     SearchOperationStatus = "running"
	SearchOperationCostPending SearchOperationStatus = "cost_pending"
	SearchOperationSucceeded   SearchOperationStatus = "succeeded"
	SearchOperationFailed      SearchOperationStatus = "failed"

	SearchOperationErrorProviderCostPending      = "provider_cost_pending"
	SearchOperationErrorProviderErrorCostPending = "provider_error_cost_pending"
)

// SearchOperation is the durable idempotency record for one execution-scoped
// search capability call. The operation namespace distinguishes web search,
// page fetch, and image search calls. The response snapshot lets a
// lost MCP response be replayed without invoking the upstream provider again.
type SearchOperation struct {
	ID                 string                `gorm:"type:char(36);primaryKey" json:"id"`
	Operation          string                `gorm:"type:varchar(80);index;not null;default:'mcp.search_web'" json:"operation"`
	UserID             string                `gorm:"type:char(36);index;not null" json:"user_id"`
	ProjectID          string                `gorm:"type:char(36);index;not null" json:"project_id"`
	TaskID             string                `gorm:"type:char(36);index;not null" json:"task_id"`
	ExecutionID        string                `gorm:"type:varchar(128);index;not null" json:"execution_id"`
	RequestFingerprint string                `gorm:"type:char(64);uniqueIndex:idx_search_operation_fingerprint;not null" json:"request_fingerprint"`
	AttemptID          string                `gorm:"type:varchar(128);index;not null" json:"attempt_id"`
	ProviderRequestID  string                `gorm:"type:varchar(256);index" json:"provider_request_id,omitempty"`
	Status             SearchOperationStatus `gorm:"type:varchar(20);index;not null" json:"status"`
	ResponseSnapshot   datatypes.JSON        `gorm:"type:json" json:"response_snapshot,omitempty"`
	ErrorCode          string                `gorm:"type:varchar(80)" json:"error_code,omitempty"`
	LeaseExpiresAt     *time.Time            `gorm:"index" json:"lease_expires_at,omitempty"`
	CreatedAt          time.Time             `json:"created_at"`
	UpdatedAt          time.Time             `json:"updated_at"`
}

func (SearchOperation) TableName() string { return "search_operations" }

func (o SearchOperation) Validate() error {
	if strings.TrimSpace(o.Operation) == "" {
		o.Operation = "mcp.search_web"
	}
	switch strings.TrimSpace(o.Operation) {
	case "mcp.search_web", "mcp.fetch_web_page", "mcp.search_images":
	default:
		return fmt.Errorf("unsupported search operation %q", o.Operation)
	}
	for field, value := range map[string]string{
		"id": o.ID, "user_id": o.UserID, "project_id": o.ProjectID,
		"task_id": o.TaskID, "execution_id": o.ExecutionID,
		"request_fingerprint": o.RequestFingerprint, "attempt_id": o.AttemptID,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("search operation %s is required", field)
		}
	}
	if len(o.RequestFingerprint) != 64 {
		return fmt.Errorf("search operation request fingerprint must be a SHA-256 digest")
	}
	switch o.Status {
	case SearchOperationRunning, SearchOperationCostPending, SearchOperationSucceeded, SearchOperationFailed:
	default:
		return fmt.Errorf("unsupported search operation status %q", o.Status)
	}
	if (o.Status == SearchOperationCostPending || o.Status == SearchOperationSucceeded) && len(o.ResponseSnapshot) == 0 {
		return fmt.Errorf("completed provider search operation requires a response snapshot")
	}
	return nil
}
