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
	Query          string
	Limit          int
	TimeRange      string
	Domains        []string
	BlockHosts     []string
	NeedContent    *bool
	NeedURL        *bool
	AuthInfoLevel  int
	ContentFormats string
	Industry       string
	QueryRewrite   *bool
}

type SearchResultItem struct {
	Type        string  `json:"type,omitempty"`
	Title       string  `json:"title"`
	URL         string  `json:"url"`
	Domain      string  `json:"domain,omitempty"`
	Snippet     string  `json:"snippet,omitempty"`
	Content     string  `json:"content,omitempty"`
	PublishedAt string  `json:"published_at,omitempty"`
	PageAge     string  `json:"page_age,omitempty"`
	CitationID  string  `json:"citation_id,omitempty"`
	Authority   string  `json:"authority,omitempty"`
	Rank        int     `json:"rank,omitempty"`
	RankScore   float64 `json:"rank_score,omitempty"`
}

type SearchResponse struct {
	Provider          string             `json:"provider"`
	ProviderRequestID string             `json:"provider_request_id,omitempty"`
	Query             string             `json:"query"`
	TotalCount        int                `json:"total_count,omitempty"`
	Results           []SearchResultItem `json:"results"`
	FetchedAt         time.Time          `json:"fetched_at"`
	Usage             map[string]any     `json:"usage,omitempty"`
	ProviderMetadata  map[string]any     `json:"provider_metadata,omitempty"`
}

type SearchProvider interface {
	Search(context.Context, SearchRequest) (*SearchResponse, error)
	Name() string
	ValidateRequest(SearchRequest) error
}

type DoubaoSearchProvider struct {
	client        *http.Client
	endpoint      string
	apiKey        string
	edition       string
	maxWebResults int
}

func NewDoubaoCustomSearchProvider(cfg serverconfig.SearchDoubaoConfig) *DoubaoSearchProvider {
	return newDoubaoSearchProvider(cfg, "custom")
}
func NewDoubaoGlobalSearchProvider(cfg serverconfig.SearchDoubaoConfig) *DoubaoSearchProvider {
	return newDoubaoSearchProvider(cfg, "global")
}

