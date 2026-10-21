package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	serverconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

var (
	ErrInvalidImageSearchRequest    = errors.New("invalid image search request")
	ErrImageSearchAccountingPending = errors.New("image search operation is saved; retry the same request to complete accounting")
)

const imageSearchProviderErrorCostPending = model.SearchOperationErrorProviderErrorCostPending

type ImageSearchRequest struct {
	Query string
	Limit int
}

type ImageSearchResultItem struct {
	Title        string    `json:"title,omitempty"`
	ImageURL     string    `json:"image_url"`
	ThumbnailURL string    `json:"thumbnail_url,omitempty"`
	SourceURL    string    `json:"source_url,omitempty"`
	Domain       string    `json:"domain,omitempty"`
	Description  string    `json:"description,omitempty"`
	PublishedAt  string    `json:"published_at,omitempty"`
	Width        int       `json:"width,omitempty"`
	Height       int       `json:"height,omitempty"`
	Rank         int       `json:"rank,omitempty"`
	FetchedAt    time.Time `json:"fetched_at"`
}

type ImageSearchResponse struct {
	Provider           string                  `json:"provider"`
	ProviderRequestID  string                  `json:"provider_request_id,omitempty"`
	Query              string                  `json:"query,omitempty"`
	TotalDocumentCount int                     `json:"total_document_count"`
	Results            []ImageSearchResultItem `json:"results"`
	FetchedAt          time.Time               `json:"fetched_at"`
	Usage              map[string]any          `json:"usage,omitempty"`
	ProviderMetadata   map[string]any          `json:"provider_metadata,omitempty"`
}

type ImageSearchProvider interface {
	SearchImages(context.Context, ImageSearchRequest) (*ImageSearchResponse, error)
}

type DoubaoGlobalImageSearchProvider struct {
	client   *http.Client
	endpoint string
	apiKey   string
	max      int
}

func NewDoubaoGlobalImageSearchProvider(cfg serverconfig.SearchDoubaoConfig) *DoubaoGlobalImageSearchProvider {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	max := cfg.MaxResults
	if max <= 0 {
		max = 5
	}
	if max > 5 {
		max = 5
	}
	return &DoubaoGlobalImageSearchProvider{
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		endpoint: strings.TrimSpace(cfg.GlobalEndpoint),
		apiKey:   strings.TrimSpace(cfg.APIKey),
		max:      max,
	}
}

func (*DoubaoGlobalImageSearchProvider) Name() string { return "doubao_global" }

func (p *DoubaoGlobalImageSearchProvider) ValidateRequest(req ImageSearchRequest) error {
	if p == nil {
		return ErrSearchProviderUnavailable
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return fmt.Errorf("%w: query is required", ErrInvalidImageSearchRequest)
	}
	if len([]rune(query)) > 100 {
		return fmt.Errorf("%w: query exceeds the 100-character limit", ErrInvalidImageSearchRequest)
	}
	if req.Limit < 0 || req.Limit > p.max {
		return fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidImageSearchRequest, p.max)
	}
	return nil
}

func (p *DoubaoGlobalImageSearchProvider) SearchImages(ctx context.Context, req ImageSearchRequest) (*ImageSearchResponse, error) {
	if p == nil || p.client == nil || p.endpoint == "" || p.apiKey == "" {
		return nil, ErrSearchProviderUnavailable
	}
	if err := p.ValidateRequest(req); err != nil {
		return nil, err
	}
	query := strings.TrimSpace(req.Query)
	limit := req.Limit
	if limit <= 0 {
		limit = p.max
	}
	searchType := "image"
	body := map[string]any{"Query": query, "SearchType": searchType, "DocCount": limit}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("%w: request encoding failed", ErrSearchProviderUnavailable)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("%w: request creation failed", ErrSearchProviderUnavailable)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%w: upstream request failed", ErrSearchProviderUnavailable)
	}
	defer resp.Body.Close()
	const maxResponseBytes = 4 << 20
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if readErr != nil || len(data) > maxResponseBytes {
		return nil, fmt.Errorf("%w: invalid response body", ErrSearchProviderUnavailable)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		providerErr := fmt.Errorf("%w: upstream HTTP %d", ErrSearchProviderUnavailable, resp.StatusCode)
		if resp.StatusCode >= http.StatusBadRequest && resp.StatusCode < http.StatusInternalServerError {
			return nil, &providerRequestRejectedError{cause: providerErr}
		}
		return nil, providerErr
	}
	var envelope doubaoSearchEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil || !hasJSONValue(envelope.Result) || isJSONNull(envelope.Result) {
		return nil, fmt.Errorf("%w: invalid response", ErrSearchProviderUnavailable)
	}
	if hasJSONValue(envelope.ResponseMetadata.Error) && !isJSONNull(envelope.ResponseMetadata.Error) {
		return nil, fmt.Errorf("%w: provider request rejected", ErrSearchProviderUnavailable)
	}
	result, err := normalizeDoubaoGlobalImageResponse(envelope, query, searchType, limit)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid image search result", ErrSearchProviderUnavailable)
	}
	return &result, nil
}

