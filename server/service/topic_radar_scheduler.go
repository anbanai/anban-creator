package service

import (
	"context"
	"errors"

	"github.com/anbanai/anban-creator/server/model"
)

// ScheduleTopicDigests claims due subscription occurrences through their unique
// occurrence keys, then advances each schedule with a compare-and-swap update.
func (s *TopicRadarService) ScheduleTopicDigests(ctx context.Context, limit int) error {
	if limit <= 0 {
		limit = 100
	}
	now := s.now()
	subs, err := s.repo.TopicSubscriptions().FindDue(ctx, now, limit)
	if err != nil {
		return err
	}
	var first error
	for _, sub := range subs {
		planned := sub.NextRunAt
		if planned == nil {
			nextErr := validateTopicSubscription(sub, now)
			if nextErr != nil {
				if first == nil {
					first = nextErr
				}
				continue
			}
			planned = sub.NextRunAt
		}
		if planned == nil {
			if first == nil {
				first = errors.New("due topic subscription has no planned time")
			}
			continue
		}
		digest, runErr := s.Run(ctx, RunTopicDigestParams{UserID: sub.UserID, SubscriptionID: sub.ID, PlannedAt: planned})
		if runErr != nil && first == nil {
			first = runErr
		}
		// An unrelated active run is reused by Run. Keep this occurrence due
		// until it owns a durable digest, so overlapping manual work cannot drop it.
		if digest == nil || digest.OccurrenceKey != model.ScheduledTopicOccurrenceKey(sub.ID, *planned) {
			continue
		}
		next, nextErr := nextTopicRun(sub, now)
		if nextErr == nil {
			nextErr = s.repo.TopicSubscriptions().AdvanceSchedule(ctx, sub, next)
		}
		if nextErr != nil && first == nil {
			first = nextErr
		}
	}
	return first
}

func (s *TopicRadarService) RecoverTopicRadarDispatches(ctx context.Context, limit int) error {
	if limit <= 0 {
		limit = 100
	}
	var first error
	digests, err := s.repo.TopicDigests().ListDispatchable(ctx, limit)
	if err != nil {
		return err
	}
	for _, d := range digests {
		if e := topicRadarRecoveryError(s.ProcessTopicDigest(ctx, d.ID)); e != nil && first == nil {
			first = e
		}
	}
	scripts, err := s.repo.TopicScriptJobs().ListDispatchable(ctx, limit)
	if err != nil {
		return err
	}
	for _, j := range scripts {
		if e := topicRadarRecoveryError(s.ProcessTopicScript(ctx, j.ID)); e != nil && first == nil {
			first = e
		}
	}
	return first
}

func topicRadarRecoveryError(err error) error {
	if errors.Is(err, ErrDispatchInProgress) {
		return nil
	}
	return err
}