func newDoubaoSearchProvider(cfg serverconfig.SearchDoubaoConfig, edition string) *DoubaoSearchProvider {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	max := cfg.MaxWebResults
	maxAllowed := 50
	if edition == "global" {
		maxAllowed = 20
	}
	if max <= 0 || max > maxAllowed {
		max = 10
	}
	return &DoubaoSearchProvider{client: &http.Client{Timeout: timeout}, endpoint: strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/"), apiKey: strings.TrimSpace(cfg.APIKey), edition: edition, maxWebResults: max}
}
func (p *DoubaoSearchProvider) Name() string {
	if p == nil {
		return ""
	}
	return "doubao_" + p.edition
}
func (p *DoubaoSearchProvider) ValidateRequest(req SearchRequest) error {
	if strings.TrimSpace(req.Query) == "" {
		return errors.New("query is required")
	}
	if len([]rune(strings.TrimSpace(req.Query))) > 100 {
		return errors.New("query exceeds the 100-character limit")
	}
	if req.Limit < 0 || req.Limit > p.maxWebResults {
		return fmt.Errorf("limit must be between 1 and %d", p.maxWebResults)
	}
	if p.edition == "global" {
		if req.TimeRange != "" || len(req.Domains) > 0 || len(req.BlockHosts) > 0 || req.NeedContent != nil || req.NeedURL != nil || req.AuthInfoLevel != 0 || req.ContentFormats != "" || req.Industry != "" || req.QueryRewrite != nil {
			return errors.New("the selected Global search edition does not support Custom filters")
		}
	}
	if p.edition == "custom" {
		if len(req.Domains) > 20 {
			return errors.New("domains supports at most 20 entries")
		}
		if len(req.BlockHosts) > 5 {
			return errors.New("block_hosts supports at most 5 entries")
		}
		if req.TimeRange != "" && !validCustomTimeRange(req.TimeRange) {
			return errors.New("invalid Custom time_range")
		}
		if req.AuthInfoLevel < 0 || req.AuthInfoLevel > 1 {
			return errors.New("auth_info_level must be 0 or 1")
		}
		if req.ContentFormats != "" && req.ContentFormats != "text" && req.ContentFormats != "markdown" {
			return errors.New("content_formats must be text or markdown")
		}
		if req.Industry != "" && !map[string]bool{"finance": true, "game": true, "health": true, "gov": true}[req.Industry] {
			return errors.New("invalid industry")
		}
	} else if p.edition != "global" {
		return errors.New("unsupported Doubao search edition")
	}
	return nil
}
func validCustomTimeRange(v string) bool {
	for _, item := range []string{"OneDay", "OneWeek", "OneMonth", "OneYear"} {
		if v == item {
			return true
		}
	}
	if len(v) != 21 || v[10:12] != ".." {
		return false
	}
	_, err1 := time.Parse("2006-01-02", v[:10])
	_, err2 := time.Parse("2006-01-02", v[12:])
	return err1 == nil && err2 == nil
}
func (p *DoubaoSearchProvider) Search(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	if p == nil || p.client == nil || p.endpoint == "" || p.apiKey == "" {
		return nil, ErrSearchProviderUnavailable
	}
	if err := p.ValidateRequest(req); err != nil {
		return nil, err
	}
	query := strings.TrimSpace(req.Query)
	limit := req.Limit
	if limit <= 0 {
		limit = p.maxWebResults
	}
	body := map[string]any{"Query": query, "SearchType": "web"}
	if p.edition == "custom" {
		body["Count"] = limit
		filter := map[string]any{}
		if req.NeedContent != nil {
			filter["NeedContent"] = *req.NeedContent
		}
		if req.NeedURL != nil {
			filter["NeedUrl"] = *req.NeedURL
		}
		if len(req.Domains) > 0 {
			filter["Sites"] = strings.Join(normalizeStrings(req.Domains), "|")
		}
		if len(req.BlockHosts) > 0 {
			filter["BlockHosts"] = strings.Join(normalizeStrings(req.BlockHosts), "|")
		}
		if req.AuthInfoLevel != 0 {
			filter["AuthInfoLevel"] = req.AuthInfoLevel
		}
		if req.TimeRange != "" {
			filter["TimeRange"] = req.TimeRange
		}
		if req.ContentFormats != "" {
			filter["ContentFormats"] = req.ContentFormats
		}
		if req.Industry != "" {
			filter["Industry"] = req.Industry
		}
		if len(filter) > 0 {
			body["Filter"] = filter
		}
		if req.QueryRewrite != nil {
			body["QueryControl"] = map[string]any{"QueryRewrite": *req.QueryRewrite}
		}
	} else {
		body["DocCount"] = limit
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%w: upstream request failed", ErrSearchProviderUnavailable)
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if readErr != nil {
		return nil, fmt.Errorf("%w: read response failed", ErrSearchProviderUnavailable)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: HTTP %d", ErrSearchProviderUnavailable, resp.StatusCode)
	}
	var envelope doubaoSearchEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("%w: invalid response", ErrSearchProviderUnavailable)
	}
	if hasJSONValue(envelope.ResponseMetadata.Error) && !isJSONNull(envelope.ResponseMetadata.Error) {
		return nil, fmt.Errorf("%w: provider request rejected", ErrSearchProviderUnavailable)
	}
	if !hasJSONValue(envelope.Result) || isJSONNull(envelope.Result) {
		return nil, fmt.Errorf("%w: missing search result", ErrSearchProviderUnavailable)
	}
	var result SearchResponse
	var normalizeErr error
	if p.edition == "custom" {
		result, normalizeErr = normalizeCustomResponse(envelope, query, limit)
	} else {
		result, normalizeErr = normalizeGlobalResponse(envelope, query, limit)
	}
	if normalizeErr != nil {
		return nil, fmt.Errorf("%w: invalid search result", ErrSearchProviderUnavailable)
	}
	return &result, nil
}

type doubaoSearchEnvelope struct {
	ResponseMetadata struct {
		RequestID string          `json:"RequestId"`
		Error     json.RawMessage `json:"Error"`
	} `json:"ResponseMetadata"`
	Result json.RawMessage `json:"Result"`
}

func normalizeStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			out = append(out, value)
			seen[value] = true
		}
	}
	return out
}

func hasJSONValue(value json.RawMessage) bool {
	return len(bytes.TrimSpace(value)) > 0
}

func isJSONNull(value json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(value), []byte("null"))
}