func normalizeDoubaoGlobalImageResponse(env doubaoSearchEnvelope, query, searchType string, limit int) (ImageSearchResponse, error) {
	var raw struct {
		ErrorCode     *int   `json:"ErrorCode"`
		ErrorMsg      string `json:"ErrorMsg"`
		TotalDocCount *int   `json:"TotalDocCount"`
		Documents     *[]struct {
			Rank    int    `json:"Rank"`
			Title   string `json:"Title"`
			URL     string `json:"Url"`
			Snippet []struct {
				Type  string `json:"Type"`
				Text  string `json:"Text"`
				Image struct {
					ImageURL     string `json:"ImageUrl"`
					Alt          string `json:"Alt"`
					Width        int    `json:"Width"`
					Height       int    `json:"Height"`
					ThumbnailURL struct {
						Medium string `json:"Medium"`
						Small  string `json:"Small"`
						Tiny   string `json:"Tiny"`
					} `json:"ThumbnailUrl"`
				} `json:"Image"`
			} `json:"Snippet"`
			DocumentInfo struct {
				PublishTime string `json:"PublishTime"`
			} `json:"DocumentInfo"`
			HostInfo struct {
				Hostname string `json:"Hostname"`
			} `json:"HostInfo"`
		} `json:"Documents"`
	}
	if err := json.Unmarshal(env.Result, &raw); err != nil {
		return ImageSearchResponse{}, err
	}
	if raw.ErrorCode == nil || raw.TotalDocCount == nil || raw.Documents == nil || *raw.ErrorCode != 0 || *raw.TotalDocCount < 0 {
		return ImageSearchResponse{}, errors.New("global image search returned an error")
	}
	items := make([]ImageSearchResultItem, 0, len(*raw.Documents))
	fetchedAt := time.Now().UTC()
	for _, doc := range *raw.Documents {
		sourceURL := strings.TrimSpace(doc.URL)
		if !validImageResultURL(sourceURL) {
			sourceURL = ""
		}
		domain := domainOf(sourceURL)
		if domain == "" {
			domain = strings.TrimSpace(doc.HostInfo.Hostname)
		}
		var descriptions []string
		for _, snippet := range doc.Snippet {
			if snippet.Type == "text" && strings.TrimSpace(snippet.Text) != "" {
				descriptions = appendUniqueString(descriptions, strings.TrimSpace(snippet.Text))
			}
		}
		for _, snippet := range doc.Snippet {
			if snippet.Type != "image" {
				continue
			}
			imageURL := strings.TrimSpace(snippet.Image.ImageURL)
			if !validImageResultURL(imageURL) {
				continue
			}
			itemDescriptions := append([]string(nil), descriptions...)
			if alt := strings.TrimSpace(snippet.Image.Alt); alt != "" {
				itemDescriptions = appendUniqueString(itemDescriptions, alt)
			}
			thumbnailURL := firstValidImageURL(snippet.Image.ThumbnailURL.Medium, snippet.Image.ThumbnailURL.Small, snippet.Image.ThumbnailURL.Tiny)
			items = append(items, ImageSearchResultItem{
				Title: doc.Title, ImageURL: imageURL, ThumbnailURL: thumbnailURL, SourceURL: sourceURL,
				Domain: domain, Description: strings.Join(itemDescriptions, "\n"), PublishedAt: doc.DocumentInfo.PublishTime,
				Width: snippet.Image.Width, Height: snippet.Image.Height, Rank: doc.Rank, FetchedAt: fetchedAt,
			})
			if len(items) == limit {
				break
			}
		}
		if len(items) == limit {
			break
		}
	}
	if *raw.TotalDocCount > 0 && len(items) == 0 {
		return ImageSearchResponse{}, errors.New("global image search returned no usable image URLs")
	}
	return ImageSearchResponse{
		Provider: "doubao_global", ProviderRequestID: env.ResponseMetadata.RequestID, Query: query,
		TotalDocumentCount: *raw.TotalDocCount, Results: items, FetchedAt: fetchedAt,
		ProviderMetadata: map[string]any{"edition": "global", "search_type": searchType},
	}, nil
}

