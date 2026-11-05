package service

import (
	"context"
)

// ReconcileTopicAdoptions drains pending and expired adoption outbox rows.
// Each row has its own lease and terminal status, so one failure does not block the batch.
func (s *TopicRadarService) ReconcileTopicAdoptions(ctx context.Context, limit int) error {
	if limit <= 0 {
		limit = 100
	}
	items, err := s.repo.TopicSuggestionAdoptions().ListRecoverable(ctx, s.now(), limit)
	if err != nil {
		return err
	}
	var firstErr error
	for _, item := range items {
		if err := s.AdoptSuggestion(ctx, item.ID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
