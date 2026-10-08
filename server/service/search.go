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

var ErrSearchProviderUnavailable = errors.New("search provider unavailable")
var ErrSearchOperationInProgress = errors.New("search operation is already in progress")

func IsSearchProviderUnavailable(err error) bool { return errors.Is(err, ErrSearchProviderUnavailable) }

type SearchRequest struct {
	Query     string
	Limit     int
	TimeRange string
	Domains   []string
}

type SearchResultItem struct {
	Type        string `json:"-"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	Domain      string `json:"domain,omitempty"`
	Snippet     string `json:"snippet,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
	PageAge     string `json:"page_age,omitempty"`
	CitationID  string `json:"citation_id,omitempty"`
}

type SearchResponse struct {
	Provider          string             `json:"provider"`
	Model             string             `json:"model,omitempty"`
	ProviderRequestID string             `json:"provider_request_id,omitempty"`
	Query             string             `json:"query"`
	Results           []SearchResultItem `json:"results"`
	FetchedAt         time.Time          `json:"fetched_at"`
	Usage             map[string]any     `json:"usage,omitempty"`
	ProviderMetadata  map[string]any     `json:"provider_metadata,omitempty"`
}

type SearchProvider interface {
	Search(context.Context, SearchRequest) (*SearchResponse, error)
	Name() string
	Model() string
}

type DoubaoSearchProvider struct {
	client     *http.Client
	baseURL    string
	apiKey     string
	model      string
	maxResults int
	maxKeyword int
}

func NewDoubaoSearchProvider(cfg serverconfig.SearchDoubaoConfig) *DoubaoSearchProvider {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	maxResults := cfg.MaxResults
	if maxResults <= 0 {
		maxResults = 10
	}
	maxKeyword := cfg.MaxKeyword
	if maxKeyword <= 0 {
		maxKeyword = 2
	}
	return &DoubaoSearchProvider{client: &http.Client{Timeout: timeout}, baseURL: strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"), apiKey: strings.TrimSpace(cfg.APIKey), model: strings.TrimSpace(cfg.Model), maxResults: maxResults, maxKeyword: maxKeyword}
}

func (p *DoubaoSearchProvider) Name() string  { return "doubao" }
func (p *DoubaoSearchProvider) Model() string { return p.model }

func (p *DoubaoSearchProvider) Search(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	if p == nil || p.client == nil || p.baseURL == "" || p.apiKey == "" || p.model == "" {
		return nil, ErrSearchProviderUnavailable
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return nil, errors.New("query is required")
	}
	limit := req.Limit
	if limit <= 0 || limit > p.maxResults {
		limit = p.maxResults
	}
	content := []map[string]any{{"type": "input_text", "text": query}}
	searchText := query
	if req.TimeRange != "" {
		searchText += " 时间范围:" + req.TimeRange
	}
	for _, domain := range req.Domains {
		if normalized := strings.TrimSpace(domain); normalized != "" {
			searchText += " site:" + normalized
		}
	}
	content[0]["text"] = searchText
	webSearch := map[string]any{
		"type":        "web_search",
		"max_keyword": p.maxKeyword,
		"limit":       limit,
		"sources":     []string{"toutiao", "douyin", "moji", "search_engine"},
	}
	body := map[string]any{
		"model": p.model,
		"input": []any{map[string]any{"role": "user", "content": content}},
		"tools": []any{webSearch},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/responses", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("ark-beta-web-search", "true")
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSearchProviderUnavailable, err)
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if readErr != nil {
		return nil, fmt.Errorf("%w: read response: %v", ErrSearchProviderUnavailable, readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: HTTP %d", ErrSearchProviderUnavailable, resp.StatusCode)
	}
	var decoded doubaoResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, fmt.Errorf("%w: decode response: %v", ErrSearchProviderUnavailable, err)
	}
	if decoded.Output == nil {
		return nil, fmt.Errorf("%w: response output is missing", ErrSearchProviderUnavailable)
	}
	results := make([]SearchResultItem, 0, limit)
	for _, item := range decoded.Output {
		for _, candidate := range decodeDoubaoSearchResults(item) {
			if candidate.Type != "" && candidate.Type != "web_search_result" && candidate.Type != "web_search_tool_result" && candidate.Type != "web_search_call" && candidate.Type != "url_citation" {
				continue
			}
			if candidate.URL == "" && candidate.Title == "" {
				continue
			}
			candidate.Domain = domainOf(candidate.URL)
			if candidate.CitationID == "" {
				candidate.CitationID = candidate.URL
			}
			results = append(results, candidate)
			if len(results) >= limit {
				break
			}
		}
		if len(results) >= limit {
			break
		}
	}
	metadata := map[string]any{"max_results": limit}
	if req.TimeRange != "" {
		metadata["time_range"] = req.TimeRange
	}
	if len(req.Domains) > 0 {
		metadata["domains"] = append([]string(nil), req.Domains...)
	}
	return &SearchResponse{Provider: p.Name(), Model: p.Model(), ProviderRequestID: decoded.ID, Query: query, Results: results, FetchedAt: time.Now().UTC(), Usage: decoded.Usage, ProviderMetadata: metadata}, nil
}

type doubaoResponse struct {
	ID     string            `json:"id"`
	Output []json.RawMessage `json:"output"`
	Usage  map[string]any    `json:"usage"`
}

// decodeDoubaoSearchResults accepts the Ark Responses shapes used by different
// Web Search revisions: direct result objects, tool-result arrays, and
// url_citation annotations nested under message content.
func decodeDoubaoSearchResults(data json.RawMessage) []SearchResultItem {
	var raw map[string]any
	if json.Unmarshal(data, &raw) != nil {
		return nil
	}
	return decodeDoubaoSearchMap(raw, 0)
}

func decodeDoubaoSearchMap(raw map[string]any, depth int) []SearchResultItem {
	if depth > 4 {
		return nil
	}
	item := SearchResultItem{}
	item.Type, _ = raw["type"].(string)
	item.Title, _ = raw["title"].(string)
	item.URL, _ = raw["url"].(string)
	item.Snippet, _ = raw["snippet"].(string)
	item.PublishedAt, _ = raw["published_at"].(string)
	if item.PublishedAt == "" {
		item.PublishedAt, _ = raw["publish_time"].(string)
	}
	item.PageAge, _ = raw["page_age"].(string)
	if item.PageAge == "" {
		item.PageAge, _ = raw["freshness_info"].(string)
	}
	item.CitationID, _ = raw["citation_id"].(string)
	if item.Snippet == "" {
		item.Snippet, _ = raw["summary"].(string)
	}
	results := make([]SearchResultItem, 0, 4)
	if item.Type == "url_citation" || item.URL != "" || item.Title != "" {
		results = append(results, item)
	}
	for _, key := range []string{"content", "results", "search_results", "citations", "annotations"} {
		values, ok := raw[key].([]any)
		if !ok {
			continue
		}
		for _, value := range values {
			child, ok := value.(map[string]any)
			if !ok {
				continue
			}
			results = append(results, decodeDoubaoSearchMap(child, depth+1)...)
		}
	}
	return results
}

func domainOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return u.Hostname()
}

