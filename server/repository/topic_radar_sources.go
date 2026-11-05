package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/anbanai/anban-creator/server/model"
)

// ListRecentByProject returns only durable, successful receipts owned by the
// project. Runtime execution-scoped receipt APIs retain their stricter scope.
func (r *searchOperationRepository) ListRecentByProject(ctx context.Context, userID, projectID string, limit int) ([]*model.SearchOperation, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(projectID) == "" {
		return nil, errors.New("project receipt scope is required")
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	var rows []*model.SearchOperation
	err := r.db.WithContext(ctx).Where("user_id = ? AND project_id = ? AND status = ? AND operation IN ?", userID, projectID, model.SearchOperationSucceeded, []string{"mcp.search_web", "mcp.fetch_web_page"}).Order("created_at DESC, id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}
