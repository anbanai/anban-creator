package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/agent"
	appimage "github.com/anbanai/anban-creator/server/app/image"
	appwechat "github.com/anbanai/anban-creator/server/app/wechat"
	"github.com/anbanai/anban-creator/server/billing"
	serverconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/rs/zerolog"
)

func billingPaidSearchSKUs() []billing.SKUConfig {
	return []billing.SKUConfig{
		{ID: "mcp.search_web", Operation: "mcp.search_web", ChargePolicy: "standalone_operation", PriceCredits: 300, Route: "search.web", Delivery: "search_results"},
		{ID: "mcp.fetch_web_page", Operation: "mcp.fetch_web_page", ChargePolicy: "standalone_operation", PriceCredits: 300, Route: "search.fetch", Delivery: "fetched_page"},
	}
}

func attachImageReuseRun(t *testing.T, f *taskImageFixture, maxCost, daily int64) *model.ContentReuseRun {
	t.Helper()
	run := model.NewContentReuseRun(f.userID, "asset", f.projectID, model.PlatformSeednote)
	run.Status, run.TaskID, run.BudgetReserved = model.ContentReuseRunStatusRunning, f.taskID, 100
	now := time.Now().UTC().Truncate(24 * time.Hour)
	run.BudgetDate = &now
	run.PolicySnapshot = mustContentAssetJSON(map[string]any{"enabled": true, "max_run_cost_credits": maxCost, "daily_budget_credits": daily})
	if err := f.repo.ContentAssets().CreateReuseRun(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	policy := model.NewContentAutopilotPolicy(f.userID)
	policy.Enabled, policy.MaxRunCostCredits, policy.DailyBudgetCredits = true, maxCost, daily
	if err := f.repo.ContentAssets().UpsertPolicy(context.Background(), policy); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&model.Task{}).Where("id = ?", f.taskID).Updates(map[string]any{"reuse_run_id": run.ID, "billing_price_credits": 100}).Error; err != nil {
		t.Fatal(err)
	}
	return run
}

