package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

var (
	ErrIlinkUnavailable = errors.New("ilink unavailable")
	ErrIlinkNoBinding   = errors.New("no ilink binding")
	ErrIlinkBindInvalid = errors.New("invalid or expired ilink bind code")
)

type IlinkAssistantAccount struct {
	AccountID   string `json:"account_id"`
	DisplayName string `json:"display_name"`
	WechatID    string `json:"wechat_id,omitempty"`
	QRCodeURL   string `json:"qrcode_url,omitempty"`
}

type IlinkBindCodeResult struct {
	Available bool                   `json:"available"`
	BindCode  string                 `json:"bind_code"`
	ExpiresAt time.Time              `json:"expires_at"`
	Assistant *IlinkAssistantAccount `json:"assistant,omitempty"`
	Status    string                 `json:"status"`
}

type IlinkStatusResult struct {
	Available        bool                   `json:"available"`
	Bound            bool                   `json:"bound"`
	Status           string                 `json:"status"`
	DefaultProjectID string                 `json:"default_project_id,omitempty"`
	Assistant        *IlinkAssistantAccount `json:"assistant,omitempty"`
	BoundAt          *time.Time             `json:"bound_at,omitempty"`
}

type IlinkBindingService struct {
	repo      repository.Repository
	enabled   bool
	assistant *IlinkAssistantAccount
	logger    *zerolog.Logger
	nowFunc   func() time.Time
	codeFunc  func() (string, error)
}

func NewIlinkBindingService(repo repository.Repository, enabled bool, assistant *IlinkAssistantAccount, logger *zerolog.Logger) *IlinkBindingService {
	return &IlinkBindingService{
		repo:      repo,
		enabled:   enabled,
		assistant: assistant,
		logger:    logger,
		nowFunc:   time.Now,
		codeFunc:  randomIlinkCode,
	}
}

func (s *IlinkBindingService) Available() bool {
	return s != nil && s.enabled && s.repo != nil
}

func (s *IlinkBindingService) CreateBindCode(ctx context.Context, userID string) (IlinkBindCodeResult, error) {
	if !s.Available() {
		return IlinkBindCodeResult{Available: false, Status: model.IlinkBindingStatusPending}, ErrIlinkUnavailable
	}
	now := s.now()
	code, err := s.codeFunc()
	if err != nil {
		return IlinkBindCodeResult{}, err
	}
	expiresAt := now.Add(10 * time.Minute)

	binding, err := s.repo.IlinkBindings().FindByUserID(ctx, userID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return IlinkBindCodeResult{}, err
	}
	if binding == nil {
		binding = &model.IlinkBinding{
			ID:        uuid.NewString(),
			UserID:    userID,
			CreatedAt: now,
		}
	}
	binding.BindCode = stringPtr(code)
	binding.BindCodeExpiresAt = &expiresAt
	if binding.Status != model.IlinkBindingStatusActive {
		binding.Status = model.IlinkBindingStatusPending
	}
	binding.UpdatedAt = now
	binding.LastSeenAt = now
	if binding.ID == "" {
		binding.ID = uuid.NewString()
	}
	if binding.CreatedAt.IsZero() {
		if err := s.repo.IlinkBindings().Create(ctx, binding); err != nil {
			return IlinkBindCodeResult{}, err
		}
	} else if err := s.repo.IlinkBindings().Update(ctx, binding); err != nil {
		return IlinkBindCodeResult{}, err
	}
	return IlinkBindCodeResult{
		Available: true,
		BindCode:  code,
		ExpiresAt: expiresAt,
		Assistant: s.assistant,
		Status:    model.IlinkBindingStatusPending,
	}, nil
}

func (s *IlinkBindingService) CompleteBindByCode(ctx context.Context, code, platformAccountID, externalUserID string) (*model.IlinkBinding, error) {
	if !s.Available() {
		return nil, ErrIlinkUnavailable
	}
	code = strings.TrimSpace(code)
	if code == "" || strings.TrimSpace(platformAccountID) == "" || strings.TrimSpace(externalUserID) == "" {
		return nil, ErrIlinkBindInvalid
	}
	now := s.now()
	binding, err := s.repo.IlinkBindings().FindByBindCode(ctx, code, now)
	if err != nil {
		return nil, ErrIlinkBindInvalid
	}
	binding.PlatformAccountID = stringPtr(platformAccountID)
	binding.ExternalUserID = stringPtr(externalUserID)
	binding.BindCode = nil
	binding.BindCodeExpiresAt = nil
	binding.Status = model.IlinkBindingStatusActive
	binding.BoundAt = &now
	binding.LastSeenAt = now
	binding.UpdatedAt = now
	if err := s.repo.IlinkBindings().Update(ctx, binding); err != nil {
		return nil, err
	}
	return binding, nil
}

func (s *IlinkBindingService) GetStatus(ctx context.Context, userID string) (IlinkStatusResult, error) {
	if !s.Available() {
		return IlinkStatusResult{Available: false, Status: "unbound", Assistant: s.assistant}, nil
	}
	binding, err := s.repo.IlinkBindings().FindByUserID(ctx, userID)
	if err != nil || binding == nil {
		return IlinkStatusResult{Available: true, Status: "unbound", Assistant: s.assistant}, nil
	}
	return IlinkStatusResult{
		Available:        true,
		Bound:            binding.Status == model.IlinkBindingStatusActive,
		Status:           binding.Status,
		DefaultProjectID: binding.DefaultProjectID,
		Assistant:        s.assistant,
		BoundAt:          binding.BoundAt,
	}, nil
}

func (s *IlinkBindingService) Unbind(ctx context.Context, userID string) error {
	if !s.Available() {
		return ErrIlinkUnavailable
	}
	if _, err := s.repo.IlinkBindings().FindByUserID(ctx, userID); err != nil {
		return ErrIlinkNoBinding
	}
	return s.repo.IlinkBindings().Delete(ctx, userID)
}

func (s *IlinkBindingService) SetDefaultProject(ctx context.Context, userID, projectID string) error {
	if strings.TrimSpace(projectID) == "" {
		return errors.New("project_id is required")
	}
	binding, err := s.repo.IlinkBindings().FindByUserID(ctx, userID)
	if err != nil || binding == nil || binding.Status != model.IlinkBindingStatusActive {
		return ErrIlinkNoBinding
	}
	project, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return fmt.Errorf("project not found: %w", err)
	}
	if project.UserID != userID {
		return fmt.Errorf("project does not belong to user")
	}
	if project.Status != model.ProjectStatusActive {
		return errors.New("project is not active")
	}
	return s.repo.IlinkBindings().UpdateDefaultProject(ctx, userID, projectID)
}

func (s *IlinkBindingService) now() time.Time {
	if s != nil && s.nowFunc != nil {
		return s.nowFunc()
	}
	return time.Now()
}

func randomIlinkCode() (string, error) {
	const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	var b [8]byte
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		b[i] = alphabet[n.Int64()]
	}
	return string(b[:]), nil
}
