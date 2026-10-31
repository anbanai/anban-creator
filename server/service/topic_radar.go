package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

var (
	ErrTopicSubscriptionNotFound = errors.New("topic subscription not found")
	ErrTopicProjectAccessDenied  = errors.New("topic project access denied")
	ErrTopicSubscriptionInvalid  = errors.New("topic subscription is invalid")
)

type TopicRadarTaskEnqueuer interface {
	EnqueueUnique(taskType string, payload []byte, uniqueKey string) (bool, error)
}

type TopicRadarService struct {
	repo               repository.Repository
	enqueuer           TopicRadarTaskEnqueuer
	taskSvc            *TaskService
	now                func() time.Time
	notificationClient *http.Client
}

func (s *TopicRadarService) SetTaskService(taskSvc *TaskService) {
	if s != nil {
		s.taskSvc = taskSvc
	}
}

func NewTopicRadarService(repo repository.Repository, enqueuer TopicRadarTaskEnqueuer, now func() time.Time) *TopicRadarService {
	if now == nil {
		now = time.Now
	}
	return &TopicRadarService{repo: repo, enqueuer: enqueuer, now: now, notificationClient: newTopicNotificationHTTPClient()}
}

type CreateTopicSubscriptionParams struct {
	UserID       string
	ProjectID    string
	Name         string
	Enabled      bool
	Timezone     string
	Schedule     datatypes.JSON
	SourceConfig datatypes.JSON
	Weights      datatypes.JSON
	ScriptPolicy datatypes.JSON
}

type RunTopicDigestParams struct {
	UserID         string
	SubscriptionID string
	RequestKey     string
	PlannedAt      *time.Time
}

