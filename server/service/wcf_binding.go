package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/wcf"
)

// Sentinel errors for the WeChat-binding lifecycle. The handler maps these to
// HTTP statuses; callers should use errors.Is to match.
var (
	ErrWCFUnavailable  = errors.New("wcf sidecar unavailable")
	ErrAlreadyBound    = errors.New("wechat account already bound")
	ErrAccountConflict = errors.New("wechat account already bound by another user")
	ErrNoBinding       = errors.New("no wechat binding")
	ErrLoginNotPending = errors.New("no pending wechat login to poll")
)

// WCFBindingService orchestrates the WeChat-binding lifecycle against the wcfLink
// sidecar: start a QR login, poll for confirmation, capture the account id, and
// unbind. It also resolves the default project used by inbound create-task
// commands. All methods are nil-client-safe: when wcf is disabled the service is
// still constructed so GetStatus can report availability, but mutating methods
// return ErrWCFUnavailable.
type WCFBindingService struct {
	repo   repository.Repository
	client *wcf.Client
	logger *zerolog.Logger
}

// NewWCFBindingService constructs the binding service. client may be nil when
// wcf is disabled (HealthCheck failed at startup); the service stays usable for
// status reads but refuses mutations.
func NewWCFBindingService(repo repository.Repository, client *wcf.Client, logger *zerolog.Logger) *WCFBindingService {
	return &WCFBindingService{repo: repo, client: client, logger: logger}
}

// Available reports whether the wcf sidecar client is wired.
func (s *WCFBindingService) Available() bool { return s != nil && s.client != nil }

// StartBindResult is returned to Studio: the QR PNG as a data URI plus the login
// session id the client echoes back on each status poll.
type StartBindResult struct {
	SessionID string `json:"session_id"`
	QRCodeURL string `json:"qrcode_url"` // data:image/png;base64,...
	Status    string `json:"status"`     // always "pending" here
}

// BindStatusResult is the current binding/login state. Status is one of:
// "unbound" (no row), "pending" (login started), "active" (confirmed),
// "wait"/"scanned" (login in progress), "expired"/"error" (login failed).
type BindStatusResult struct {
	Available        bool       `json:"available"`
	Status           string     `json:"status"`
	AccountID        string     `json:"account_id,omitempty"`
	DefaultProjectID string     `json:"default_project_id,omitempty"`
	BoundAt          *time.Time `json:"bound_at,omitempty"`
}