func firstValidImageURL(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if validImageResultURL(value) {
			return value
		}
	}
	return ""
}

func validImageResultURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil
}

type ImageSearchOperationRequest struct {
	UserID      string
	ProjectID   string
	TaskID      string
	ExecutionID string
	Request     ImageSearchRequest
}

type ImageSearchService struct {
	config     *serverconfig.SearchConfig
	provider   ImageSearchProvider
	catalog    *BillingCatalogService
	wallet     *BillingWalletService
	costs      *ProviderCostService
	operations repository.SearchOperationRepository
	logger     *zerolog.Logger
}

func NewImageSearchService(cfg *serverconfig.SearchConfig, provider ImageSearchProvider, catalog *BillingCatalogService, wallet *BillingWalletService, costs *ProviderCostService, operations repository.SearchOperationRepository, logger ...*zerolog.Logger) *ImageSearchService {
	var log *zerolog.Logger
	if len(logger) > 0 {
		log = logger[0]
	}
	return &ImageSearchService{config: cfg, provider: provider, catalog: catalog, wallet: wallet, costs: costs, operations: operations, logger: log}
}

func (s *ImageSearchService) operationLease() time.Duration {
	lease := 5 * time.Minute
	if s != nil && s.config != nil && s.config.Providers.Doubao.Timeout+time.Minute > lease {
		lease = s.config.Providers.Doubao.Timeout + time.Minute
	}
	return lease
}

