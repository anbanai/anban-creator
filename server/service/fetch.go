package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	serverconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type FetchOperationRequest struct {
	UserID      string
	ProjectID   string
	TaskID      string
	ExecutionID string
	Request     PageFetchRequest
}

type FetchService struct {
	config     *serverconfig.SearchConfig
	provider   PageFetchProvider
	catalog    *BillingCatalogService
	wallet     *BillingWalletService
	costs      *ProviderCostService
	operations repository.SearchOperationRepository
	logger     *zerolog.Logger
}

func NewFetchService(cfg *serverconfig.SearchConfig, provider PageFetchProvider, catalog *BillingCatalogService, wallet *BillingWalletService, costs *ProviderCostService, operations repository.SearchOperationRepository, logger ...*zerolog.Logger) *FetchService {
	var log *zerolog.Logger
	if len(logger) > 0 {
		log = logger[0]
	}
	return &FetchService{config: cfg, provider: provider, catalog: catalog, wallet: wallet, costs: costs, operations: operations, logger: log}
}

func (s *FetchService) Fetch(ctx context.Context, req FetchOperationRequest) (*PageFetchResponse, error) {
	if s == nil || s.config == nil || !s.config.Fetch.Enabled || strings.TrimSpace(s.config.Fetch.Provider) == "" || s.provider == nil {
		return nil, ErrPageFetchUnavailable
	}
	if strings.TrimSpace(req.UserID) == "" || strings.TrimSpace(req.ProjectID) == "" || strings.TrimSpace(req.TaskID) == "" || strings.TrimSpace(req.ExecutionID) == "" {
		return nil, errors.New("fetch execution identity is required")
	}
	if err := ValidatePublicHTTPURL(req.Request.URL); err != nil {
		return nil, err
	}
	fingerprint := fetchFingerprint(req)
	if s.operations != nil {
		existing, err := s.operations.FindByFingerprint(ctx, fingerprint)
		if err != nil {
			return nil, fmt.Errorf("find fetch operation: %w", err)
		}
		if existing != nil {
			if existing.Status == model.SearchOperationSucceeded {
				var replay PageFetchResponse
				if err := json.Unmarshal(existing.ResponseSnapshot, &replay); err != nil {
					return nil, fmt.Errorf("decode fetch operation response: %w", err)
				}
				if err := s.wallet.reconcileReuseOperationHold(ctx, req.UserID, req.TaskID); err != nil {
					return nil, fmt.Errorf("reconcile fetch operation: %w", err)
				}
				return &replay, nil
			}
			if existing.Status == model.SearchOperationRunning && existing.LeaseExpiresAt != nil && existing.LeaseExpiresAt.After(time.Now().UTC()) {
				return nil, ErrSearchOperationInProgress
			}
			if existing.Status == model.SearchOperationRunning {
				if stopped, err := s.wallet.stopStaleReuseOperation(ctx, req.UserID, req.TaskID); err != nil {
					return nil, err
				} else if stopped {
					return nil, ErrSearchOperationInProgress
				}
			}
		}
	}
	if s.catalog == nil || s.wallet == nil {
		return nil, errors.New("fetch billing is not configured")
	}
	reuseTask, billingKey, err := s.wallet.reuseStandaloneOperationIdentity(ctx, req.UserID, req.TaskID, "mcp.fetch_charge", fingerprint)
	if err != nil {
		return nil, err
	}
	quote, err := s.catalog.CreateQuote(ctx, QuoteRequest{UserID: req.UserID, CatalogID: s.catalog.CatalogID(), Operation: "mcp.fetch_web_page", Route: "search.fetch", SKUID: "mcp.fetch_web_page", RequestFingerprint: fingerprint, IdempotencyScope: "mcp.fetch_quote", IdempotencyKey: billingKey})
	if err != nil {
		return nil, fmt.Errorf("quote fetch: %w", err)
	}
	charge, err := s.wallet.ChargeStandaloneOperation(ctx, OperationChargeRequest{UserID: req.UserID, QuoteID: quote.ID, CatalogID: quote.CatalogID, SKUID: quote.SKUID, ResourceType: "fetch", ResourceID: fingerprint, RequestFingerprint: fingerprint, IdempotencyScope: "mcp.fetch_charge", IdempotencyKey: billingKey, ActorType: "managed_agent", ActorID: req.ExecutionID, SourceService: "mcp.fetch_web_page", TaskID: req.TaskID})
	if err != nil {
		return nil, fmt.Errorf("charge fetch: %w", err)
	}
	attemptID := "fetch-attempt-" + uuid.NewString()
	var operation *model.SearchOperation
	if s.operations != nil {
		var claimed bool
		var claimErr error
		operation, claimed, claimErr = s.operations.Claim(ctx, &model.SearchOperation{Operation: "mcp.fetch_web_page", UserID: req.UserID, ProjectID: req.ProjectID, TaskID: req.TaskID, ExecutionID: req.ExecutionID, RequestFingerprint: fingerprint, AttemptID: attemptID}, time.Now().UTC(), s.operationLease())
		if claimErr != nil {
			return nil, fmt.Errorf("claim fetch operation: %w", claimErr)
		}
		if !claimed {
			if operation != nil && operation.Status == model.SearchOperationSucceeded {
				var replay PageFetchResponse
				if err := json.Unmarshal(operation.ResponseSnapshot, &replay); err != nil {
					return nil, fmt.Errorf("decode fetch operation response: %w", err)
				}
				if err := s.wallet.reconcileReuseOperationHold(ctx, req.UserID, req.TaskID); err != nil {
					return nil, fmt.Errorf("reconcile fetch operation: %w", err)
				}
				return &replay, nil
			}
			return nil, ErrSearchOperationInProgress
		}
	}
	result, err := s.provider.Fetch(ctx, req.Request)
	if err != nil {
		s.recordFetchCost(ctx, req, fingerprint, attemptID, "provider_error", nil)
		if failureErr := s.wallet.failReuseStandaloneOperation(ctx, reuseTask, charge, err); failureErr != nil {
			return nil, errors.Join(err, failureErr)
		}
		if operation != nil {
			_ = s.operations.MarkFailed(ctx, operation.ID, attemptID, "provider_unavailable", time.Now().UTC())
		}
		return nil, err
	}
	if result == nil || strings.TrimSpace(result.ContentMarkdown) == "" {
		err = fmt.Errorf("%w: provider returned no markdown content", ErrPageFetchUnavailable)
		if failureErr := s.wallet.failReuseStandaloneOperation(ctx, reuseTask, charge, err); failureErr != nil {
			return nil, errors.Join(err, failureErr)
		}
		s.recordFetchCost(ctx, req, fingerprint, attemptID, "provider_error", nil)
		if operation != nil {
			_ = s.operations.MarkFailed(ctx, operation.ID, attemptID, "provider_unavailable", time.Now().UTC())
		}
		return nil, err
	}
	s.recordFetchCost(ctx, req, fingerprint, attemptID, "completed", result)
	if operation != nil {
		payload, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			return nil, fmt.Errorf("marshal fetch operation response: %w", marshalErr)
		}
		if err := s.operations.MarkSucceeded(ctx, operation.ID, attemptID, result.ProviderRequestID, payload, time.Now().UTC()); err != nil {
			return nil, fmt.Errorf("persist fetch operation response: %w", err)
		}
		if err := s.wallet.reconcileReuseOperationHold(ctx, req.UserID, req.TaskID); err != nil {
			return nil, fmt.Errorf("reconcile fetch operation: %w", err)
		}
	}
	return result, nil
}