func normalizeCustomResponse(env doubaoSearchEnvelope, query string, limit int) (SearchResponse, error) {
	var raw struct {
		ResultCount int `json:"ResultCount"`
		WebResults  []struct {
			ID          string  `json:"Id"`
			Title       string  `json:"Title"`
			URL         string  `json:"Url"`
			Summary     string  `json:"Summary"`
			Snippet     string  `json:"Snippet"`
			Content     string  `json:"Content"`
			PublishTime string  `json:"PublishTime"`
			Auth        string  `json:"AuthInfoDes"`
			Rank        float64 `json:"RankScore"`
		} `json:"WebResults"`
		TimeCost int    `json:"TimeCost"`
		LogID    string `json:"LogId"`
	}
	if err := json.Unmarshal(env.Result, &raw); err != nil {
		return SearchResponse{}, err
	}
	items := make([]SearchResultItem, 0, len(raw.WebResults))
	for _, item := range raw.WebResults {
		snippet := item.Summary
		if snippet == "" {
			snippet = item.Snippet
		}
		items = append(items, SearchResultItem{Title: item.Title, URL: item.URL, Domain: domainOf(item.URL), Snippet: snippet, Content: item.Content, PublishedAt: item.PublishTime, CitationID: item.ID, Authority: item.Auth, RankScore: item.Rank})
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return SearchResponse{Provider: "doubao_custom", ProviderRequestID: env.ResponseMetadata.RequestID, Query: query, TotalCount: raw.ResultCount, Results: items, FetchedAt: time.Now().UTC(), ProviderMetadata: map[string]any{"edition": "custom", "time_cost_ms": raw.TimeCost, "log_id": raw.LogID}}, nil
}
func normalizeGlobalResponse(env doubaoSearchEnvelope, query string, limit int) (SearchResponse, error) {
	var raw struct {
		ErrorCode     int    `json:"ErrorCode"`
		ErrorMsg      string `json:"ErrorMsg"`
		TotalDocCount int    `json:"TotalDocCount"`
		Documents     []struct {
			Rank    int    `json:"Rank"`
			URL     string `json:"Url"`
			Title   string `json:"Title"`
			Snippet []struct {
				Type string `json:"Type"`
				Text string `json:"Text"`
			} `json:"Snippet"`
			Info struct {
				PublishTime string `json:"PublishTime"`
				Filetype    string `json:"Filetype"`
			} `json:"DocumentInfo"`
			Host struct {
				Hostname  string `json:"Hostname"`
				Authority string `json:"AuthorityLevel"`
			} `json:"HostInfo"`
		} `json:"Documents"`
	}
	if err := json.Unmarshal(env.Result, &raw); err != nil {
		return SearchResponse{}, err
	}
	if raw.ErrorCode != 0 {
		return SearchResponse{}, errors.New("global search returned an error")
	}
	items := make([]SearchResultItem, 0, len(raw.Documents))
	for _, doc := range raw.Documents {
		var parts []string
		for _, snip := range doc.Snippet {
			if snip.Type == "text" && strings.TrimSpace(snip.Text) != "" {
				parts = append(parts, snip.Text)
			}
		}
		items = append(items, SearchResultItem{Title: doc.Title, URL: doc.URL, Domain: domainOf(doc.URL), Snippet: strings.Join(parts, "\n"), PublishedAt: doc.Info.PublishTime, CitationID: doc.URL, Authority: doc.Host.Authority, Rank: doc.Rank, Type: doc.Info.Filetype})
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return SearchResponse{Provider: "doubao_global", ProviderRequestID: env.ResponseMetadata.RequestID, Query: query, TotalCount: raw.TotalDocCount, Results: items, FetchedAt: time.Now().UTC(), ProviderMetadata: map[string]any{"edition": "global"}}, nil
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
	if s != nil && s.config != nil {
		timeout := s.config.Custom.Timeout
		if strings.TrimSpace(s.config.Provider) == "global" {
			timeout = s.config.Global.Timeout
		}
		configured := timeout + time.Minute
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
	if err := s.provider.ValidateRequest(req.Request); err != nil {
		return nil, err
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
	providerName, modelName := s.provider.Name(), ""
	providerRequestID := ""
	resultCount := 0
	var usage map[string]any
	providerMetadata := make(map[string]any)
	if result != nil {
		if strings.TrimSpace(result.Provider) != "" {
			providerName = result.Provider
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
	blockHosts := make([]string, 0, len(req.Request.BlockHosts))
	for _, host := range req.Request.BlockHosts {
		if normalized := strings.TrimSpace(host); normalized != "" {
			blockHosts = append(blockHosts, normalized)
		}
	}
	payload, _ := json.Marshal(struct {
		Version       int      `json:"version"`
		UserID        string   `json:"user_id"`
		ProjectID     string   `json:"project_id"`
		TaskID        string   `json:"task_id"`
		ExecutionID   string   `json:"execution_id"`
		Query         string   `json:"query"`
		Limit         int      `json:"limit"`
		TimeRange     string   `json:"time_range"`
		Domains       []string `json:"domains"`
		BlockHosts    []string `json:"block_hosts"`
		NeedContent   *bool    `json:"need_content,omitempty"`
		NeedURL       *bool    `json:"need_url,omitempty"`
		AuthInfoLevel int      `json:"auth_info_level"`
		ContentFormat string   `json:"content_formats"`
		Industry      string   `json:"industry"`
		QueryRewrite  *bool    `json:"query_rewrite,omitempty"`
	}{1, strings.TrimSpace(req.UserID), strings.TrimSpace(req.ProjectID), strings.TrimSpace(req.TaskID), strings.TrimSpace(req.ExecutionID), strings.TrimSpace(req.Request.Query), req.Request.Limit, strings.TrimSpace(req.Request.TimeRange), domains, blockHosts, req.Request.NeedContent, req.Request.NeedURL, req.Request.AuthInfoLevel, strings.TrimSpace(req.Request.ContentFormats), strings.TrimSpace(req.Request.Industry), req.Request.QueryRewrite})
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
		return nil, errors.New("search provider cost requires a provider")
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
