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
	"sort"
	"strconv"
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

type WebSearchRequest struct {
	Query          string
	Limit          int
	Sites          []string
	BlockedSites   []string
	After          string
	Before         string
	IncludeContent bool
}

type SearchResultItem struct {
	Type        string    `json:"type,omitempty"`
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	Domain      string    `json:"domain,omitempty"`
	Snippet     string    `json:"snippet,omitempty"`
	Summary     string    `json:"summary,omitempty"`
	Content     string    `json:"content,omitempty"`
	PublishedAt string    `json:"published_at,omitempty"`
	PageAge     string    `json:"page_age,omitempty"`
	CitationID  string    `json:"citation_id,omitempty"`
	SiteName    string    `json:"site_name,omitempty"`
	SiteIcon    string    `json:"site_icon,omitempty"`
	Authority   string    `json:"authority,omitempty"`
	Rank        int       `json:"rank,omitempty"`
	RankScore   float64   `json:"rank_score,omitempty"`
	FetchedAt   time.Time `json:"fetched_at,omitempty"`
}

type WebSearchResponse struct {
	Provider          string             `json:"provider"`
	ProviderRequestID string             `json:"provider_request_id,omitempty"`
	Query             string             `json:"query"`
	TotalCount        int                `json:"total_count,omitempty"`
	Results           []SearchResultItem `json:"results"`
	FetchedAt         time.Time          `json:"fetched_at"`
	Usage             map[string]any     `json:"usage,omitempty"`
	ProviderMetadata  map[string]any     `json:"provider_metadata,omitempty"`
}

type WebSearchProvider interface {
	Search(context.Context, WebSearchRequest) (*WebSearchResponse, error)
}

type PageFetchRequest struct {
	URL string
}

type PageFetchResponse struct {
	Title             string         `json:"title,omitempty"`
	URL               string         `json:"url"`
	ContentMarkdown   string         `json:"content_markdown"`
	FetchedAt         time.Time      `json:"fetched_at"`
	Provider          string         `json:"provider"`
	ProviderRequestID string         `json:"provider_request_id,omitempty"`
	ProviderMetadata  map[string]any `json:"provider_metadata,omitempty"`
}

type PageFetchProvider interface {
	Fetch(context.Context, PageFetchRequest) (*PageFetchResponse, error)
}

type ImageSearchImagesRequest struct {
	Query    string
	ImageURL string
	Limit    int
	Sites    []string
}

type ImageSearchResult struct {
	Title            string         `json:"title,omitempty"`
	ImageURL         string         `json:"image_url"`
	SourceURL        string         `json:"source_url,omitempty"`
	Domain           string         `json:"domain,omitempty"`
	CitationID       string         `json:"citation_id,omitempty"`
	FetchedAt        string         `json:"fetched_at,omitempty"`
	ProviderMetadata map[string]any `json:"provider_metadata,omitempty"`
}

type ImageSearchResponse struct {
	Provider          string              `json:"provider"`
	ProviderRequestID string              `json:"provider_request_id,omitempty"`
	Query             string              `json:"query,omitempty"`
	Results           []ImageSearchResult `json:"results"`
	FetchedAt         time.Time           `json:"fetched_at"`
}

type ImageSearchProvider interface {
	SearchImages(context.Context, ImageSearchImagesRequest) (*ImageSearchResponse, error)
}

type DoubaoSearchProvider struct {
	client        *http.Client
	endpoint      string
	apiKey        string
	edition       string
	maxWebResults int
}

// DeepSeekSearchProvider implements DeepSeek's standalone Search API. Fetch is
// intentionally outside search_web so the MCP contract remains one atomic
// web-search capability.
type DeepSeekSearchProvider struct {
	client     *http.Client
	endpoint   string
	apiKey     string
	maxResults int
}

