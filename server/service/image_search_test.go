package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	serverconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

type imageSearchProviderStub struct {
	response *ImageSearchResponse
	err      error
	calls    int
}

func (p *imageSearchProviderStub) Name() string { return "doubao_global" }

func (p *imageSearchProviderStub) SearchImages(_ context.Context, _ ImageSearchRequest) (*ImageSearchResponse, error) {
	p.calls++
	return p.response, p.err
}

func TestDoubaoGlobalImageSearchProviderUsesDocumentedTextSearchContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/search_api/global_search" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" || r.Header.Get("Content-Type") != "application/json" {
			t.Fatal("expected server-owned bearer credentials and JSON content type")
		}
		var request struct {
			Query      string          `json:"Query"`
			SearchType string          `json:"SearchType"`
			DocCount   int             `json:"DocCount"`
			ImageQuery json.RawMessage `json:"ImageQuery"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Query != "雪山日出" || request.SearchType != "image" || request.DocCount != 5 || len(request.ImageQuery) != 0 {
			t.Fatalf("request = %+v", request)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ResponseMetadata": map[string]any{"RequestId": "image-request-1"},
			"Result": map[string]any{
				"ErrorCode": 0, "ErrorMsg": "", "TotalDocCount": 1,
				"Documents": []any{map[string]any{
					"Rank": 0, "Url": "https://travel.example.com/story", "Title": "雪山日出",
					"Snippet": []any{
						map[string]any{"Type": "text", "Text": "摄影作品"},
						map[string]any{"Type": "image", "Image": map[string]any{
							"ImageUrl": "https://cdn.example.com/sunrise.jpg", "Width": 1200, "Height": 800, "Alt": "雪山日出",
							"ThumbnailUrl": map[string]any{"Small": "https://cdn.example.com/sunrise-thumb.jpg"},
						}},
						map[string]any{"Type": "image", "Image": map[string]any{
							"ImageUrl": "https://cdn.example.com/sunrise-detail.jpg", "Width": 640, "Height": 480,
						}},
					},
					"DocumentInfo": map[string]any{"Filetype": "image", "PublishTime": "2026-10-20T08:30:00+08:00"},
					"HostInfo":     map[string]any{"Hostname": "travel.example.com"},
				}},
			},
		})
	}))
	defer server.Close()

	provider := NewDoubaoGlobalImageSearchProvider(serverconfig.SearchDoubaoConfig{GlobalEndpoint: server.URL + "/search_api/global_search", APIKey: "secret", Timeout: time.Second, MaxResults: 10})
	response, err := provider.SearchImages(context.Background(), ImageSearchRequest{Query: "雪山日出", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if response.Provider != "doubao_global" || response.ProviderRequestID != "image-request-1" || len(response.Results) != 2 {
		t.Fatalf("response = %#v", response)
	}
	item := response.Results[0]
	if item.ImageURL != "https://cdn.example.com/sunrise.jpg" || item.ThumbnailURL != "https://cdn.example.com/sunrise-thumb.jpg" || item.SourceURL != "https://travel.example.com/story" || item.Domain != "travel.example.com" || item.Description != "摄影作品\n雪山日出" || item.PublishedAt != "2026-10-20T08:30:00+08:00" || item.Width != 1200 || item.Height != 800 || item.Rank != 0 {
		t.Fatalf("normalized image result = %#v", item)
	}
	encoded, err := json.Marshal(response)
	if err != nil || strings.Contains(string(encoded), "secret") {
		t.Fatalf("provider credentials leaked in response: %s (%v)", encoded, err)
	}
	var publicResponse map[string]any
	if err := json.Unmarshal(encoded, &publicResponse); err != nil {
		t.Fatal(err)
	}
	if publicResponse["total_document_count"] != float64(1) || publicResponse["total_count"] != nil {
		t.Fatalf("response count must describe provider documents, not expanded images: %s", encoded)
	}
}

func TestDoubaoGlobalImageSearchRejectsResultLimitsAboveDocumentedCap(t *testing.T) {
	provider := NewDoubaoGlobalImageSearchProvider(serverconfig.SearchDoubaoConfig{GlobalEndpoint: "https://example.com/search_api/global_search", APIKey: "secret", Timeout: time.Second, MaxResults: 10})
	if _, err := provider.SearchImages(context.Background(), ImageSearchRequest{Query: "cats", Limit: 6}); !errors.Is(err, ErrInvalidImageSearchRequest) {
		t.Fatalf("limit 6 error = %v, want documented image result cap", err)
	}
}

func TestImageSearchRetriesProviderCostRecordingFromSavedResponse(t *testing.T) {
	repo, db, catalog, wallet := newSearchBillingFixture(t)
	defer repo.Close()
	provider := &imageSearchProviderStub{response: &ImageSearchResponse{
		Provider: "doubao_global", ProviderRequestID: "image-request-cost-retry", TotalDocumentCount: 1,
		Results: []ImageSearchResultItem{{ImageURL: "https://cdn.example.com/image.jpg"}}, FetchedAt: time.Now().UTC(),
	}}
	costs := NewProviderCostService(repository.NewBillingCostRepository(db), providerCostBundle())
	cfg := &serverconfig.SearchConfig{Images: serverconfig.SearchCapabilityConfig{Enabled: true, Provider: "doubao"}, Providers: serverconfig.SearchProvidersConfig{Doubao: serverconfig.SearchDoubaoConfig{Timeout: 30 * time.Second}}}
	svc := NewImageSearchService(cfg, provider, catalog, wallet, costs, repo.SearchOperations())
	req := ImageSearchOperationRequest{UserID: billingWalletUserID, ProjectID: "project-1", TaskID: "task-cost-retry", ExecutionID: "execution-cost-retry", Request: ImageSearchRequest{Query: "winter sky", Limit: 2}}
	if err := db.Exec("CREATE TRIGGER fail_image_search_cost BEFORE INSERT ON billing_provider_cost_events BEGIN SELECT RAISE(FAIL, 'simulated cost store outage'); END").Error; err != nil {
		t.Fatal(err)
	}
	if result, err := svc.SearchImages(context.Background(), req); err == nil || result != nil {
		t.Fatalf("first call with cost persistence failure = %#v, %v; want an error", result, err)
	}
	if err := db.Exec("DROP TRIGGER fail_image_search_cost").Error; err != nil {
		t.Fatal(err)
	}
	result, err := svc.SearchImages(context.Background(), req)
	if err != nil {
		t.Fatalf("retry saved image search result: %v", err)
	}
	if result.ProviderRequestID != "image-request-cost-retry" || provider.calls != 1 {
		t.Fatalf("retry reissued provider request: result=%#v calls=%d", result, provider.calls)
	}
	var costEvents int64
	if err := db.Model(&model.BillingProviderCostEvent{}).Where("task_id = ? AND operation = ?", req.TaskID, "mcp.search_images").Count(&costEvents).Error; err != nil {
		t.Fatal(err)
	}
	if costEvents != 1 {
		t.Fatalf("provider cost event count = %d, want 1", costEvents)
	}
}

func TestImageSearchRetriesProviderErrorCostRecordingWithoutRepeatingProviderRequest(t *testing.T) {
	repo, db, catalog, wallet := newSearchBillingFixture(t)
	defer repo.Close()
	provider := &imageSearchProviderStub{err: ErrSearchProviderUnavailable}
	costs := NewProviderCostService(repository.NewBillingCostRepository(db), providerCostBundle())
	cfg := &serverconfig.SearchConfig{Images: serverconfig.SearchCapabilityConfig{Enabled: true, Provider: "doubao"}, Providers: serverconfig.SearchProvidersConfig{Doubao: serverconfig.SearchDoubaoConfig{Timeout: 30 * time.Second}}}
	svc := NewImageSearchService(cfg, provider, catalog, wallet, costs, repo.SearchOperations())
	req := ImageSearchOperationRequest{UserID: billingWalletUserID, ProjectID: "project-1", TaskID: "task-error-cost-retry", ExecutionID: "execution-error-cost-retry", Request: ImageSearchRequest{Query: "winter sky", Limit: 2}}
	if err := db.Exec("CREATE TRIGGER fail_image_search_cost BEFORE INSERT ON billing_provider_cost_events BEGIN SELECT RAISE(FAIL, 'simulated cost store outage'); END").Error; err != nil {
		t.Fatal(err)
	}
	if result, err := svc.SearchImages(context.Background(), req); result != nil || !errors.Is(err, ErrImageSearchAccountingPending) {
		t.Fatalf("first provider failure with cost persistence outage = %#v, %v", result, err)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls = %d, want one", provider.calls)
	}
	if err := db.Exec("DROP TRIGGER fail_image_search_cost").Error; err != nil {
		t.Fatal(err)
	}
	if result, err := svc.SearchImages(context.Background(), req); result != nil || !errors.Is(err, ErrSearchProviderUnavailable) {
		t.Fatalf("recovered provider failure = %#v, %v", result, err)
	}
	if provider.calls != 1 {
		t.Fatalf("cost recovery repeated provider request: calls=%d", provider.calls)
	}
	var operation model.SearchOperation
	if err := db.Where("task_id = ?", req.TaskID).First(&operation).Error; err != nil {
		t.Fatal(err)
	}
	if operation.Status != model.SearchOperationFailed || operation.ErrorCode != "provider_unavailable" {
		t.Fatalf("recovered operation = %+v", operation)
	}
	var event model.BillingProviderCostEvent
	if err := db.Where("task_id = ? AND operation = ?", req.TaskID, "mcp.search_images").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	var evidence map[string]any
	if err := json.Unmarshal(event.UsageEvidence, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence["outcome"] != "provider_error" || evidence["search_request_count"] != float64(1) {
		t.Fatalf("provider error cost evidence = %#v", evidence)
	}
}

func TestDoubaoGlobalImageSearchRejectsProviderFailuresAndInvalidQueries(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body       string
		delay      time.Duration
		wantEmpty  bool
		wantFailed bool
	}{
		{name: "4xx body is hidden", status: http.StatusForbidden, body: "secret permission denied", wantFailed: true},
		{name: "5xx", status: http.StatusBadGateway, body: "upstream internals", wantFailed: true},
		{name: "invalid JSON", status: http.StatusOK, body: `{"Result":`, wantFailed: true},
		{name: "missing result", status: http.StatusOK, body: `{"ResponseMetadata":{"RequestId":"missing"}}`, wantFailed: true},
		{name: "provider error", status: http.StatusOK, body: `{"ResponseMetadata":{"Error":{"Code":"denied","Message":"secret"}},"Result":null}`, wantFailed: true},
		{name: "empty results", status: http.StatusOK, body: `{"ResponseMetadata":{"RequestId":"empty"},"Result":{"ErrorCode":0,"TotalDocCount":0,"Documents":[]}}`, wantEmpty: true},
		{name: "timeout", status: http.StatusOK, body: `{}`, delay: 50 * time.Millisecond, wantFailed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.delay > 0 {
					time.Sleep(tc.delay)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			provider := NewDoubaoGlobalImageSearchProvider(serverconfig.SearchDoubaoConfig{GlobalEndpoint: server.URL, APIKey: "secret", Timeout: 10 * time.Millisecond, MaxResults: 10})
			response, err := provider.SearchImages(context.Background(), ImageSearchRequest{Query: "cats"})
			if tc.wantFailed {
				if err == nil || !IsSearchProviderUnavailable(err) || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "permission denied") || strings.Contains(err.Error(), "upstream internals") {
					t.Fatalf("provider failure = %v, want sanitized provider_unavailable", err)
				}
				return
			}
			if err != nil || !tc.wantEmpty || response == nil || response.Results == nil || len(response.Results) != 0 {
				t.Fatalf("empty result = %#v, %v", response, err)
			}
		})
	}
	provider := NewDoubaoGlobalImageSearchProvider(serverconfig.SearchDoubaoConfig{GlobalEndpoint: "https://example.com/search_api/global_search", APIKey: "secret", Timeout: time.Second, MaxResults: 10})
	for _, query := range []string{"", strings.Repeat("x", 101)} {
		if _, err := provider.SearchImages(context.Background(), ImageSearchRequest{Query: query}); !errors.Is(err, ErrInvalidImageSearchRequest) {
			t.Errorf("invalid query length %d error = %v", len([]rune(query)), err)
		}
	}
}

func TestImageSearchServiceCreatesZeroPriceChargeAndReplaysIdempotently(t *testing.T) {
	repo, db, catalog, wallet := newSearchBillingFixture(t)
	defer repo.Close()
	provider := &imageSearchProviderStub{response: &ImageSearchResponse{
		Provider: "doubao_global", ProviderRequestID: "image-request-1", TotalDocumentCount: 1,
		Results:   []ImageSearchResultItem{{ImageURL: "https://cdn.example.com/image.jpg", SourceURL: "https://example.com/article"}},
		FetchedAt: time.Now().UTC(),
	}}
	bundle := providerCostBundle()
	costs := NewProviderCostService(repository.NewBillingCostRepository(db), bundle)
	cfg := &serverconfig.SearchConfig{Images: serverconfig.SearchCapabilityConfig{Enabled: true, Provider: "doubao"}, Providers: serverconfig.SearchProvidersConfig{Doubao: serverconfig.SearchDoubaoConfig{Timeout: 30 * time.Second}}}
	svc := NewImageSearchService(cfg, provider, catalog, wallet, costs, repo.SearchOperations())
	req := ImageSearchOperationRequest{UserID: billingWalletUserID, ProjectID: "project-1", TaskID: "task-1", ExecutionID: "execution-1", Request: ImageSearchRequest{Query: "snow mountain", Limit: 3}}
	first, err := svc.SearchImages(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.SearchImages(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if first.ProviderRequestID != second.ProviderRequestID || provider.calls != 1 {
		t.Fatalf("replay = %#v calls=%d", second, provider.calls)
	}
	var charges int64
	if err := db.Model(&model.BillingCharge{}).Where("user_id = ? AND sku_id = ?", billingWalletUserID, "mcp.search_images").Count(&charges).Error; err != nil {
		t.Fatal(err)
	}
	if charges != 1 {
		t.Fatalf("charge count = %d, want one", charges)
	}
	var account model.BillingWalletAccount
	if err := db.Where("user_id = ?", billingWalletUserID).First(&account).Error; err != nil {
		t.Fatal(err)
	}
	if account.PaidCredits != 0 || account.PromotionalCredits != 0 || account.DebtCredits != 0 {
		t.Fatalf("wallet changed: %+v", account)
	}
	var event model.BillingProviderCostEvent
	if err := db.Where("task_id = ?", "task-1").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	var evidence map[string]any
	if err := json.Unmarshal(event.UsageEvidence, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence["operation"] != "mcp.search_images" || evidence["search_request_count"] != float64(1) {
		t.Fatalf("provider cost evidence = %#v", evidence)
	}
	if evidence["query"] != redactSearchQuery("snow mountain") || evidence["query"] == "snow mountain" || event.Status != model.BillingProviderCostStatusUnreconciled || event.Model != "" {
		t.Fatalf("provider cost privacy/reconciliation = event %#v evidence %#v", event, evidence)
	}
}