type blockingReuseImageGenerator struct {
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

type reuseImageGeneratorFunc func() (*ImageResult, error)

func (g reuseImageGeneratorFunc) GenerateImage(context.Context, string, string, string, string, string, string, []string, string, string, *ResolvedImageModel, *bool) (*ImageResult, error) {
	return g()
}

func TestReuseImagePostProviderFailureRetainsCapacity(t *testing.T) {
	for _, failure := range []string{"nil", "unreadable", "normalization"} {
		t.Run(failure, func(t *testing.T) {
			f := newTaskImageFixture(t)
			run := attachImageReuseRun(t, f, 1000, 1000)
			calls := 0
			f.service.generator = reuseImageGeneratorFunc(func() (*ImageResult, error) {
				calls++
				if failure == "nil" {
					return nil, nil
				}
				payload := []byte("not an image")
				if failure == "normalization" {
					payload = taskImagePNG(1, 1)
				}
				if err := os.WriteFile(f.generator.result.LocalFilePath, payload, 0o644); err != nil {
					t.Fatal(err)
				}
				return f.generator.result, nil
			})
			if _, err := f.service.Generate(context.Background(), f.request()); err == nil {
				t.Fatal("invalid provider result accepted")
			}
			var capacity int64
			if err := f.db.Model(&model.ContentReuseOperationBudget{}).Where("run_id = ?", run.ID).Select("COALESCE(SUM(amount), 0)").Scan(&capacity).Error; err != nil || capacity != 500 {
				t.Fatalf("paid capacity released: capacity=%d err=%v", capacity, err)
			}
			current, _ := f.repo.ContentAssets().FindReuseRunByID(context.Background(), run.ID)
			if current.Status != model.ContentReuseRunStatusPaused || current.ErrorCode != "operation_reconciliation_required" {
				t.Fatalf("unresolved result did not pause: %+v", current)
			}
			if _, err := f.service.Generate(context.Background(), f.request()); err == nil || calls != 1 {
				t.Fatalf("paid provider repeated: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestReuseLateImageSettlementReconcilesOnlyOperationHold(t *testing.T) {
	for _, control := range []string{"resume", "other_unresolved", "user_pause", "cancelled"} {
		t.Run(control, func(t *testing.T) {
			f := newTaskImageFixture(t)
			run := attachImageReuseRun(t, f, 2000, 2000)
			ctx := context.Background()
			f.service.generator = reuseImageGeneratorFunc(func() (*ImageResult, error) {
				if control == "other_unresolved" {
					if err := f.db.Create(&model.ContentReuseOperationBudget{RunID: run.ID, OperationKey: "other", Fingerprint: "other", Amount: 50, Status: "ambiguous"}).Error; err != nil {
						t.Fatal(err)
					}
				}
				if control == "user_pause" || control == "cancelled" {
					status := model.ContentReuseRunStatusPaused
					if control == "cancelled" {
						status = model.ContentReuseRunStatusCancelled
					}
					if err := f.db.Model(&model.ContentReuseRun{}).Where("id = ?", run.ID).Update("status", status).Error; err != nil {
						t.Fatal(err)
					}
				}
				if err := f.db.Model(&model.ContentReuseOperationBudget{}).Where("run_id = ?", run.ID).Update("created_at", time.Now().Add(-20*time.Minute)).Error; err != nil {
					t.Fatal(err)
				}
				if err := f.repo.ContentAssets().RecoverReuseOperationBudgets(ctx, time.Now()); err != nil {
					t.Fatal(err)
				}
				current, _ := f.repo.ContentAssets().FindReuseRunByID(ctx, run.ID)
				if control == "user_pause" && current.Status != model.ContentReuseRunStatusPaused {
					t.Fatalf("user pause overwritten: %+v", current)
				}
				return f.generator.GenerateImage(ctx, "", "", "", "", f.request().OutputPath, "", nil, "", "3:4", nil, nil)
			})
			if _, err := f.service.Generate(ctx, f.request()); err != nil {
				t.Fatal(err)
			}
			assets := NewContentAssetService(f.repo)
			assets.SetEnqueuer(&contentAssetTestEnqueuer{})
			if _, err := assets.SetReuseRunStatus(ctx, f.userID, run.ID, model.ContentReuseRunStatusQueued); !errors.Is(err, ErrInvalidReuseRunState) {
				t.Fatalf("unsettled result allowed resume: %v", err)
			}
			if processed, err := f.wallet.ProcessSettlementOutbox(ctx, 10); err != nil || processed != 1 {
				t.Fatalf("settlement=%d err=%v", processed, err)
			}
			operationID, _, _ := taskImageOperationIdentity(f.request())
			if err := f.repo.ContentAssets().MarkReuseOperationAmbiguous(ctx, run.ID, billingFingerprint(f.taskID, f.executionID, operationID)); err != nil {
				t.Fatal(err)
			}
			current, _ := f.repo.ContentAssets().FindReuseRunByID(ctx, run.ID)
			wantStatus := model.ContentReuseRunStatusPaused
			if control == "cancelled" {
				wantStatus = model.ContentReuseRunStatusCancelled
			}
			wantCode := ""
			if control == "other_unresolved" {
				wantCode = "operation_reconciliation_required"
			}
			if current.Status != wantStatus || current.ErrorCode != wantCode || current.CostCredits != 600 {
				t.Fatalf("late settlement run=%+v want status=%s code=%s", current, wantStatus, wantCode)
			}
			if _, err := f.service.Generate(ctx, f.request()); err != nil || f.generator.calls != 1 {
				t.Fatalf("durable replay calls=%d err=%v", f.generator.calls, err)
			}
			_, err := assets.SetReuseRunStatus(ctx, f.userID, run.ID, model.ContentReuseRunStatusQueued)
			if control == "other_unresolved" || control == "cancelled" {
				if !errors.Is(err, ErrInvalidReuseRunState) {
					t.Fatalf("unsafe resume=%v", err)
				}
			} else if err != nil {
				t.Fatalf("reconciled operation cannot resume: %v", err)
			}
		})
	}
}

type reuseSearchProviderFunc func() (*WebSearchResponse, error)

func (p reuseSearchProviderFunc) Search(context.Context, WebSearchRequest) (*WebSearchResponse, error) {
	return p()
}

type reuseFetchProviderFunc func() (*PageFetchResponse, error)

func (p reuseFetchProviderFunc) Fetch(context.Context, PageFetchRequest) (*PageFetchResponse, error) {
	return p()
}

func TestReuseMalformedPaidSearchAndFetchRetainsChargeAndBlocksReplay(t *testing.T) {
	for _, fetch := range []bool{false, true} {
		t.Run(fmt.Sprint("fetch=", fetch), func(t *testing.T) {
			f := newTaskImageFixture(t)
			run := attachImageReuseRun(t, f, 1000, 1000)
			ctx := context.Background()
			bundle := f.wallet.bundle
			bundle.Products.CatalogID += "-malformed-search"
			bundle.Products.SKUs = append(bundle.Products.SKUs, billingPaidSearchSKUs()...)
			bundle.Policy.TaskFailureReversal = billing.TaskFailureReversalPolicy{Enabled: true, Reasons: []string{"provider_error"}}
			f.wallet.bundle = bundle
			catalog := NewBillingCatalogService(f.repo, &bundle, BillingCatalogOptions{})
			if _, err := catalog.Publish(ctx); err != nil {
				t.Fatal(err)
			}
			cfg := &serverconfig.SearchConfig{}
			cfg.Web.Enabled, cfg.Web.Provider = true, "doubao_custom"
			cfg.Fetch.Enabled, cfg.Fetch.Provider = true, "deepseek"
			calls := 0
			var invoke func() error
			if fetch {
				svc := NewFetchService(cfg, reuseFetchProviderFunc(func() (*PageFetchResponse, error) { calls++; return nil, nil }), catalog, f.wallet, nil, f.repo.SearchOperations())
				req := FetchOperationRequest{UserID: f.userID, ProjectID: f.projectID, TaskID: f.taskID, ExecutionID: f.executionID, Request: PageFetchRequest{URL: "https://example.com"}}
				invoke = func() error { _, err := svc.Fetch(ctx, req); return err }
			} else {
				svc := NewSearchService(cfg, reuseSearchProviderFunc(func() (*WebSearchResponse, error) { calls++; return nil, nil }), catalog, f.wallet, nil, f.repo.SearchOperations())
				req := SearchOperationRequest{UserID: f.userID, ProjectID: f.projectID, TaskID: f.taskID, ExecutionID: f.executionID, Request: WebSearchRequest{Query: "content"}}
				invoke = func() error { _, err := svc.Search(ctx, req); return err }
			}
			if err := invoke(); err == nil {
				t.Fatal("malformed result accepted")
			}
			current, _ := f.repo.ContentAssets().FindReuseRunByID(ctx, run.ID)
			account, _ := f.repo.Billing().FindAccount(ctx, f.userID)
			if current.Status != model.ContentReuseRunStatusPaused || current.ErrorCode != "operation_reconciliation_required" || account.PaidCredits != 700 {
				t.Fatalf("uncertain provider spend released: run=%+v credits=%d", current, account.PaidCredits)
			}
			if err := invoke(); err == nil || calls != 1 {
				t.Fatalf("malformed paid result repeated: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestReuseLateSearchAndFetchSnapshotReconcilesHold(t *testing.T) {
	for _, fetch := range []bool{false, true} {
		for _, outstanding := range []bool{false, true} {
			t.Run(fmt.Sprint("fetch=", fetch, "/other_unresolved=", outstanding), func(t *testing.T) {
				f := newTaskImageFixture(t)
				run := attachImageReuseRun(t, f, 1000, 1000)
				ctx := context.Background()
				bundle := f.wallet.bundle
				bundle.Products.CatalogID += "-late-search"
				bundle.Products.SKUs = append(bundle.Products.SKUs, billingPaidSearchSKUs()...)
				catalog := NewBillingCatalogService(f.repo, &bundle, BillingCatalogOptions{})
				if _, err := catalog.Publish(ctx); err != nil {
					t.Fatal(err)
				}
				cfg := &serverconfig.SearchConfig{}
				cfg.Web.Enabled, cfg.Web.Provider = true, "doubao_custom"
				cfg.Fetch.Enabled, cfg.Fetch.Provider = true, "deepseek"
				var invoke func() error
				calls := 0
				late := func() {
					calls++
					if outstanding {
						other := NewSearchService(cfg, &searchProviderStub{err: errors.New("provider connection lost")}, catalog, f.wallet, nil, f.repo.SearchOperations())
						if _, err := other.Search(ctx, SearchOperationRequest{UserID: f.userID, ProjectID: f.projectID, TaskID: f.taskID, ExecutionID: f.executionID, Request: WebSearchRequest{Query: "other unresolved request"}}); err == nil {
							t.Fatal("unknown second operation succeeded")
						}
					}
					if err := f.db.Model(&model.SearchOperation{}).Where("task_id = ?", f.taskID).Update("lease_expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
						t.Fatal(err)
					}
					if err := invoke(); !errors.Is(err, ErrSearchOperationInProgress) {
						t.Fatalf("expired operation replay=%v", err)
					}
					assets := NewContentAssetService(f.repo)
					if _, err := assets.SetReuseRunStatus(ctx, f.userID, run.ID, model.ContentReuseRunStatusQueued); !errors.Is(err, ErrInvalidReuseRunState) {
						t.Fatalf("unknown outcome allowed resume: %v", err)
					}
				}
				if fetch {
					svc := NewFetchService(cfg, reuseFetchProviderFunc(func() (*PageFetchResponse, error) {
						late()
						return &PageFetchResponse{ContentMarkdown: "durable content"}, nil
					}), catalog, f.wallet, nil, f.repo.SearchOperations())
					req := FetchOperationRequest{UserID: f.userID, ProjectID: f.projectID, TaskID: f.taskID, ExecutionID: f.executionID, Request: PageFetchRequest{URL: "https://example.com/page"}}
					invoke = func() error { _, err := svc.Fetch(ctx, req); return err }
				} else {
					svc := NewSearchService(cfg, reuseSearchProviderFunc(func() (*WebSearchResponse, error) {
						late()
						return &WebSearchResponse{Query: "content"}, nil
					}), catalog, f.wallet, nil, f.repo.SearchOperations())
					req := SearchOperationRequest{UserID: f.userID, ProjectID: f.projectID, TaskID: f.taskID, ExecutionID: f.executionID, Request: WebSearchRequest{Query: "content"}}
					invoke = func() error { _, err := svc.Search(ctx, req); return err }
				}
				if err := invoke(); err != nil {
					t.Fatal(err)
				}
				current, _ := f.repo.ContentAssets().FindReuseRunByID(ctx, run.ID)
				wantCode := ""
				if outstanding {
					wantCode = "operation_reconciliation_required"
				}
				if current.Status != model.ContentReuseRunStatusPaused || current.ErrorCode != wantCode {
					t.Fatalf("successful snapshot did not reconcile hold: %+v", current)
				}
				if err := invoke(); err != nil || calls != 1 {
					t.Fatalf("late successful operation repeated: calls=%d err=%v", calls, err)
				}
				assets := NewContentAssetService(f.repo)
				assets.SetEnqueuer(&contentAssetTestEnqueuer{})
				_, err := assets.SetReuseRunStatus(ctx, f.userID, run.ID, model.ContentReuseRunStatusQueued)
				if outstanding {
					if !errors.Is(err, ErrInvalidReuseRunState) {
						t.Fatalf("other unresolved operation allowed resume: %v", err)
					}
				} else if err != nil {
					t.Fatalf("late durable snapshot cannot resume: %v", err)
				}
			})
		}
	}
}

func (g *blockingReuseImageGenerator) GenerateImage(context.Context, string, string, string, string, string, string, []string, string, string, *ResolvedImageModel, *bool) (*ImageResult, error) {
	g.calls.Add(1)
	close(g.entered)
	<-g.release
	return nil, &appimage.GenerateError{Code: "rate_limit", Message: "provider rejected request"}
}

func TestReuseImageConcurrentReservationFailureReleaseAndSettlement(t *testing.T) {
	f := newTaskImageFixture(t)
	run := attachImageReuseRun(t, f, 600, 600)
	ctx := context.Background()
	g := &blockingReuseImageGenerator{entered: make(chan struct{}), release: make(chan struct{})}
	f.service.generator = g
	done := make(chan error, 1)
	go func() { _, err := f.service.Generate(ctx, f.request()); done <- err }()
	<-g.entered
	second := f.request()
	second.OutputPath = "output/second.png"
	if _, err := f.service.Generate(ctx, second); !errors.Is(err, repository.ErrContentReuseBudgetExceeded) {
		t.Fatalf("concurrent over-budget operation=%v", err)
	}
	if _, err := f.service.Generate(ctx, f.request()); !errors.Is(err, repository.ErrContentReuseOperationInProgress) {
		t.Fatalf("concurrent replay=%v", err)
	}
	close(g.release)
	if err := <-done; err == nil {
		t.Fatal("provider failure lost")
	}
	var pending int64
	if err := f.db.Model(&model.ContentReuseOperationBudget{}).Count(&pending).Error; err != nil || pending != 0 {
		t.Fatalf("failed provider reservation count=%d err=%v", pending, err)
	}
	f.service.generator = f.generator
	if _, err := f.service.Generate(ctx, f.request()); err != nil {
		t.Fatal(err)
	}
	// Pausing cannot prevent payment for a provider operation already admitted.
	if err := f.db.Model(&model.ContentReuseRun{}).Where("id = ?", run.ID).Update("status", model.ContentReuseRunStatusPaused).Error; err != nil {
		t.Fatal(err)
	}
	if processed, err := f.wallet.ProcessSettlementOutbox(ctx, 10); err != nil || processed != 1 {
		t.Fatalf("settlement=%d err=%v", processed, err)
	}
	current, err := f.repo.ContentAssets().FindReuseRunByID(ctx, run.ID)
	if err != nil || current.CostCredits != 600 || current.BudgetReserved != 0 {
		var settlements []model.BillingSettlementOutbox
		f.db.Find(&settlements)
		t.Fatalf("settled run=%+v err=%v outbox=%+v", current, err, settlements)
	}
	if _, err := f.service.Generate(ctx, f.request()); err != nil {
		t.Fatalf("settled replay=%v", err)
	}
	if f.generator.calls != 1 {
		t.Fatalf("provider replay calls=%d", f.generator.calls)
	}
}

func TestReusePaidSearchAndFetchRejectBeforeProvider(t *testing.T) {
	for _, fetch := range []bool{false, true} {
		t.Run(fmt.Sprint("fetch=", fetch), func(t *testing.T) {
			f := newTaskImageFixture(t)
			attachImageReuseRun(t, f, 100, 100)
			bundle := f.wallet.bundle
			bundle.Products.CatalogID += "-search"
			bundle.Products.SKUs = append(bundle.Products.SKUs, billingPaidSearchSKUs()...)
			catalog := NewBillingCatalogService(f.repo, &bundle, BillingCatalogOptions{})
			if _, err := catalog.Publish(context.Background()); err != nil {
				t.Fatal(err)
			}
			cfg := &serverconfig.SearchConfig{}
			cfg.Web.Enabled, cfg.Web.Provider = true, "doubao_custom"
			cfg.Fetch.Enabled, cfg.Fetch.Provider = true, "deepseek"
			searchProvider := &searchProviderStub{}
			fetchProvider := &fetchProviderStub{response: &PageFetchResponse{ContentMarkdown: "content"}}
			var err error
			if fetch {
				_, err = NewFetchService(cfg, fetchProvider, catalog, f.wallet, nil, nil).Fetch(context.Background(), FetchOperationRequest{UserID: f.userID, ProjectID: f.projectID, TaskID: f.taskID, ExecutionID: f.executionID, Request: PageFetchRequest{URL: "https://example.com/page"}})
			} else {
				_, err = NewSearchService(cfg, searchProvider, catalog, f.wallet, nil, nil).Search(context.Background(), SearchOperationRequest{UserID: f.userID, ProjectID: f.projectID, TaskID: f.taskID, ExecutionID: f.executionID, Request: WebSearchRequest{Query: "content"}})
			}
			if !errors.Is(err, repository.ErrContentReuseBudgetExceeded) || searchProvider.calls != 0 || fetchProvider.calls != 0 {
				t.Fatalf("provider escaped budget: err=%v search=%d fetch=%d", err, searchProvider.calls, fetchProvider.calls)
			}
		})
	}
}

func TestReuseOperationBudgetUsesInvocationDay(t *testing.T) {
	f := newTaskImageFixture(t)
	run := attachImageReuseRun(t, f, 1000, 600)
	yesterday := time.Now().UTC().Add(-24 * time.Hour).Truncate(24 * time.Hour)
	if err := f.db.Model(&model.ContentReuseRun{}).Where("id = ?", run.ID).Update("budget_date", yesterday).Error; err != nil {
		t.Fatal(err)
	}
	other := model.NewContentReuseRun(f.userID, "asset", f.projectID, model.PlatformSeednote)
	other.Status, other.CostCredits = model.ContentReuseRunStatusCompleted, 500
	now := time.Now().UTC().Truncate(24 * time.Hour)
	other.BudgetDate = &now
	if err := f.repo.ContentAssets().CreateReuseRun(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Generate(context.Background(), f.request()); !errors.Is(err, repository.ErrContentReuseBudgetExceeded) {
		t.Fatalf("today's operation escaped today's cap: %v", err)
	}
	if f.generator.calls != 0 {
		t.Fatal("provider called despite exhausted invocation-day budget")
	}
}

func TestReuseImagePersistenceFailurePausesWithoutRepeatedProviderSpend(t *testing.T) {
	f := newTaskImageFixture(t)
	run := attachImageReuseRun(t, f, 1000, 1000)
	if err := f.db.Exec(`CREATE TRIGGER fail_image_persist BEFORE INSERT ON task_files BEGIN SELECT RAISE(ABORT, 'storage metadata unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Generate(context.Background(), f.request()); err == nil {
		t.Fatal("expected persistence failure")
	}
	current, err := f.repo.ContentAssets().FindReuseRunByID(context.Background(), run.ID)
	if err != nil || current.Status != model.ContentReuseRunStatusPaused || current.ErrorCode != "operation_reconciliation_required" {
		t.Fatalf("ambiguous operation run=%+v err=%v", current, err)
	}
	if _, err := f.service.Generate(context.Background(), f.request()); err == nil {
		t.Fatal("ambiguous provider operation was repeated")
	}
	if f.generator.calls != 1 {
		t.Fatalf("provider invoked %d times", f.generator.calls)
	}
}

func TestReusePaidSearchFailureReversesAndRetryChargesExactlyOnce(t *testing.T) {
	f := newTaskImageFixture(t)
	run := attachImageReuseRun(t, f, 400, 400)
	bundle := f.wallet.bundle
	bundle.Products.CatalogID += "-search-retry"
	bundle.Products.SKUs = append(bundle.Products.SKUs, billingPaidSearchSKUs()...)
	bundle.Policy.TaskFailureReversal = billing.TaskFailureReversalPolicy{Enabled: true, Reasons: []string{"provider_error"}}
	f.wallet.bundle = bundle
	catalog := NewBillingCatalogService(f.repo, &bundle, BillingCatalogOptions{})
	if _, err := catalog.Publish(context.Background()); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ResponseMetadata":{"RequestId":"ok"},"Result":{"ResultCount":1,"WebResults":[{"Title":"result","Url":"https://example.com"}]}}`)
	}))
	defer provider.Close()
	cfg := &serverconfig.SearchConfig{}
	cfg.Web.Enabled, cfg.Web.Provider = true, "doubao_custom"
	svc := NewSearchService(cfg, NewDoubaoCustomSearchProvider(serverconfig.SearchDoubaoConfig{CustomEndpoint: provider.URL, APIKey: "fixture", Timeout: time.Second}), catalog, f.wallet, nil, f.repo.SearchOperations())
	req := SearchOperationRequest{UserID: f.userID, ProjectID: f.projectID, TaskID: f.taskID, ExecutionID: f.executionID, Request: WebSearchRequest{Query: "content"}}
	if _, err := svc.Search(context.Background(), req); err == nil {
		t.Fatal("provider rejection lost")
	}
	account, err := f.repo.Billing().FindAccount(context.Background(), f.userID)
	if err != nil || account.PaidCredits != 1000 {
		t.Fatalf("rejection not refunded: account=%+v err=%v", account, err)
	}
	for i := 0; i < 2; i++ {
		if _, err := svc.Search(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	}
	account, _ = f.repo.Billing().FindAccount(context.Background(), f.userID)
	current, _ := f.repo.ContentAssets().FindReuseRunByID(context.Background(), run.ID)
	if account.PaidCredits != 700 || calls.Load() != 2 || current.CostCredits != 400 {
		t.Fatalf("retry account=%+v calls=%d run=%+v", account, calls.Load(), current)
	}
}

func TestReuseImageBudgetRejectsBeforeProvider(t *testing.T) {
	for _, daily := range []bool{false, true} {
		t.Run(fmt.Sprint("daily=", daily), func(t *testing.T) {
			f := newTaskImageFixture(t)
			ctx := context.Background()
			run := model.NewContentReuseRun(f.userID, "asset", f.projectID, model.PlatformSeednote)
			run.Status, run.TaskID, run.BudgetReserved = model.ContentReuseRunStatusRunning, f.taskID, 100
			limit := int64(500)
			if daily {
				limit = 1000
			}
			run.PolicySnapshot = mustContentAssetJSON(map[string]any{"enabled": true, "max_run_cost_credits": limit, "daily_budget_credits": 500})
			if err := f.repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
				t.Fatal(err)
			}
			if err := f.db.Model(&model.Task{}).Where("id = ?", f.taskID).Updates(map[string]any{"reuse_run_id": run.ID, "billing_price_credits": 100}).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := f.service.Generate(ctx, f.request()); err == nil {
				t.Fatal("over-budget image was accepted")
			}
			if f.generator.calls != 0 {
				t.Fatalf("provider invoked %d times before budget rejection", f.generator.calls)
			}
		})
	}
}

func TestReusePausedCompletionResumesServerDraftFinalization(t *testing.T) {
	for _, publish := range []bool{false, true} {
		t.Run(fmt.Sprint("publish=", publish), func(t *testing.T) {
			svc, repo, db, task, execution := setupCloudCompletionTestWithDB(t, true)
			if err := model.AutoMigrate(db); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			asset := model.NewContentAsset(task.UserID, "reusable topic")
			if err := repo.ContentAssets().Create(ctx, asset); err != nil {
				t.Fatal(err)
			}
			run := model.NewContentReuseRun(task.UserID, asset.ID, task.ProjectID, model.PlatformWechat)
			run.TaskID, run.Status = task.ID, model.ContentReuseRunStatusPaused
			run.PolicySnapshot = mustContentAssetJSON(map[string]any{"draft_enabled": true, "publish_enabled": publish, "publish_windows": []map[string]string{{"timezone": "UTC", "start": "00:00", "end": "23:59"}}})
			if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
				t.Fatal(err)
			}
			task.ReuseRunID, task.ContentAssetID = run.ID, run.AssetID
			if err := repo.Tasks().Update(ctx, task); err != nil {
				t.Fatal(err)
			}
			report := completeReuseReportFixture(t, fmt.Sprintf(`{"schema_version":1,"status":"ready","reuse_run_id":%q,"asset_id":%q,"task_id":%q,"target_platform":"wechat","checks":[{"id":"fact_sources","status":"passed"},{"id":"core_consistency","status":"passed"},{"id":"platform_fit","status":"passed"},{"id":"compliance","status":"passed"},{"id":"material_rights","status":"passed"}]}`, run.ID, run.AssetID, task.ID), task)
			for _, name := range []string{"content-reuse-brief.json", "content-lineage.json", "quality-report.json"} {
				addCloudOutcomeArtifact(t, svc, repo, task, execution, "output/"+name, "application/json", []byte(report))
			}
			addCloudOutcomeArtifact(t, svc, repo, task, execution, "output/topic-analysis.md", "text/markdown", []byte("# Evidence\nInternal source checked.\n"))
			if err := repo.TaskFiles().ReplacePendingCurrentExecutionPreservingMCPArtifacts(ctx, task.ID, execution.ID, nil); err != nil {
				t.Fatal(err)
			}
			api := &fakeWechatPublicationAPI{addResponse: &appwechat.DraftAddResponse{MediaID: "resumed-draft"}, draftListResponse: &appwechat.DraftBatchGetResponse{Items: []appwechat.DraftBatchItem{{MediaID: "resumed-draft", UpdateTime: time.Now().Unix()}}}, publishedListResponse: &appwechat.FreePublishBatchGetResponse{}, submitResponse: &appwechat.FreePublishSubmitResponse{PublishID: "formal-publish"}}
			logger := zerolog.New(io.Discard)
			publicationSvc := NewWechatPublicationService(repo, func(*model.Project) (WechatPublicationAPI, error) { return api, nil }, &logger)
			publicationSvc.SetEnqueuer(&recordingWechatPublicationEnqueuer{})
			svc.SetWechatPublicationService(publicationSvc)
			assets := NewContentAssetService(repo)
			assets.SetTaskService(svc)
			assets.SetEnqueuer(&contentAssetTestEnqueuer{})
			if err := svc.CompleteCloudExecution(ctx, execution.ID, &agent.ExecutionResult{Success: true, RemoteArtifacts: true}); err != nil {
				t.Fatal(err)
			}
			if api.addCalls != 0 {
				t.Fatal("paused run created a draft")
			}
			task, _ = repo.Tasks().FindByID(ctx, task.ID)
			if task.Outcome == nil || task.Outcome.Publication.Action != "resume_reuse_run" || task.Outcome.Publication.Code != "content_reuse_policy_unavailable" {
				t.Fatalf("resume action missing: status=%s error=%s outcome=%+v", task.Status, task.ErrorMessage, task.Outcome)
			}
			if _, err := assets.SetReuseRunStatus(ctx, task.UserID, run.ID, model.ContentReuseRunStatusQueued); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if err := assets.ProcessReuseRun(ctx, run.ID); err != nil {
					t.Fatal(err)
				}
			}
			if api.addCalls != 1 {
				t.Fatalf("resume draft calls=%d, want 1", api.addCalls)
			}
			if publish {
				if api.submitCalls != 1 {
					t.Fatalf("formal publish calls=%d, want 1", api.submitCalls)
				}
				publication, err := repo.WechatPublications().FindByTaskID(ctx, task.ID)
				if err != nil {
					t.Fatal(err)
				}
				publication.Status = model.WechatPublicationStatusPublished
				if err := repo.WechatPublications().Update(ctx, publication); err != nil {
					t.Fatal(err)
				}
				task, _ = repo.Tasks().FindByID(ctx, task.ID)
				if err := assets.FinalizeReuseTask(ctx, task); err != nil {
					t.Fatal(err)
				}
			} else if api.submitCalls != 0 {
				t.Fatalf("draft-only run formally published %d times", api.submitCalls)
			}
			current, err := repo.ContentAssets().FindReuseRunByID(ctx, run.ID)
			if err != nil || current.Status != model.ContentReuseRunStatusCompleted {
				t.Fatalf("resumed run=%+v err=%v", current, err)
			}
		})
	}
}

func TestAutopilotTickTraversesMoreThanPolicyBatch(t *testing.T) {
	db := setupTaskTestDB(t)
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	now := time.Now().UTC()
	for i := 0; i < 103; i++ {
		userID, projectID := fmt.Sprintf("owner-%03d", i), fmt.Sprintf("project-%03d", i)
		if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Name: "target", Platform: model.PlatformWechat}); err != nil {
			t.Fatal(err)
		}
		asset := model.NewContentAsset(userID, "validated")
		asset.Status = model.ContentAssetStatusValidated
		if err := repo.ContentAssets().Create(ctx, asset); err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.AnalyticsState{ProjectID: projectID, Revision: 1, ActiveGeneration: 1, Status: "ready"}).Error; err != nil {
			t.Fatal(err)
		}
		policy := model.NewContentAutopilotPolicy(userID)
		policy.Enabled = true
		policy.ProjectIDs, _ = json.Marshal([]string{projectID})
		policy.Platforms = []byte(`["wechat"]`)
		if err := repo.ContentAssets().UpsertPolicy(ctx, policy); err != nil {
			t.Fatal(err)
		}
		if err := repo.ContentAssets().CreateScoreSnapshot(ctx, &model.ContentAssetScoreSnapshot{ID: fmt.Sprintf("score-%d", i), UserID: userID, AssetID: asset.ID, ProjectID: projectID, Platform: model.PlatformWechat, AnalyticsRevision: 1, Status: "scored", Score: 99, Confidence: "high", DataAsOf: &now, Snapshot: []byte(`{"schema_version":1}`), CreatedAt: now.Add(time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewContentAssetService(repo)
	queue := &contentAssetTestEnqueuer{}
	svc.SetEnqueuer(queue)
	for tick := 0; tick < 2; tick++ {
		created, err := svc.RunAutopilotTick(ctx, now.Add(time.Minute))
		want := 103
		if tick == 1 {
			want = 0
		}
		if err != nil || created != want {
			t.Fatalf("tick %d created=%d want=%d err=%v", tick, created, want, err)
		}
	}
	if queue.calls != 103 {
		t.Fatalf("queue calls=%d", queue.calls)
	}
}