func (s *ImageSearchService) SearchImages(ctx context.Context, req ImageSearchOperationRequest) (*ImageSearchResponse, error) {
	if s == nil || s.config == nil || !s.config.Images.Enabled || !strings.EqualFold(strings.TrimSpace(s.config.Images.Provider), "doubao") || s.provider == nil {
		return nil, ErrSearchProviderUnavailable
	}
	if strings.TrimSpace(req.UserID) == "" || strings.TrimSpace(req.ProjectID) == "" || strings.TrimSpace(req.TaskID) == "" || strings.TrimSpace(req.ExecutionID) == "" {
		return nil, errors.New("image search execution identity is required")
	}
	validator, ok := s.provider.(interface {
		ValidateRequest(ImageSearchRequest) error
	})
	if ok {
		if err := validator.ValidateRequest(req.Request); err != nil {
			return nil, err
		}
	} else if err := validateImageSearchRequest(req.Request, 5); err != nil {
		return nil, err
	}
	if s.catalog == nil || s.wallet == nil || s.costs == nil || s.operations == nil {
		return nil, errors.New("image search billing is not configured")
	}
	fingerprint := imageSearchFingerprint(req)
	existing, err := s.operations.FindByFingerprint(ctx, fingerprint)
	if err != nil {
		return nil, fmt.Errorf("find image search operation: %w", err)
	}
	if existing != nil {
		switch existing.Status {
		case model.SearchOperationSucceeded:
			var replay ImageSearchResponse
			if err := json.Unmarshal(existing.ResponseSnapshot, &replay); err != nil {
				return nil, fmt.Errorf("decode image search operation response: %w", err)
			}
			if err := s.wallet.reconcileReuseOperationHold(ctx, req.UserID, req.TaskID); err != nil {
				return nil, fmt.Errorf("reconcile image search operation: %w", err)
			}
			return &replay, nil
		case model.SearchOperationCostPending:
			if existing.ErrorCode == imageSearchProviderErrorCostPending {
				return nil, s.recoverProviderErrorCost(ctx, req, fingerprint, existing)
			}
			var replay ImageSearchResponse
			if err := json.Unmarshal(existing.ResponseSnapshot, &replay); err != nil {
				return nil, fmt.Errorf("decode image search operation response: %w", err)
			}
			if err := s.recordImageSearchProviderCost(ctx, req, fingerprint, existing.AttemptID, "completed", &replay); err != nil {
				return nil, ErrImageSearchAccountingPending
			}
			if err := s.operations.MarkSucceeded(ctx, existing.ID, existing.AttemptID, replay.ProviderRequestID, existing.ResponseSnapshot, time.Now().UTC()); err != nil {
				return nil, ErrImageSearchAccountingPending
			}
			if err := s.wallet.reconcileReuseOperationHold(ctx, req.UserID, req.TaskID); err != nil {
				return nil, fmt.Errorf("reconcile image search operation: %w", err)
			}
			return &replay, nil
		case model.SearchOperationRunning:
			if existing.LeaseExpiresAt != nil && existing.LeaseExpiresAt.After(time.Now().UTC()) {
				return nil, ErrSearchOperationInProgress
			}
			if stopped, err := s.wallet.stopStaleReuseOperation(ctx, req.UserID, req.TaskID); err != nil {
				return nil, err
			} else if stopped {
				return nil, ErrSearchOperationInProgress
			}
		}
	}
	reuseTask, billingKey, err := s.wallet.reuseStandaloneOperationIdentity(ctx, req.UserID, req.TaskID, "mcp.search_images_charge", fingerprint)
	if err != nil {
		return nil, err
	}
	quote, err := s.catalog.CreateQuote(ctx, QuoteRequest{
		UserID: req.UserID, CatalogID: s.catalog.CatalogID(), Operation: "mcp.search_images", Route: "search.images", SKUID: "mcp.search_images",
		RequestFingerprint: fingerprint, IdempotencyScope: "mcp.search_images_quote", IdempotencyKey: billingKey,
	})
	if err != nil {
		return nil, fmt.Errorf("quote image search: %w", err)
	}
	charge, err := s.wallet.ChargeStandaloneOperation(ctx, OperationChargeRequest{
		UserID: req.UserID, QuoteID: quote.ID, CatalogID: quote.CatalogID, SKUID: quote.SKUID,
		ResourceType: "image_search", ResourceID: fingerprint, RequestFingerprint: fingerprint,
		IdempotencyScope: "mcp.search_images_charge", IdempotencyKey: billingKey,
		ActorType: "managed_agent", ActorID: req.ExecutionID, SourceService: "mcp.search_images", TaskID: req.TaskID,
	})
	if err != nil {
		return nil, fmt.Errorf("charge image search: %w", err)
	}
	attemptID := "image-search-attempt-" + uuid.NewString()
	var operation *model.SearchOperation
	var claimed bool
	operation, claimed, err = s.operations.Claim(ctx, &model.SearchOperation{
		Operation: "mcp.search_images", UserID: req.UserID, ProjectID: req.ProjectID, TaskID: req.TaskID,
		ExecutionID: req.ExecutionID, RequestFingerprint: fingerprint, AttemptID: attemptID,
	}, time.Now().UTC(), s.operationLease())
	if err != nil {
		return nil, fmt.Errorf("claim image search operation: %w", err)
	}
	if !claimed {
		if operation != nil && operation.Status == model.SearchOperationCostPending && operation.ErrorCode == imageSearchProviderErrorCostPending {
			return nil, s.recoverProviderErrorCost(ctx, req, fingerprint, operation)
		}
		if operation != nil && (operation.Status == model.SearchOperationSucceeded || operation.Status == model.SearchOperationCostPending) {
			var replay ImageSearchResponse
			if err := json.Unmarshal(operation.ResponseSnapshot, &replay); err != nil {
				return nil, fmt.Errorf("decode image search operation response: %w", err)
			}
			if err := s.recordImageSearchProviderCost(ctx, req, fingerprint, operation.AttemptID, "completed", &replay); err != nil {
				return nil, ErrImageSearchAccountingPending
			}
			if operation.Status == model.SearchOperationCostPending {
				if err := s.operations.MarkSucceeded(ctx, operation.ID, operation.AttemptID, replay.ProviderRequestID, operation.ResponseSnapshot, time.Now().UTC()); err != nil {
					return nil, ErrImageSearchAccountingPending
				}
			}
			if err := s.wallet.reconcileReuseOperationHold(ctx, req.UserID, req.TaskID); err != nil {
				return nil, fmt.Errorf("reconcile image search operation: %w", err)
			}
			return &replay, nil
		}
		return nil, ErrSearchOperationInProgress
	}
	result, err := s.provider.SearchImages(ctx, req.Request)
	if err == nil && (result == nil || result.Results == nil) {
		err = fmt.Errorf("%w: provider returned an empty response", ErrSearchProviderUnavailable)
	}
	if err != nil {
		if err := s.persistProviderFailure(ctx, req, fingerprint, attemptID, operation, charge, reuseTask, err); err != nil {
			return nil, err
		}
		return nil, ErrSearchProviderUnavailable
	}
	if operation != nil {
		payload, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("marshal image search operation response: %w", err)
		}
		if err := s.operations.MarkCostPending(ctx, operation.ID, attemptID, result.ProviderRequestID, payload, time.Now().UTC()); err != nil {
			if s.logger != nil {
				s.logger.Error().Err(err).Str("operation", "mcp.search_images").Msg("failed to persist image search response before provider cost recording")
			}
			return nil, ErrImageSearchAccountingPending
		}
	}
	if err := s.recordImageSearchProviderCost(ctx, req, fingerprint, attemptID, "completed", result); err != nil {
		return nil, ErrImageSearchAccountingPending
	}
	if operation != nil {
		payload, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("marshal image search operation response: %w", err)
		}
		if err := s.operations.MarkSucceeded(ctx, operation.ID, attemptID, result.ProviderRequestID, payload, time.Now().UTC()); err != nil {
			if s.logger != nil {
				s.logger.Error().Err(err).Str("operation", "mcp.search_images").Msg("failed to complete image search operation after provider cost recording")
			}
			return nil, ErrImageSearchAccountingPending
		}
		if err := s.wallet.reconcileReuseOperationHold(ctx, req.UserID, req.TaskID); err != nil {
			return nil, fmt.Errorf("reconcile image search operation: %w", err)
		}
	}
	return result, nil
}

