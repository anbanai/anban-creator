package service

import (
	"fmt"
	"strings"
)

// PrepareResult is the response for workspace preparation.
type PrepareResult struct {
	Path string `json:"path"`
}

// WorkspaceService provides managed task workspace paths.
type WorkspaceService struct{}

// NewWorkspaceService creates a new WorkspaceService.
func NewWorkspaceService() *WorkspaceService {
	return &WorkspaceService{}
}

// Prepare returns the output directory relative to the managed task workspace.
func (s *WorkspaceService) Prepare(contentType, taskID string) (*PrepareResult, error) {
	if strings.TrimSpace(contentType) == "" {
		return nil, fmt.Errorf("content_type is required")
	}
	if strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("task_id is required")
	}
	return &PrepareResult{Path: "output"}, nil
}
