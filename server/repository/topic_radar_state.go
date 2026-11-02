package repository

import (
	"context"
	"errors"
	"time"

	"github.com/anbanai/anban-creator/server/model"
)

func topicOwnerID(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// UpdateIfNonterminal is the terminal compare-and-swap. The caller must include
// the exact task/execution observed before loading the execution's artifact.
func (r *topicDigestRepository) UpdateIfNonterminal(ctx context.Context, item *model.TopicDigest) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.TopicDigest{}).
		Where("id = ? AND status NOT IN ? AND COALESCE(agent_task_id, '') = ? AND COALESCE(agent_execution_id, '') = ?", item.ID, []string{model.TopicDigestStatusSucceeded, model.TopicDigestStatusFailed}, topicOwnerID(item.AgentTaskID), topicOwnerID(item.AgentExecutionID)).
		Updates(map[string]any{"status": item.Status, "dispatch_status": item.DispatchStatus, "candidate_count": item.CandidateCount, "active_key": item.ActiveKey, "finished_at": item.FinishedAt, "error": item.Error})
	return result.RowsAffected == 1, result.Error
}

func (r *topicAdoptionRepository) UpdateClaimed(ctx context.Context, item *model.TopicSuggestionAdoption, token string) error {
	result := r.db.WithContext(ctx).Model(&model.TopicSuggestionAdoption{}).
		Where("id = ? AND status = ? AND lease_token = ? AND lease_until > ?", item.ID, model.TopicAdoptionProcessing, token, time.Now()).
		Updates(map[string]any{"status": item.Status, "topic_pool_id": item.TopicPoolID, "error": item.Error, "lease_token": "", "lease_until": nil})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("topic adoption lease lost")
	}
	return nil
}