type imageSearchProviderFailureSnapshot struct {
	BillingChargeID string `json:"billing_charge_id"`
	Rejected        bool   `json:"rejected"`
}

func (s *ImageSearchService) persistProviderFailure(ctx context.Context, req ImageSearchOperationRequest, fingerprint, attemptID string, operation *model.SearchOperation, charge *model.BillingCharge, reuseTask *model.Task, providerErr error) error {
	if operation == nil || charge == nil {
		return ErrImageSearchAccountingPending
	}
	var rejected *providerRequestRejectedError
	snapshot, err := json.Marshal(imageSearchProviderFailureSnapshot{BillingChargeID: charge.ID, Rejected: errors.As(providerErr, &rejected)})
	if err != nil {
		return ErrImageSearchAccountingPending
	}
	if err := s.operations.MarkProviderErrorCostPending(ctx, operation.ID, attemptID, snapshot, time.Now().UTC()); err != nil {
		if s.logger != nil {
			s.logger.Error().Err(err).Str("operation", "mcp.search_images").Msg("failed to persist image search provider failure before cost recording")
		}
		return ErrImageSearchAccountingPending
	}
	if err := s.recordImageSearchProviderCost(ctx, req, fingerprint, attemptID, "provider_error", nil); err != nil {
		return ErrImageSearchAccountingPending
	}
	if err := s.wallet.failReuseStandaloneOperation(ctx, reuseTask, charge, providerErr); err != nil {
		return ErrImageSearchAccountingPending
	}
	if err := s.operations.MarkFailed(ctx, operation.ID, attemptID, "provider_unavailable", time.Now().UTC()); err != nil {
		return ErrImageSearchAccountingPending
	}
	return nil
}