type SearchService struct {
	config     *serverconfig.SearchConfig
	provider   SearchProvider
	catalog    *BillingCatalogService
	wallet     *BillingWalletService
	costs      *ProviderCostService
	operations repository.SearchOperationRepository
	logger     *zerolog.Logger
}

func NewSearchService(cfg *serverconfig.SearchConfig, provider SearchProvider, catalog *BillingCatalogService, wallet *BillingWalletService, costs *ProviderCostService, operations repository.SearchOperationRepository, logger ...*zerolog.Logger) *SearchService {
	var log *zerolog.Logger
	if len(logger) > 0 {
		log = logger[0]
	}
	return &SearchService{config: cfg, provider: provider, catalog: catalog, wallet: wallet, costs: costs, operations: operations, logger: log}
}

func (s *SearchService) operationLease() time.Duration {
	lease := 5 * time.Minute
	if s != nil && s.config != nil && s.config.Doubao.Timeout > 0 {
		configured := s.config.Doubao.Timeout + time.Minute
		if configured > lease {
			lease = configured
		}
	}
	return lease
}

type SearchOperationRequest struct {
	UserID      string
	ProjectID   string
	TaskID      string
	ExecutionID string
	Request     SearchRequest
}

func (s *SearchService) Search(ctx context.Context, req SearchOperationRequest) (*SearchResponse, error) {
	if s == nil || s.config == nil || !s.config.Enabled || strings.TrimSpace(s.config.Provider) == "" || s.provider == nil {
		return nil, ErrSearchProviderUnavailable
	}
	if strings.TrimSpace(req.UserID) == "" || strings.TrimSpace(req.ProjectID) == "" || strings.TrimSpace(req.TaskID) == "" || strings.TrimSpace(req.ExecutionID) == "" {
		return nil, errors.New("search execution identity is required")
	}
	if strings.TrimSpace(req.Request.Query) == "" {
		return nil, errors.New("query is required")
	}
	if len([]rune(req.Request.Query)) > 2000 {
		return nil, errors.New("query exceeds the 2000-character limit")
	}
	fingerprint := searchFingerprint(req)
	if s.operations != nil {
		existing, findErr := s.operations.FindByFingerprint(ctx, fingerprint)
		if findErr != nil {
			return nil, fmt.Errorf("find search operation: %w", findErr)
		}
		if existing != nil {
			switch existing.Status {
			case model.SearchOperationSucceeded:
				var replay SearchResponse
				if err := json.Unmarshal(existing.ResponseSnapshot, &replay); err != nil {
					return nil, fmt.Errorf("decode search operation response: %w", err)
				}
				return &replay, nil
			case model.SearchOperationRunning:
				if existing.LeaseExpiresAt != nil && existing.LeaseExpiresAt.After(time.Now().UTC()) {
					return nil, ErrSearchOperationInProgress
				}
			}
		}
	}
	if s.catalog == nil || s.wallet == nil {
		return nil, errors.New("search billing is not configured")
	}
	catalogID := s.catalog.CatalogID()
	quote, err := s.catalog.CreateQuote(ctx, QuoteRequest{UserID: req.UserID, CatalogID: catalogID, Operation: "mcp.search_web", Route: "search.web", SKUID: "mcp.search_web", RequestFingerprint: fingerprint, IdempotencyScope: "mcp.search_quote", IdempotencyKey: fingerprint})
	if err != nil {
		return nil, fmt.Errorf("quote search: %w", err)
	}
	if _, err := s.wallet.ChargeStandaloneOperation(ctx, OperationChargeRequest{UserID: req.UserID, QuoteID: quote.ID, CatalogID: quote.CatalogID, SKUID: quote.SKUID, ResourceType: "search", ResourceID: fingerprint, RequestFingerprint: fingerprint, IdempotencyScope: "mcp.search_charge", IdempotencyKey: fingerprint, ActorType: "managed_agent", ActorID: req.ExecutionID, SourceService: "mcp.search_web", TaskID: req.TaskID}); err != nil {
		return nil, fmt.Errorf("charge search: %w", err)
	}
	attemptID := "search-attempt-" + uuid.NewString()
	var operation *model.SearchOperation
	if s.operations != nil {
		candidate := &model.SearchOperation{UserID: req.UserID, ProjectID: req.ProjectID, TaskID: req.TaskID, ExecutionID: req.ExecutionID, RequestFingerprint: fingerprint, AttemptID: attemptID}
		var claimed bool
		operation, claimed, err = s.operations.Claim(ctx, candidate, time.Now().UTC(), s.operationLease())
		if err != nil {
			return nil, fmt.Errorf("claim search operation: %w", err)
		}
		if !claimed {
			if operation != nil && operation.Status == model.SearchOperationSucceeded {
				var replay SearchResponse
				if err := json.Unmarshal(operation.ResponseSnapshot, &replay); err != nil {
					return nil, fmt.Errorf("decode search operation response: %w", err)
				}
				return &replay, nil
			}
			return nil, ErrSearchOperationInProgress
		}
	}
	result, err := s.provider.Search(ctx, req.Request)
	if err != nil {
		s.recordSearchProviderCost(ctx, req, fingerprint, attemptID, "provider_error", nil)
		if operation != nil {
			_ = s.operations.MarkFailed(ctx, operation.ID, attemptID, "provider_unavailable", time.Now().UTC())
		}
		return nil, err
	}
	if result == nil {
		err = fmt.Errorf("%w: provider returned an empty response", ErrSearchProviderUnavailable)
		s.recordSearchProviderCost(ctx, req, fingerprint, attemptID, "provider_error", nil)
		if operation != nil {
			_ = s.operations.MarkFailed(ctx, operation.ID, attemptID, "provider_unavailable", time.Now().UTC())
		}
		return nil, err
	}
	s.recordSearchProviderCost(ctx, req, fingerprint, attemptID, "completed", result)
	if operation != nil {
		payload, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			return nil, fmt.Errorf("marshal search operation response: %w", marshalErr)
		}
		if markErr := s.operations.MarkSucceeded(ctx, operation.ID, attemptID, result.ProviderRequestID, payload, time.Now().UTC()); markErr != nil {
			return nil, fmt.Errorf("persist search operation response: %w", markErr)
		}
	}
	return result, nil
}

