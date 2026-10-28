package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
	"gorm.io/gorm"
)

var (
	ErrTaskShareNotFound      = errors.New("share not found")
	ErrTaskShareGone          = errors.New("share expired or revoked")
	ErrTaskShareUnavailable   = errors.New("shared content is unavailable")
	ErrTaskShareInvalidExpiry = errors.New("expires_in_days must be an integer from 1 to 365")
)

type TaskShareStatus struct {
	Status    string     `json:"status"`
	SharePath string     `json:"share_path,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type TaskShareService struct {
	repo  repository.Repository
	store storage.Provider
	now   func() time.Time
}

func NewTaskShareService(repo repository.Repository, store storage.Provider) *TaskShareService {
	return &TaskShareService{repo: repo, store: store, now: time.Now}
}

func shareableTask(task *model.Task) bool {
	return task != nil && task.DeletingAt == nil && task.TaskKind == model.TaskKindContentGeneration &&
		((task.AgentID == model.AgentIDArticle && task.Channel == model.ChannelArticle) ||
			(task.AgentID == model.AgentIDSeednote && task.Channel == model.ChannelSeednote))
}

func shareStatus(share *model.TaskShare, now time.Time) *TaskShareStatus {
	if share == nil {
		return &TaskShareStatus{Status: "none"}
	}
	status := "active"
	if share.RevokedAt != nil {
		status = "revoked"
	} else if !now.Before(share.ExpiresAt) {
		status = "expired"
	}
	return &TaskShareStatus{Status: status, SharePath: "/share/" + share.Token, ExpiresAt: &share.ExpiresAt}
}

func (s *TaskShareService) withOwnedTask(ctx context.Context, userID, taskID string, fn func(repository.Repository, *model.Task) error) error {
	return s.repo.WithTx(ctx, func(tx repository.Repository) error {
		task, err := tx.Tasks().FindByIDForUpdate(ctx, taskID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrTaskShareNotFound
		}
		if err != nil {
			return err
		}
		if userID == "" || task.UserID != userID || !shareableTask(task) {
			return ErrTaskShareNotFound
		}
		return fn(tx, task)
	})
}

func (s *TaskShareService) Get(ctx context.Context, userID, taskID string) (*TaskShareStatus, error) {
	var result *TaskShareStatus
	err := s.withOwnedTask(ctx, userID, taskID, func(tx repository.Repository, _ *model.Task) error {
		share, err := tx.TaskShares().FindByTaskID(ctx, taskID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		result = shareStatus(share, s.now())
		return nil
	})
	return result, err
}

func (s *TaskShareService) Put(ctx context.Context, userID, taskID string, days int) (*TaskShareStatus, error) {
	if days < 1 || days > 365 {
		return nil, ErrTaskShareInvalidExpiry
	}
	var result *TaskShareStatus
	err := s.withOwnedTask(ctx, userID, taskID, func(tx repository.Repository, task *model.Task) error {
		share, err := tx.TaskShares().FindByTaskID(ctx, taskID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		validationToken := ""
		if share != nil {
			validationToken = share.Token
		}
		reader := &TaskShareService{repo: tx, store: s.store, now: s.now}
		if _, err := reader.content(ctx, task, validationToken, time.Time{}); err != nil {
			return err
		}
		now := s.now().UTC()
		if share == nil {
			share = &model.TaskShare{TaskID: taskID, UserID: userID, CreatedAt: now}
		}
		if share.Token == "" || shareStatus(share, now).Status != "active" {
			var random [32]byte
			if _, err := rand.Read(random[:]); err != nil {
				return err
			}
			share.Token = base64.RawURLEncoding.EncodeToString(random[:])
		}
		share.ExpiresAt, share.RevokedAt, share.UpdatedAt = now.Add(time.Duration(days)*24*time.Hour), nil, now
		if err := tx.TaskShares().Save(ctx, share); err != nil {
			return err
		}
		result = shareStatus(share, now)
		return nil
	})
	return result, err
}

func (s *TaskShareService) Revoke(ctx context.Context, userID, taskID string) error {
	return s.withOwnedTask(ctx, userID, taskID, func(tx repository.Repository, _ *model.Task) error {
		share, err := tx.TaskShares().FindByTaskID(ctx, taskID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if share.RevokedAt != nil {
			return nil
		}
		now := s.now().UTC()
		share.RevokedAt, share.UpdatedAt = &now, now
		return tx.TaskShares().Save(ctx, share)
	})
}

func (s *TaskShareService) publicTask(ctx context.Context, token string) (*model.TaskShare, *model.Task, error) {
	if len(token) != 43 {
		return nil, nil, ErrTaskShareNotFound
	}
	share, err := s.repo.TaskShares().FindByToken(ctx, token)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, ErrTaskShareNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	if share.Token != token {
		return nil, nil, ErrTaskShareNotFound
	}
	task, err := s.repo.Tasks().FindByID(ctx, share.TaskID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, ErrTaskShareNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	if !shareableTask(task) || task.UserID != share.UserID {
		return nil, nil, ErrTaskShareNotFound
	}
	if shareStatus(share, s.now()).Status != "active" {
		return nil, nil, ErrTaskShareGone
	}
	return share, task, nil
}