func (s *FetchService) operationLease() time.Duration {
	lease := 5 * time.Minute
	if s != nil && s.config != nil && s.config.Providers.DeepSeek.Timeout > 0 && s.config.Providers.DeepSeek.Timeout+time.Minute > lease {
		lease = s.config.Providers.DeepSeek.Timeout + time.Minute
	}
	return lease
}

func (s *FetchService) recordFetchCost(ctx context.Context, req FetchOperationRequest, fingerprint, attemptID, outcome string, result *PageFetchResponse) {
	if s == nil || s.costs == nil {
		return
	}
	provider := "unknown"
	providerRequestID := ""
	resultCount := 0
	metadata := map[string]any{"fetch_type": "web_page", "_anban_attempt_id": attemptID, "_anban_operation_fingerprint": fingerprint}
	if named, ok := s.provider.(interface{ Name() string }); ok {
		provider = strings.TrimSpace(named.Name())
	}
	if result != nil {
		if result.Provider != "" {
			provider = result.Provider
		}
		providerRequestID = result.ProviderRequestID
		resultCount = 1
		for key, value := range result.ProviderMetadata {
			metadata[key] = value
		}
	}
	redactedURL := redactFetchURL(req.Request.URL)
	if _, err := s.costs.RecordSearchRequest(ctx, RecordSearchRequestCost{TaskID: req.TaskID, Provider: provider, ProviderRequestID: providerRequestID, AttemptID: attemptID, RequestFingerprint: fingerprint, URL: redactedURL, Query: redactedURL, ResultCount: resultCount, SearchRequestCount: 1, ProviderMetadata: metadata, Outcome: outcome, Operation: "mcp.fetch_web_page"}); err != nil && s.logger != nil {
		s.logger.Error().Err(err).Str("operation", "mcp.fetch_web_page").Msg("failed to record fetch provider cost")
	}
}

func redactFetchURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "redacted-url"
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func fetchFingerprint(req FetchOperationRequest) string {
	payload, _ := json.Marshal(struct {
		Version                                                int `json:"version"`
		Operation, UserID, ProjectID, TaskID, ExecutionID, URL string
	}{2, "mcp.fetch_web_page", strings.TrimSpace(req.UserID), strings.TrimSpace(req.ProjectID), strings.TrimSpace(req.TaskID), strings.TrimSpace(req.ExecutionID), strings.TrimSpace(req.Request.URL)})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