func (s *SearchService) recordSearchProviderCost(ctx context.Context, operation SearchOperationRequest, fingerprint, attemptID, outcome string, result *SearchResponse) {
	if s == nil || s.costs == nil || s.provider == nil {
		return
	}
	providerName, modelName := s.provider.Name(), s.provider.Model()
	providerRequestID := ""
	resultCount := 0
	var usage map[string]any
	providerMetadata := make(map[string]any)
	if result != nil {
		if strings.TrimSpace(result.Provider) != "" {
			providerName = result.Provider
		}
		if strings.TrimSpace(result.Model) != "" {
			modelName = result.Model
		}
		providerRequestID = strings.TrimSpace(result.ProviderRequestID)
		resultCount = len(result.Results)
		usage = result.Usage
		for key, value := range result.ProviderMetadata {
			providerMetadata[key] = value
		}
	}
	providerMetadata["_anban_attempt_id"] = attemptID
	providerMetadata["_anban_operation_fingerprint"] = fingerprint
	_, costErr := s.costs.RecordSearchRequest(ctx, RecordSearchRequestCost{
		TaskID: operation.TaskID, Provider: providerName, Model: modelName,
		ProviderRequestID: providerRequestID, AttemptID: attemptID, RequestFingerprint: fingerprint,
		Query: strings.TrimSpace(operation.Request.Query), ResultCount: resultCount, SearchRequestCount: 1,
		Usage: usage, ProviderMetadata: providerMetadata, Outcome: outcome,
	})
	if costErr != nil && s.logger != nil {
		s.logger.Error().Err(costErr).Str("operation", "mcp.search_web").Msg("failed to record search provider cost; preserving operation outcome")
	}
}