func (s *TopicRadarService) CreateSubscription(ctx context.Context, p CreateTopicSubscriptionParams) (*model.TopicSubscription, error) {
	if strings.TrimSpace(p.UserID) == "" || strings.TrimSpace(p.ProjectID) == "" || strings.TrimSpace(p.Name) == "" {
		return nil, fmt.Errorf("%w: user_id, project_id and name are required", ErrTopicSubscriptionInvalid)
	}
	if _, err := s.projectForUser(ctx, p.UserID, p.ProjectID); err != nil {
		return nil, err
	}
	timezone := strings.TrimSpace(p.Timezone)
	if timezone == "" {
		timezone = "Asia/Shanghai"
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return nil, fmt.Errorf("%w: invalid timezone %q", ErrTopicSubscriptionInvalid, timezone)
	}
	item := &model.TopicSubscription{
		ID:           uuid.NewString(),
		UserID:       p.UserID,
		ProjectID:    p.ProjectID,
		Name:         strings.TrimSpace(p.Name),
		Enabled:      p.Enabled,
		Timezone:     timezone,
		Schedule:     defaultJSON(p.Schedule, `{"hour":9,"minute":0}`),
		SourceConfig: defaultJSON(p.SourceConfig, `{"platforms":[]}`),
		Weights:      defaultJSON(p.Weights, topicJSON(model.DefaultTopicRadarWeights())),
		ScriptPolicy: defaultJSON(p.ScriptPolicy, `{"enabled":false}`),
	}
	if err := validateTopicSubscription(item, s.now()); err != nil {
		return nil, err
	}
	if !item.Enabled {
		item.NextRunAt = nil
	}
	if err := s.repo.TopicSubscriptions().Create(ctx, item); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *TopicRadarService) Run(ctx context.Context, p RunTopicDigestParams) (*model.TopicDigest, error) {
	if strings.TrimSpace(p.UserID) == "" || strings.TrimSpace(p.SubscriptionID) == "" {
		return nil, fmt.Errorf("%w: user_id and subscription_id are required", ErrTopicSubscriptionInvalid)
	}
	subscription, err := s.repo.TopicSubscriptions().FindByID(ctx, p.SubscriptionID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTopicSubscriptionNotFound
	}
	if err != nil {
		return nil, err
	}
	if subscription.UserID != p.UserID {
		return nil, ErrTopicProjectAccessDenied
	}
	if subscription.DeletedAt != nil {
		return nil, ErrTopicSubscriptionNotFound
	}
	project, err := s.projectForUser(ctx, p.UserID, subscription.ProjectID)
	if err != nil {
		return nil, err
	}
	if active, activeErr := s.repo.TopicDigests().FindActiveBySubscription(ctx, subscription.ID); activeErr == nil {
		return active, nil
	} else if !errors.Is(activeErr, gorm.ErrRecordNotFound) {
		return nil, activeErr
	}
	requestKey := strings.TrimSpace(p.RequestKey)
	if requestKey == "" {
		requestKey = uuid.NewString()
	}
	plannedAt := s.now()
	if p.PlannedAt != nil {
		plannedAt = p.PlannedAt.UTC()
	}
	occurrenceKey := "manual:" + subscription.ID + ":" + requestKey
	if p.PlannedAt != nil {
		occurrenceKey = model.ScheduledTopicOccurrenceKey(subscription.ID, plannedAt)
	}
	frozen, err := s.collectTopicInputs(ctx, subscription, project)
	if err != nil {
		return nil, err
	}
	snapshot, hash, err := model.NewTopicDigestInput(frozen)
	if err != nil {
		return nil, err
	}
	activeKey := subscription.ID
	digest := &model.TopicDigest{
		ID:             uuid.NewString(),
		SubscriptionID: subscription.ID,
		UserID:         p.UserID,
		ProjectID:      subscription.ProjectID,
		OccurrenceKey:  occurrenceKey,
		RequestKey:     requestKey,
		PlannedAt:      plannedAt,
		Status:         model.TopicDigestStatusQueued,
		DispatchStatus: model.TopicDigestDispatchPending,
		ActiveKey:      &activeKey,
		InputSnapshot:  snapshot,
		InputHash:      hash,
	}
	err = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		locked, lockErr := tx.TopicSubscriptions().LockByID(ctx, subscription.ID)
		if errors.Is(lockErr, gorm.ErrRecordNotFound) {
			return ErrTopicSubscriptionNotFound
		}
		if lockErr != nil {
			return lockErr
		}
		if locked.UserID != p.UserID || locked.ProjectID != subscription.ProjectID {
			return ErrTopicProjectAccessDenied
		}
		if locked.DeletedAt != nil {
			return ErrTopicSubscriptionNotFound
		}
		return tx.TopicDigests().CreateIfAbsent(ctx, digest)
	})
	if err != nil {
		if active, activeErr := s.repo.TopicDigests().FindActiveBySubscription(ctx, subscription.ID); activeErr == nil {
			return active, nil
		}
		return nil, err
	}
	persisted, err := s.repo.TopicDigests().FindByOccurrence(ctx, occurrenceKey)
	if err != nil {
		return nil, err
	}
	if persisted.ID != digest.ID {
		return persisted, nil
	}
	payload, err := json.Marshal(map[string]string{"digest_id": persisted.ID})
	if err != nil {
		return nil, err
	}
	if s.enqueuer == nil {
		return persisted, nil
	}
	if _, err := s.enqueuer.EnqueueUnique(model.TaskTypeTopicDigest, payload, persisted.ID); err != nil {
		updateErr := s.repo.TopicDigests().MarkDispatch(ctx, persisted.ID, model.TopicDigestDispatchFailed, "queue unavailable; automatic recovery pending")
		return persisted, errors.Join(err, updateErr)
	}
	if err := s.repo.TopicDigests().MarkDispatch(ctx, persisted.ID, model.TopicDigestDispatchEnqueued, ""); err != nil {
		return persisted, err
	}
	current, err := s.repo.TopicDigests().FindByID(ctx, persisted.ID)
	if err != nil {
		return persisted, err
	}
	return current, nil
}

func (s *TopicRadarService) Retry(ctx context.Context, userID, digestID string) (*model.TopicDigest, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(digestID) == "" {
		return nil, fmt.Errorf("%w: user_id and digest_id are required", ErrTopicSubscriptionInvalid)
	}
	previous, err := s.repo.TopicDigests().FindByID(ctx, digestID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, gorm.ErrRecordNotFound
	}
	if err != nil {
		return nil, err
	}
	if previous.UserID != userID {
		return nil, ErrTopicProjectAccessDenied
	}
	if previous.Status != model.TopicDigestStatusSucceeded && previous.Status != model.TopicDigestStatusFailed {
		return nil, fmt.Errorf("digest %s is not retryable in status %q", previous.ID, previous.Status)
	}
	return s.Run(ctx, RunTopicDigestParams{
		UserID:         userID,
		SubscriptionID: previous.SubscriptionID,
		RequestKey:     "retry:" + uuid.NewString(),
	})
}

func (s *TopicRadarService) projectForUser(ctx context.Context, userID, projectID string) (*model.Project, error) {
	project, err := s.repo.Projects().FindByID(ctx, projectID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTopicProjectAccessDenied
	}
	if err != nil {
		return nil, err
	}
	if project.UserID != userID || project.Status != model.ProjectStatusActive {
		return nil, ErrTopicProjectAccessDenied
	}
	return project, nil
}

func defaultJSON(value datatypes.JSON, fallback string) datatypes.JSON {
	if len(value) == 0 || string(value) == "null" {
		return datatypes.JSON([]byte(fallback))
	}
	return value
}

func topicJSON(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}