func (s *ImageSearchService) recoverProviderErrorCost(ctx context.Context, req ImageSearchOperationRequest, fingerprint string, operation *model.SearchOperation) error {
	var snapshot imageSearchProviderFailureSnapshot
	if operation == nil || json.Unmarshal(operation.ResponseSnapshot, &snapshot) != nil || strings.TrimSpace(snapshot.BillingChargeID) == "" {
		return ErrImageSearchAccountingPending
	}
	if err := s.recordImageSearchProviderCost(ctx, req, fingerprint, operation.AttemptID, "provider_error", nil); err != nil {
		return ErrImageSearchAccountingPending
	}
	charge, err := s.wallet.repo.Billing().FindChargeByID(ctx, snapshot.BillingChargeID)
	if err != nil {
		return ErrImageSearchAccountingPending
	}
	reuseTask, _, err := s.wallet.reuseStandaloneOperationIdentity(ctx, req.UserID, req.TaskID, "mcp.search_images_charge", fingerprint)
	if err != nil {
		return ErrImageSearchAccountingPending
	}
	providerErr := error(ErrSearchProviderUnavailable)
	if snapshot.Rejected {
		providerErr = &providerRequestRejectedError{cause: ErrSearchProviderUnavailable}
	}
	if err := s.wallet.failReuseStandaloneOperation(ctx, reuseTask, charge, providerErr); err != nil {
		return ErrImageSearchAccountingPending
	}
	if err := s.operations.MarkFailed(ctx, operation.ID, operation.AttemptID, "provider_unavailable", time.Now().UTC()); err != nil {
		return ErrImageSearchAccountingPending
	}
	return ErrSearchProviderUnavailable
}

func validateImageSearchRequest(req ImageSearchRequest, max int) error {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return fmt.Errorf("%w: query is required", ErrInvalidImageSearchRequest)
	}
	if len([]rune(query)) > 100 {
		return fmt.Errorf("%w: query exceeds the 100-character limit", ErrInvalidImageSearchRequest)
	}
	if req.Limit < 0 || req.Limit > max {
		return fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidImageSearchRequest, max)
	}
	return nil
}

func (s *ImageSearchService) recordImageSearchProviderCost(ctx context.Context, req ImageSearchOperationRequest, fingerprint, attemptID, outcome string, result *ImageSearchResponse) error {
	if s == nil || s.costs == nil || s.provider == nil {
		return errors.New("image search provider cost recorder is not configured")
	}
	provider := "unknown"
	if named, ok := s.provider.(interface{ Name() string }); ok && strings.TrimSpace(named.Name()) != "" {
		provider = strings.TrimSpace(named.Name())
	}
	providerRequestID := ""
	resultCount := 0
	var usage map[string]any
	searchType := "image"
	input := strings.TrimSpace(req.Request.Query)
	metadata := map[string]any{
		"search_type": searchType, "limit": req.Request.Limit,
		"input_length": len([]rune(input)), "_anban_attempt_id": attemptID,
		"_anban_operation_fingerprint": fingerprint,
	}
	if result != nil {
		if strings.TrimSpace(result.Provider) != "" {
			provider = strings.TrimSpace(result.Provider)
		}
		providerRequestID = strings.TrimSpace(result.ProviderRequestID)
		resultCount = len(result.Results)
		usage = result.Usage
		for key, value := range result.ProviderMetadata {
			metadata[key] = value
		}
	}
	_, err := s.costs.RecordSearchRequest(ctx, RecordSearchRequestCost{
		TaskID: req.TaskID, Provider: provider, ProviderRequestID: providerRequestID,
		AttemptID: attemptID, RequestFingerprint: fingerprint, Query: redactSearchQuery(input),
		ResultCount: resultCount, SearchRequestCount: 1, Usage: usage, ProviderMetadata: metadata,
		Outcome: outcome, Operation: "mcp.search_images",
	})
	if err != nil && s.logger != nil {
		s.logger.Error().Err(err).Str("operation", "mcp.search_images").Msg("failed to record image search provider cost")
	}
	return err
}

func imageSearchFingerprint(req ImageSearchOperationRequest) string {
	payload, _ := json.Marshal(struct {
		Version     int    `json:"version"`
		Operation   string `json:"operation"`
		UserID      string `json:"user_id"`
		ProjectID   string `json:"project_id"`
		TaskID      string `json:"task_id"`
		ExecutionID string `json:"execution_id"`
		Query       string `json:"query"`
		Limit       int    `json:"limit"`
	}{3, "mcp.search_images", strings.TrimSpace(req.UserID), strings.TrimSpace(req.ProjectID), strings.TrimSpace(req.TaskID), strings.TrimSpace(req.ExecutionID), strings.TrimSpace(req.Request.Query), req.Request.Limit})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}
