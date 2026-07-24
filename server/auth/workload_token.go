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
	workloadTokenIssuer     = "anban-workload-dispatcher"
	workloadTokenAudience   = "anban-server-workload-bootstrap"
	maximumWorkloadLifetime = time.Hour
)

// WorkloadClaims bind a bootstrap credential to one provider-owned runtime.
type WorkloadClaims struct {
	RuntimeScope      string `json:"runtime_scope"`
	RuntimeWorkload   string `json:"runtime_workload"`
	RuntimeInstanceID string `json:"runtime_instance_id"`
	UserID            string `json:"user_id"`
	ProjectID         string `json:"project_id"`
	TaskID            string `json:"task_id"`
	ExecutionID       string `json:"execution_id"`
	jwt.RegisteredClaims
}

// WorkloadTokenService issues and validates provider workload JWTs used only
// to exchange a verified runtime identity for an execution token.
type WorkloadTokenService struct {
	secret []byte
	now    func() time.Time
}

func NewWorkloadTokenService(secret string) (*WorkloadTokenService, error) {
	if len(strings.TrimSpace(secret)) < minimumExecutionSecret {
		return nil, fmt.Errorf("workload token secret must contain at least %d characters", minimumExecutionSecret)
	}
	return &WorkloadTokenService{secret: []byte(secret), now: time.Now}, nil
}

func (s *WorkloadTokenService) Issue(identity WorkloadClaims, expiresAt time.Time) (string, error) {
	if s == nil || s.now == nil {
		return "", errors.New("workload token service is not configured")
	}
	return s.IssueAt(identity, s.now().UTC(), expiresAt)
}

func (s *WorkloadTokenService) IssueAt(identity WorkloadClaims, issuedAt, expiresAt time.Time) (string, error) {
	if s == nil || len(s.secret) == 0 {
		return "", errors.New("workload token service is not configured")
	}
	if err := validateWorkloadIdentity(identity); err != nil {
		return "", err
	}
	issuedAt = issuedAt.UTC()
	expiresAt = expiresAt.UTC()
	if !expiresAt.After(issuedAt) {
		return "", errors.New("workload token expiry must be in the future")
	}
	if expiresAt.Sub(issuedAt) > maximumWorkloadLifetime {
		return "", errors.New("workload token lifetime exceeds maximum")
	}
	identity.RegisteredClaims = jwt.RegisteredClaims{
		Issuer:    workloadTokenIssuer,
		Subject:   identity.ExecutionID,
		Audience:  jwt.ClaimStrings{workloadTokenAudience},
		ExpiresAt: jwt.NewNumericDate(expiresAt),
		NotBefore: jwt.NewNumericDate(issuedAt),
		IssuedAt:  jwt.NewNumericDate(issuedAt),
		ID:        uuid.NewString(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, identity).SignedString(s.secret)
}

func (s *WorkloadTokenService) Validate(raw string) (*WorkloadClaims, error) {
	if s == nil || len(s.secret) == 0 || s.now == nil {
		return nil, errors.New("workload token service is not configured")
	}
	claims := &WorkloadClaims{}
	token, err := jwt.ParseWithClaims(strings.TrimSpace(raw), claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected workload token signing method %q", token.Method.Alg())
		}
		return s.secret, nil
	}, jwt.WithIssuer(workloadTokenIssuer), jwt.WithAudience(workloadTokenAudience), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithTimeFunc(s.now))
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("invalid workload token: %w", err)
	}
	if err := validateWorkloadIdentity(*claims); err != nil {
		return nil, err
	}
	if claims.Subject != claims.ExecutionID {
		return nil, errors.New("workload token subject does not match execution identity")
	}
	if claims.IssuedAt == nil || claims.NotBefore == nil || claims.ExpiresAt == nil || strings.TrimSpace(claims.ID) == "" {
		return nil, errors.New("workload token registered identity is incomplete")
	}
	if claims.ExpiresAt.Time.Sub(claims.IssuedAt.Time) > maximumWorkloadLifetime {
		return nil, errors.New("workload token lifetime exceeds maximum")
	}
	return claims, nil
}

func validateWorkloadIdentity(claims WorkloadClaims) error {
	for _, value := range []string{
		claims.RuntimeScope, claims.RuntimeWorkload, claims.RuntimeInstanceID,
		claims.UserID, claims.ProjectID, claims.TaskID, claims.ExecutionID,
	} {
		if strings.TrimSpace(value) == "" {
			return errors.New("workload token identity is incomplete")
		}
	}
	return nil
}
