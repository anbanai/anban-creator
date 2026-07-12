package auth

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	executionTokenIssuer     = "anban-server"
	executionTokenAudience   = "anban-agent-execution"
	minimumExecutionSecret   = 32
	maximumExecutionLifetime = time.Hour
)

// ExecutionClaims bind an agent credential to one durable execution attempt.
type ExecutionClaims struct {
	UserID      string `json:"user_id"`
	ProjectID   string `json:"project_id"`
	TaskID      string `json:"task_id"`
	ExecutionID string `json:"execution_id"`
	jwt.RegisteredClaims
}

// ExecutionTokenService issues and validates short-lived agent execution JWTs.
type ExecutionTokenService struct {
	secret []byte
	now    func() time.Time
}

func NewExecutionTokenService(secret string) (*ExecutionTokenService, error) {
	if len(strings.TrimSpace(secret)) < minimumExecutionSecret {
		return nil, fmt.Errorf("execution token secret must contain at least %d characters", minimumExecutionSecret)
	}
	return &ExecutionTokenService{secret: []byte(secret), now: time.Now}, nil
}

// Issue creates a token whose expiry must be chosen no later than the Job deadline.
func (s *ExecutionTokenService) Issue(identity ExecutionClaims, expiresAt time.Time) (string, error) {
	if s == nil || s.now == nil {
		return "", errors.New("execution token service is not configured")
	}
	return s.IssueAt(identity, s.now().UTC(), expiresAt)
}

// IssueAt issues a token from one caller-sampled clock value so related
// credential deadlines can be calculated from the same instant.
func (s *ExecutionTokenService) IssueAt(identity ExecutionClaims, issuedAt, expiresAt time.Time) (string, error) {
	if s == nil || len(s.secret) == 0 {
		return "", errors.New("execution token service is not configured")
	}
	if err := validateExecutionIdentity(identity); err != nil {
		return "", err
	}
	issuedAt = issuedAt.UTC()
	expiresAt = expiresAt.UTC()
	if !expiresAt.After(issuedAt) {
		return "", errors.New("execution token expiry must be in the future")
	}
	if expiresAt.Sub(issuedAt) > maximumExecutionLifetime {
		return "", errors.New("execution token lifetime exceeds maximum")
	}
	identity.RegisteredClaims = jwt.RegisteredClaims{
		Issuer:    executionTokenIssuer,
		Subject:   identity.ExecutionID,
		Audience:  jwt.ClaimStrings{executionTokenAudience},
		ExpiresAt: jwt.NewNumericDate(expiresAt.UTC()),
		NotBefore: jwt.NewNumericDate(issuedAt),
		IssuedAt:  jwt.NewNumericDate(issuedAt),
		ID:        uuid.NewString(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, identity).SignedString(s.secret)
}

func (s *ExecutionTokenService) Validate(raw string) (*ExecutionClaims, error) {
	if s == nil || len(s.secret) == 0 {
		return nil, errors.New("execution token service is not configured")
	}
	claims := &ExecutionClaims{}
	token, err := jwt.ParseWithClaims(strings.TrimSpace(raw), claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected execution token signing method %q", token.Method.Alg())
		}
		return s.secret, nil
	}, jwt.WithIssuer(executionTokenIssuer), jwt.WithAudience(executionTokenAudience), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithTimeFunc(s.now))
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("invalid execution token: %w", err)
	}
	if err := validateExecutionIdentity(*claims); err != nil {
		return nil, err
	}
	if claims.Subject != claims.ExecutionID {
		return nil, errors.New("execution token subject does not match execution identity")
	}
	if claims.IssuedAt == nil || claims.NotBefore == nil || strings.TrimSpace(claims.ID) == "" {
		return nil, errors.New("execution token registered identity is incomplete")
	}
	if claims.ExpiresAt.Time.Sub(claims.IssuedAt.Time) > maximumExecutionLifetime {
		return nil, errors.New("execution token lifetime exceeds maximum")
	}
	return claims, nil
}

func validateExecutionIdentity(claims ExecutionClaims) error {
	if strings.TrimSpace(claims.UserID) == "" || strings.TrimSpace(claims.ProjectID) == "" || strings.TrimSpace(claims.TaskID) == "" || strings.TrimSpace(claims.ExecutionID) == "" {
		return errors.New("execution token identity is incomplete")
	}
	return nil
}