// StartBind starts a wcfLink QR login for the user, persists a pending binding
// row, and returns the QR as a data URI. Refuses if the user already has an
// active binding. The login session id is returned for the client to poll with.
func (s *WCFBindingService) StartBind(ctx context.Context, userID string) (StartBindResult, error) {
	if !s.Available() {
		return StartBindResult{}, ErrWCFUnavailable
	}
	if existing, err := s.repo.WCFBindings().FindByUserID(ctx, userID); err == nil && existing != nil {
		if existing.Status == model.WCFBindingStatusActive {
			return StartBindResult{}, ErrAlreadyBound
		}
	}

	session, err := s.client.StartLogin(ctx, "")
	if err != nil {
		return StartBindResult{}, fmt.Errorf("start wcf login: %w", err)
	}
	if session.SessionID == "" {
		return StartBindResult{}, fmt.Errorf("wcf login returned empty session_id")
	}

	png, err := s.client.GetLoginQR(ctx, session.SessionID)
	if err != nil {
		return StartBindResult{}, fmt.Errorf("fetch wcf qr: %w", err)
	}

	// Upsert a pending binding row so GetStatus can show "binding in progress".
	// The login session id is stored server-side and re-checked at PollBindStatus
	// so an authenticated user can't poll ANOTHER user's in-flight login — wcfLink
	// mints session ids as "login_<unixnano>" (sequential, not random).
	if existing, err := s.repo.WCFBindings().FindByUserID(ctx, userID); err == nil && existing != nil {
		// One atomic write (Save): flip to pending AND stamp the login session id
		// in a single UPDATE. Two separate writes would leave the row pending
		// under the PREVIOUS session id if the second failed — then PollBindStatus's
		// ownership check would reject the user's own (new) id and lock them out.
		existing.Status = model.WCFBindingStatusPending
		existing.LoginSessionID = session.SessionID
		if err := s.repo.WCFBindings().Update(ctx, existing); err != nil {
			return StartBindResult{}, fmt.Errorf("persist wcf binding: %w", err)
		}
	} else {
		now := time.Now()
		binding := &model.WCFBinding{
			ID:             uuid.NewString(),
			UserID:         userID,
			LoginSessionID: session.SessionID,
			Status:         model.WCFBindingStatusPending,
			LastSeenAt:     now,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if err := s.repo.WCFBindings().Create(ctx, binding); err != nil {
			return StartBindResult{}, fmt.Errorf("create wcf binding: %w", err)
		}
	}

	return StartBindResult{
		SessionID: session.SessionID,
		QRCodeURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
		Status:    "pending",
	}, nil
}

// PollBindStatus polls a pending login. On iLink confirmation it captures the
// wcfLink account id, rejects account reuse by another user, and flips the
// binding to active. sessionID is the id StartBind returned (the server is
// otherwise stateless across polls).
func (s *WCFBindingService) PollBindStatus(ctx context.Context, userID, sessionID string) (BindStatusResult, error) {
	if !s.Available() {
		return BindStatusResult{Available: false}, ErrWCFUnavailable
	}
	if strings.TrimSpace(sessionID) == "" {
		return BindStatusResult{Available: true}, ErrLoginNotPending
	}

	binding, err := s.repo.WCFBindings().FindByUserID(ctx, userID)
	if err != nil || binding == nil {
		return BindStatusResult{Available: true, Status: "unbound"}, ErrLoginNotPending
	}
	// Ownership: the supplied session id must be exactly the one THIS user's
	// bind started. wcfLink session ids are sequential ("login_<unixnano>"), so
	// without this check an authenticated user could poll another user's in-flight
	// login and bind that WeChat to their own account. Strict equality — no legacy
	// leniency: every pending row is created via StartBind, which stamps the
	// session id in the same atomic write as the pending status flip, so a pending
	// row always carries its id. A pre-fix pending row (empty id, only reachable
	// at first deploy) is rejected once and self-heals when the user restarts
	// StartBind. A lenient "" branch would instead let ANY guessed id through on a
	// legacy row, reopening the very window this check exists to close.
	if binding.LoginSessionID != sessionID {
		return BindStatusResult{Available: true}, ErrLoginNotPending
	}

	session, err := s.client.GetLoginStatus(ctx, sessionID)
	if err != nil {
		return BindStatusResult{}, fmt.Errorf("poll wcf login: %w", err)
	}

	if !session.Confirmed() {
		// Map iLink status to a client-facing state; unknown → raw status.
		status := session.Status
		if status == "" {
			status = "wait"
		}
		return BindStatusResult{Available: true, Status: status}, nil
	}

	// Confirmed: capture account id. Reject if another user already owns it.
	if other, err := s.repo.WCFBindings().FindByWCFAccountID(ctx, session.AccountID); err == nil && other != nil && other.UserID != userID {
		return BindStatusResult{Available: true}, ErrAccountConflict
	}

	binding.WCFAccountID = session.AccountID
	if err := s.repo.WCFBindings().Update(ctx, binding); err != nil {
		return BindStatusResult{}, fmt.Errorf("persist wcf account id: %w", err)
	}
	if err := s.repo.WCFBindings().UpdateStatus(ctx, userID, model.WCFBindingStatusActive); err != nil {
		return BindStatusResult{}, fmt.Errorf("activate wcf binding: %w", err)
	}

	// Re-read to pick up bound_at set by UpdateStatus.
	if refreshed, err := s.repo.WCFBindings().FindByUserID(ctx, userID); err == nil && refreshed != nil {
		binding = refreshed
	}
	return BindStatusResult{
		Available: true,
		Status:    model.WCFBindingStatusActive,
		AccountID: binding.WCFAccountID,
		BoundAt:   binding.BoundAt,
	}, nil
}

// Unbind removes the user's binding row. wcfLink has no HTTP logout route, so the
// wcfLink-side account persists until the sidecar restarts (documented gap); the
// binding row is deleted so notifications stop and commands are ignored.
func (s *WCFBindingService) Unbind(ctx context.Context, userID string) error {
	if _, err := s.repo.WCFBindings().FindByUserID(ctx, userID); err != nil {
		return ErrNoBinding
	}
	return s.repo.WCFBindings().Delete(ctx, userID)
}

// GetStatus returns the current binding state (or "unbound" when none). Always
// reports availability so Studio can show "微信通知未启用" when wcf is down.
func (s *WCFBindingService) GetStatus(ctx context.Context, userID string) (BindStatusResult, error) {
	if !s.Available() {
		return BindStatusResult{Available: false, Status: "unbound"}, nil
	}
	binding, err := s.repo.WCFBindings().FindByUserID(ctx, userID)
	if err != nil || binding == nil {
		return BindStatusResult{Available: true, Status: "unbound"}, nil
	}
	return BindStatusResult{
		Available:        true,
		Status:           binding.Status,
		AccountID:        binding.WCFAccountID,
		DefaultProjectID: binding.DefaultProjectID,
		BoundAt:          binding.BoundAt,
	}, nil
}

// SetDefaultProject records the project used by inbound create-task commands.
// Validates the project belongs to the caller and is active.
func (s *WCFBindingService) SetDefaultProject(ctx context.Context, userID, projectID string) error {
	if strings.TrimSpace(projectID) == "" {
		return errors.New("project_id is required")
	}
	binding, err := s.repo.WCFBindings().FindByUserID(ctx, userID)
	if err != nil || binding == nil {
		return ErrNoBinding
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
	return s.repo.WCFBindings().UpdateDefaultProject(ctx, userID, projectID)
}
