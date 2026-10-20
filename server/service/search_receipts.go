package service

import (
	"context"
	"encoding/json"
	"time"
)

type SearchReceipt struct {
	OperationID string          `json:"operation_id"`
	Operation   string          `json:"operation"`
	CompletedAt time.Time       `json:"completed_at"`
	Response    json.RawMessage `json:"response"`
}

func (s *TaskService) ListSearchReceipts(ctx context.Context, userID, projectID, taskID, executionID string) ([]SearchReceipt, error) {
	if err := s.ValidateAgentExecutionAccess(ctx, userID, projectID, taskID, executionID); err != nil {
		return nil, err
	}
	records, err := s.repo.SearchOperations().ListSucceeded(ctx, userID, projectID, taskID, executionID)
	if err != nil {
		return nil, err
	}
	result := make([]SearchReceipt, 0, len(records))
	for _, record := range records {
		result = append(result, SearchReceipt{OperationID: record.ID, Operation: record.Operation, CompletedAt: record.UpdatedAt, Response: json.RawMessage(record.ResponseSnapshot)})
	}
	return result, nil
}