func NewDeepSeekSearchProvider(cfg serverconfig.SearchDeepSeekConfig) *DeepSeekSearchProvider {
	endpoint := cfg.SearchEndpoint
	if endpoint == "" {
		endpoint = "https://api.deepseek.com/v1/tools/search"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	max := cfg.MaxResults
	if max <= 0 || max > 50 {
		max = 10
	}
	return &DeepSeekSearchProvider{
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		endpoint:   strings.TrimRight(strings.TrimSpace(endpoint), "/"),
		apiKey:     strings.TrimSpace(cfg.APIKey),
		maxResults: max,
	}
}

func (p *DeepSeekSearchProvider) Name() string {
	if p == nil {
		return ""
	}
	return "deepseek"
}

func (p *DeepSeekSearchProvider) ValidateRequest(req WebSearchRequest) error {
	if p == nil {
		return ErrSearchProviderUnavailable
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return errors.New("query is required")
	}
	if len([]rune(query)) > 128 {
		return errors.New("query exceeds the 128-character limit")
	}
	if req.Limit < 0 || req.Limit > p.maxResults {
		return fmt.Errorf("limit must be between 1 and %d", p.maxResults)
	}
	if len(req.Sites) > 20 || len(req.BlockedSites) > 20 {
		return errors.New("sites and blocked_sites support at most 20 entries")
	}
	if req.After != "" || req.Before != "" {
		if req.After != "" && !validSearchDate(req.After) || req.Before != "" && !validSearchDate(req.Before) {
			return errors.New("after and before must use YYYY-MM-DD")
		}
		if req.After != "" && req.Before != "" && strings.TrimSpace(req.After) > strings.TrimSpace(req.Before) {
			return errors.New("after must not be later than before")
		}
	}
	return nil
}

func deepSeekQuery(req WebSearchRequest) string {
	query := strings.TrimSpace(req.Query)
	for _, site := range normalizeStrings(req.Sites) {
		query += " site:" + site
	}
	for _, site := range normalizeStrings(req.BlockedSites) {
		query += " -site:" + site
	}
	if strings.TrimSpace(req.After) != "" {
		query += " after:" + strings.TrimSpace(req.After)
	}
	if strings.TrimSpace(req.Before) != "" {
		query += " before:" + strings.TrimSpace(req.Before)
	}
	return query
}

func (p *DeepSeekSearchProvider) Search(ctx context.Context, req WebSearchRequest) (*WebSearchResponse, error) {
	if p == nil || p.client == nil || p.endpoint == "" || p.apiKey == "" {
		return nil, ErrSearchProviderUnavailable
	}
	if err := p.ValidateRequest(req); err != nil {
		return nil, err
	}
	query := deepSeekQuery(req)
	limit := req.Limit
	if limit <= 0 {
		limit = p.maxResults
	}
	body := map[string]any{"query": query, "count": limit}
	if req.IncludeContent {
		body["need_content"] = true
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
	const maxResponseBytes = 4 << 20
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if readErr != nil {
		return nil, fmt.Errorf("%w: read response failed", ErrSearchProviderUnavailable)
	}
	if len(data) > maxResponseBytes {
		return nil, fmt.Errorf("%w: response too large", ErrSearchProviderUnavailable)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: HTTP %d", ErrSearchProviderUnavailable, resp.StatusCode)
	}
	var decoded struct {
		Results *[]struct {
			RerankScore float64 `json:"rerank_score"`
			Title       string  `json:"title"`
			URL         string  `json:"url"`
			Snippet     string  `json:"snippet"`
			Summary     string  `json:"summary"`
			Content     string  `json:"content"`
			SiteName    string  `json:"site_name"`
			SiteIcon    string  `json:"site_icon"`
			Date        string  `json:"date"`
			Authority   int     `json:"authority"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil || decoded.Results == nil {
		return nil, fmt.Errorf("%w: invalid response", ErrSearchProviderUnavailable)
	}
	items := make([]SearchResultItem, 0, len(*decoded.Results))
	fetchedAt := time.Now().UTC()
	for _, item := range *decoded.Results {
		items = append(items, SearchResultItem{
			Title: item.Title, URL: item.URL, Domain: domainOf(item.URL), Snippet: item.Snippet, Summary: item.Summary,
			Content: item.Content, PublishedAt: item.Date, CitationID: item.URL,
			SiteName: item.SiteName, SiteIcon: item.SiteIcon, Authority: strconv.Itoa(item.Authority), RankScore: item.RerankScore, FetchedAt: fetchedAt,
		})
		if len(items) == limit {
			break
		}
	}
	return &WebSearchResponse{
		Provider: "deepseek", ProviderRequestID: firstHeader(resp.Header, "X-Request-ID", "Request-ID"),
		Query: query, TotalCount: len(items), Results: items, FetchedAt: time.Now().UTC(),
		ProviderMetadata: map[string]any{"api": "search"},
	}, nil
}

func firstHeader(headers http.Header, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(headers.Get(name)); value != "" {
			return value
		}
	}
	return ""
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
	max := cfg.MaxResults
	maxAllowed := 50
	if edition == "global" {
		maxAllowed = 20
	}
	if max <= 0 || max > maxAllowed {
		max = 10
	}
	endpoint := cfg.CustomEndpoint
	if edition == "global" {
		endpoint = cfg.GlobalEndpoint
	}
	return &DoubaoSearchProvider{client: &http.Client{Timeout: timeout}, endpoint: strings.TrimRight(strings.TrimSpace(endpoint), "/"), apiKey: strings.TrimSpace(cfg.APIKey), edition: edition, maxWebResults: max}
}
func (p *DoubaoSearchProvider) Name() string {
	if p == nil {
		return ""
	}
	return "doubao_" + p.edition
}
func (p *DoubaoSearchProvider) ValidateRequest(req WebSearchRequest) error {
	if strings.TrimSpace(req.Query) == "" {
		return errors.New("query is required")
	}
	if len([]rune(strings.TrimSpace(req.Query))) > 100 {
		return errors.New("query exceeds the 100-character limit")
	}
	if req.Limit < 0 || req.Limit > p.maxWebResults {
		return fmt.Errorf("limit must be between 1 and %d", p.maxWebResults)
	}
	if len(req.Sites) > 20 || len(req.BlockedSites) > 20 {
		return errors.New("sites and blocked_sites support at most 20 entries")
	}
	if req.After != "" || req.Before != "" {
		if req.After != "" && !validSearchDate(req.After) || req.Before != "" && !validSearchDate(req.Before) {
			return errors.New("after and before must use YYYY-MM-DD")
		}
		if req.After != "" && req.Before != "" && strings.TrimSpace(req.After) > strings.TrimSpace(req.Before) {
			return errors.New("after must not be later than before")
		}
	}
	if p.edition != "custom" && p.edition != "global" {
		return errors.New("unsupported Doubao search edition")
	}
	return nil
}

func validSearchDate(value string) bool {
	_, err := time.Parse("2006-01-02", strings.TrimSpace(value))
	return err == nil
}
func (p *DoubaoSearchProvider) Search(ctx context.Context, req WebSearchRequest) (*WebSearchResponse, error) {
	if p == nil || p.client == nil || p.endpoint == "" || p.apiKey == "" {
		return nil, ErrSearchProviderUnavailable
	}
	if err := p.ValidateRequest(req); err != nil {
		return nil, err
	}
	query := strings.TrimSpace(req.Query)
	if p.edition == "global" {
		for _, site := range normalizeStrings(req.Sites) {
			query += " site:" + site
		}
		for _, site := range normalizeStrings(req.BlockedSites) {
			query += " -site:" + site
		}
		if req.After != "" {
			query += " after:" + strings.TrimSpace(req.After)
		}
		if req.Before != "" {
			query += " before:" + strings.TrimSpace(req.Before)
		}
	}
	limit := req.Limit
	if limit <= 0 {
		limit = p.maxWebResults
	}
	body := map[string]any{"Query": query, "SearchType": "web"}
	if p.edition == "custom" {
		body["Count"] = limit
		filter := map[string]any{}
		if req.IncludeContent {
			filter["NeedContent"] = true
		}
		sites := append([]string{}, req.Sites...)
		if len(sites) > 0 {
			filter["Sites"] = strings.Join(normalizeStrings(sites), "|")
		}
		blocked := append([]string{}, req.BlockedSites...)
		if len(blocked) > 0 {
			filter["BlockHosts"] = strings.Join(normalizeStrings(blocked), "|")
		}
		if len(filter) > 0 {
			body["Filter"] = filter
		}
		if req.After != "" || req.Before != "" {
			filter["TimeRange"] = strings.TrimSpace(req.After) + ".." + strings.TrimSpace(req.Before)
			body["Filter"] = filter
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
	var result WebSearchResponse
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

func normalizeCustomResponse(env doubaoSearchEnvelope, query string, limit int) (WebSearchResponse, error) {
	var raw struct {
		ResultCount *int `json:"ResultCount"`
		WebResults  *[]struct {
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
		return WebSearchResponse{}, err
	}
	if raw.ResultCount == nil || raw.WebResults == nil || *raw.ResultCount < 0 {
		return WebSearchResponse{}, errors.New("custom search result is missing required fields")
	}
	items := make([]SearchResultItem, 0, len(*raw.WebResults))
	fetchedAt := time.Now().UTC()
	for _, item := range *raw.WebResults {
		snippet := item.Summary
		if snippet == "" {
			snippet = item.Snippet
		}
		items = append(items, SearchResultItem{Title: item.Title, URL: item.URL, Domain: domainOf(item.URL), Snippet: snippet, Content: item.Content, PublishedAt: item.PublishTime, CitationID: item.ID, Authority: item.Auth, RankScore: item.Rank, FetchedAt: fetchedAt})
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return WebSearchResponse{Provider: "doubao_custom", ProviderRequestID: env.ResponseMetadata.RequestID, Query: query, TotalCount: *raw.ResultCount, Results: items, FetchedAt: time.Now().UTC(), ProviderMetadata: map[string]any{"edition": "custom", "time_cost_ms": raw.TimeCost, "log_id": raw.LogID}}, nil
}
func normalizeGlobalResponse(env doubaoSearchEnvelope, query string, limit int) (WebSearchResponse, error) {
	var raw struct {
		ErrorCode     *int   `json:"ErrorCode"`
		ErrorMsg      string `json:"ErrorMsg"`
		TotalDocCount *int   `json:"TotalDocCount"`
		Documents     *[]struct {
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
		return WebSearchResponse{}, err
	}
	if raw.ErrorCode == nil || raw.TotalDocCount == nil || raw.Documents == nil || *raw.ErrorCode != 0 || *raw.TotalDocCount < 0 {
		return WebSearchResponse{}, errors.New("global search returned an error")
	}
	items := make([]SearchResultItem, 0, len(*raw.Documents))
	fetchedAt := time.Now().UTC()
	for _, doc := range *raw.Documents {
		var parts []string
		for _, snip := range doc.Snippet {
			if snip.Type == "text" && strings.TrimSpace(snip.Text) != "" {
				parts = append(parts, snip.Text)
			}
		}
		items = append(items, SearchResultItem{Title: doc.Title, URL: doc.URL, Domain: domainOf(doc.URL), Snippet: strings.Join(parts, "\n"), PublishedAt: doc.Info.PublishTime, CitationID: doc.URL, Authority: doc.Host.Authority, Rank: doc.Rank, Type: doc.Info.Filetype, FetchedAt: fetchedAt})
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return WebSearchResponse{Provider: "doubao_global", ProviderRequestID: env.ResponseMetadata.RequestID, Query: query, TotalCount: *raw.TotalDocCount, Results: items, FetchedAt: time.Now().UTC(), ProviderMetadata: map[string]any{"edition": "global"}}, nil
}
func domainOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func webProviderName(provider WebSearchProvider) string {
	if provider == nil {
		return ""
	}
	if named, ok := provider.(interface{ Name() string }); ok {
		return strings.TrimSpace(named.Name())
	}
	return "unknown"
}

type SearchService struct {
	config     *serverconfig.SearchConfig
	provider   WebSearchProvider
	catalog    *BillingCatalogService
	wallet     *BillingWalletService
	costs      *ProviderCostService
	operations repository.SearchOperationRepository
	logger     *zerolog.Logger
}

func NewSearchService(cfg *serverconfig.SearchConfig, provider WebSearchProvider, catalog *BillingCatalogService, wallet *BillingWalletService, costs *ProviderCostService, operations repository.SearchOperationRepository, logger ...*zerolog.Logger) *SearchService {
	var log *zerolog.Logger
	if len(logger) > 0 {
		log = logger[0]
	}
	return &SearchService{config: cfg, provider: provider, catalog: catalog, wallet: wallet, costs: costs, operations: operations, logger: log}
}

func (s *SearchService) operationLease() time.Duration {
	lease := 5 * time.Minute
	if s != nil && s.config != nil {
		timeout := s.config.Providers.DeepSeek.Timeout
		if strings.HasPrefix(strings.TrimSpace(s.config.Web.Provider), "doubao_") {
			timeout = s.config.Providers.Doubao.Timeout
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
	Request     WebSearchRequest
}

func (s *SearchService) Search(ctx context.Context, req SearchOperationRequest) (*WebSearchResponse, error) {
	if s == nil || s.config == nil || !webSearchEnabled(s.config) || s.provider == nil {
		return nil, ErrSearchProviderUnavailable
	}
	if strings.TrimSpace(req.UserID) == "" || strings.TrimSpace(req.ProjectID) == "" || strings.TrimSpace(req.TaskID) == "" || strings.TrimSpace(req.ExecutionID) == "" {
		return nil, errors.New("search execution identity is required")
	}
	if strings.TrimSpace(req.Request.Query) == "" {
		return nil, errors.New("query is required")
	}
	if len([]rune(req.Request.Query)) > 128 {
		return nil, errors.New("query exceeds the 128-character limit")
	}
	if validator, ok := s.provider.(interface{ ValidateRequest(WebSearchRequest) error }); ok {
		if err := validator.ValidateRequest(req.Request); err != nil {
			return nil, err
		}
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
				var replay WebSearchResponse
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
		candidate := &model.SearchOperation{Operation: "mcp.search_web", UserID: req.UserID, ProjectID: req.ProjectID, TaskID: req.TaskID, ExecutionID: req.ExecutionID, RequestFingerprint: fingerprint, AttemptID: attemptID}
		var claimed bool
		operation, claimed, err = s.operations.Claim(ctx, candidate, time.Now().UTC(), s.operationLease())
		if err != nil {
			return nil, fmt.Errorf("claim search operation: %w", err)
		}
		if !claimed {
			if operation != nil && operation.Status == model.SearchOperationSucceeded {
				var replay WebSearchResponse
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

func webSearchEnabled(cfg *serverconfig.SearchConfig) bool {
	return cfg != nil && cfg.Web.Enabled && strings.TrimSpace(cfg.Web.Provider) != ""
}

func (s *SearchService) recordSearchProviderCost(ctx context.Context, operation SearchOperationRequest, fingerprint, attemptID, outcome string, result *WebSearchResponse) {
	if s == nil || s.costs == nil || s.provider == nil {
		return
	}
	providerName, modelName := webProviderName(s.provider), ""
	providerRequestID := ""
	resultCount := 0
	var usage map[string]any
	providerMetadata := map[string]any{
		"search_type": "web",
		"filters": map[string]any{
			"sites":           normalizedFingerprintStrings(operation.Request.Sites),
			"blocked_sites":   normalizedFingerprintStrings(operation.Request.BlockedSites),
			"after":           strings.TrimSpace(operation.Request.After),
			"before":          strings.TrimSpace(operation.Request.Before),
			"include_content": operation.Request.IncludeContent,
		},
		"query_length": len([]rune(strings.TrimSpace(operation.Request.Query))),
	}
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
		Query: redactSearchQuery(operation.Request.Query), ResultCount: resultCount, SearchRequestCount: 1,
		Usage: usage, ProviderMetadata: providerMetadata, Outcome: outcome, Operation: "mcp.search_web",
	})
	if costErr != nil && s.logger != nil {
		s.logger.Error().Err(costErr).Str("operation", "mcp.search_web").Msg("failed to record search provider cost; preserving operation outcome")
	}
}

func redactSearchQuery(query string) string {
	value := strings.TrimSpace(query)
	digest := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func searchFingerprint(req SearchOperationRequest) string {
	payload, _ := json.Marshal(struct {
		Version        int      `json:"version"`
		Operation      string   `json:"operation"`
		UserID         string   `json:"user_id"`
		ProjectID      string   `json:"project_id"`
		TaskID         string   `json:"task_id"`
		ExecutionID    string   `json:"execution_id"`
		Query          string   `json:"query"`
		Limit          int      `json:"limit"`
		Sites          []string `json:"sites"`
		BlockedSites   []string `json:"blocked_sites"`
		After          string   `json:"after"`
		Before         string   `json:"before"`
		IncludeContent bool     `json:"include_content"`
	}{2, "mcp.search_web", strings.TrimSpace(req.UserID), strings.TrimSpace(req.ProjectID), strings.TrimSpace(req.TaskID), strings.TrimSpace(req.ExecutionID), strings.TrimSpace(req.Request.Query), req.Request.Limit, normalizedFingerprintStrings(req.Request.Sites), normalizedFingerprintStrings(req.Request.BlockedSites), strings.TrimSpace(req.Request.After), strings.TrimSpace(req.Request.Before), req.Request.IncludeContent})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func normalizedFingerprintStrings(values []string) []string {
	values = normalizeStrings(values)
	sort.Strings(values)
	return values
}

func (s *BillingCatalogService) CatalogID() string {
	if s == nil {
		return ""
	}
	return s.bundle.Products.CatalogID
}

type RecordSearchRequestCost struct {
	TaskID, Provider, Model, ProviderRequestID, AttemptID, RequestFingerprint, Query, URL, Operation string
	ResultCount                                                                                      int
	SearchRequestCount                                                                               int
	Usage                                                                                            map[string]any
	ProviderMetadata                                                                                 map[string]any
	Outcome                                                                                          string
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
	if operation := strings.TrimSpace(req.Operation); operation != "" {
		evidence["operation"] = operation
	}
	if targetURL := strings.TrimSpace(req.URL); targetURL != "" {
		evidence["url"] = targetURL
	}
	if fingerprint := strings.TrimSpace(req.RequestFingerprint); fingerprint != "" {
		evidence["operation_fingerprint"] = fingerprint
	}
	return s.appendProviderRequestEvent(ctx, providerRequestEvent{TaskID: req.TaskID, Provider: provider, Model: modelID, ProviderRequestID: requestID, CatalogID: s.catalogID, IdempotencyKey: requestID, Operation: req.Operation, Source: model.BillingProviderCostSourceProviderResponse, Status: model.BillingProviderCostStatusUnreconciled, Evidence: evidence, Calculation: map[string]any{"version": 1, "reason_code": string(model.BillingExecutionCostReasonMissingProviderUsage)}})
}