func searchFingerprint(req SearchOperationRequest) string {
	domains := make([]string, 0, len(req.Request.Domains))
	for _, domain := range req.Request.Domains {
		if normalized := strings.TrimSpace(domain); normalized != "" {
			domains = append(domains, normalized)
		}
	}
	payload, _ := json.Marshal(struct {
		Version     int      `json:"version"`
		UserID      string   `json:"user_id"`
		ProjectID   string   `json:"project_id"`
		TaskID      string   `json:"task_id"`
		ExecutionID string   `json:"execution_id"`
		Query       string   `json:"query"`
		Limit       int      `json:"limit"`
		TimeRange   string   `json:"time_range"`
		Domains     []string `json:"domains"`
	}{1, strings.TrimSpace(req.UserID), strings.TrimSpace(req.ProjectID), strings.TrimSpace(req.TaskID), strings.TrimSpace(req.ExecutionID), strings.TrimSpace(req.Request.Query), req.Request.Limit, strings.TrimSpace(req.Request.TimeRange), domains})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func (s *BillingCatalogService) CatalogID() string {
	if s == nil {
		return ""
	}
	return s.bundle.Products.CatalogID
}

type RecordSearchRequestCost struct {
	TaskID, Provider, Model, ProviderRequestID, AttemptID, RequestFingerprint, Query string
	ResultCount                                                                      int
	SearchRequestCount                                                               int
	Usage                                                                            map[string]any
	ProviderMetadata                                                                 map[string]any
	Outcome                                                                          string
}

func (s *ProviderCostService) RecordSearchRequest(ctx context.Context, req RecordSearchRequestCost) (*model.BillingProviderCostEvent, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("provider cost service is not configured")
	}
	provider, modelID, requestID := strings.TrimSpace(req.Provider), strings.TrimSpace(req.Model), strings.TrimSpace(req.ProviderRequestID)
	if provider == "" {
		provider = "unknown"
	}
	if modelID == "" {
		modelID = "unknown"
	}
	requestIDSource := "provider"
	if requestID == "" {
		requestID = strings.TrimSpace(req.AttemptID)
		requestIDSource = "search_attempt"
	}
	if requestID == "" {
		return nil, errors.New("search provider cost requires a provider request ID or attempt ID")
	}
	requestCount := req.SearchRequestCount
	if requestCount <= 0 {
		requestCount = 1
	}
	evidence := map[string]any{
		"kind": "search_request", "query": req.Query, "result_count": req.ResultCount,
		"search_request_count": requestCount, "usage": req.Usage,
		"provider_metadata": req.ProviderMetadata, "request_id_source": requestIDSource,
		"outcome": req.Outcome,
	}
	if fingerprint := strings.TrimSpace(req.RequestFingerprint); fingerprint != "" {
		evidence["operation_fingerprint"] = fingerprint
	}
	return s.appendProviderRequestEvent(ctx, providerRequestEvent{TaskID: req.TaskID, Provider: provider, Model: modelID, ProviderRequestID: requestID, CatalogID: s.catalogID, IdempotencyKey: requestID, Source: model.BillingProviderCostSourceProviderResponse, Status: model.BillingProviderCostStatusUnreconciled, Evidence: evidence, Calculation: map[string]any{"version": 1, "reason_code": string(model.BillingExecutionCostReasonMissingProviderUsage)}})
}
