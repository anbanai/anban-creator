package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/billing"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const billingCatalogUserID = "40000000-0000-4000-8000-000000000001"

func TestBillingCatalogCanonicalRequestValidationPrecedesSQL(t *testing.T) {
	bundle := testBillingBundle()
	validQuote := QuoteRequest{
		UserID: billingCatalogUserID, CatalogID: bundle.Products.CatalogID, Operation: "task.article",
		ExecutionProfile:   "balanced",
		RequestFingerprint: billingFingerprint("canonical-quote"), IdempotencyScope: "quote", IdempotencyKey: "canonical-quote",
	}
	quoteCases := []struct {
		name   string
		mutate func(*QuoteRequest)
	}{
		{name: "invalid user UUID", mutate: func(req *QuoteRequest) { req.UserID = "not-a-uuid" }},
		{name: "empty operation", mutate: func(req *QuoteRequest) { req.Operation = " " }},
		{name: "operation too long", mutate: func(req *QuoteRequest) { req.Operation = strings.Repeat("o", 129) }},
		{name: "route too long", mutate: func(req *QuoteRequest) { req.Route = strings.Repeat("r", 129) }},
		{name: "catalog too long", mutate: func(req *QuoteRequest) { req.CatalogID = strings.Repeat("c", 129) }},
		{name: "scope too long", mutate: func(req *QuoteRequest) { req.IdempotencyScope = strings.Repeat("s", 81) }},
		{name: "key too long", mutate: func(req *QuoteRequest) { req.IdempotencyKey = strings.Repeat("k", 129) }},
		{name: "invalid fingerprint", mutate: func(req *QuoteRequest) { req.RequestFingerprint = "not-a-fingerprint" }},
	}
	for _, tt := range quoteCases {
		t.Run("quote "+tt.name, func(t *testing.T) {
			guard := &billingCatalogAccessGuard{Repository: newBillingServiceRepository(t)}
			svc := NewBillingCatalogService(guard, &bundle, BillingCatalogOptions{})
			req := validQuote
			tt.mutate(&req)
			if _, err := svc.CreateQuote(context.Background(), req); !errors.Is(err, ErrBillingInvalid) {
				t.Fatalf("CreateQuote error = %v, want ErrBillingInvalid", err)
			}
			if guard.calls != 0 {
				t.Fatalf("invalid quote made %d billing repository calls", guard.calls)
			}
		})
	}

	resolveCases := []struct {
		name      string
		catalogID string
		operation string
		route     string
	}{
		{name: "empty operation", catalogID: bundle.Products.CatalogID, operation: " "},
		{name: "operation too long", catalogID: bundle.Products.CatalogID, operation: strings.Repeat("o", 129)},
		{name: "route too long", catalogID: bundle.Products.CatalogID, operation: "task.article", route: strings.Repeat("r", 129)},
		{name: "catalog too long", catalogID: strings.Repeat("c", 129), operation: "task.article"},
	}
	for _, tt := range resolveCases {
		t.Run("resolve "+tt.name, func(t *testing.T) {
			guard := &billingCatalogAccessGuard{Repository: newBillingServiceRepository(t)}
			svc := NewBillingCatalogService(guard, &bundle, BillingCatalogOptions{})
			if _, err := svc.ResolveSKU(context.Background(), tt.catalogID, tt.operation, tt.route); !errors.Is(err, ErrBillingInvalid) {
				t.Fatalf("ResolveSKU error = %v, want ErrBillingInvalid", err)
			}
			if guard.calls != 0 {
				t.Fatalf("invalid SKU resolution made %d billing repository calls", guard.calls)
			}
		})
	}
}

func TestBillingCatalogCanonicalQuoteTrimsPersistedIdentity(t *testing.T) {
	repo := newBillingServiceRepository(t)
	bundle := testBillingBundle()
	svc := NewBillingCatalogService(repo, &bundle, BillingCatalogOptions{})
	if _, err := svc.Publish(context.Background()); err != nil {
		t.Fatal(err)
	}
	quote, err := svc.CreateQuote(context.Background(), QuoteRequest{
		UserID: "  " + billingCatalogUserID + "  ", CatalogID: "  " + bundle.Products.CatalogID + "  ",
		Operation: "  task.article  ", ExecutionProfile: "  balanced  ", Route: "  ", RequestFingerprint: "  " + billingFingerprint("trimmed") + "  ",
		IdempotencyScope: "  quote  ", IdempotencyKey: "  trimmed  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if quote.UserID != billingCatalogUserID || quote.CatalogID != bundle.Products.CatalogID ||
		quote.IdempotencyScope != "quote" || quote.IdempotencyKey != "trimmed" || quote.RequestFingerprint != billingFingerprint("trimmed") {
		t.Fatalf("canonical quote = %+v", quote)
	}
}

func TestBillingCatalogPublishesImmutableSnapshot(t *testing.T) {
	ctx := context.Background()
	repo := newBillingServiceRepository(t)
	bundle := testBillingBundle()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	svc := NewBillingCatalogService(repo, &bundle, BillingCatalogOptions{Now: func() time.Time { return now }})

	first, err := svc.Publish(ctx)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	second, err := svc.Publish(ctx)
	if err != nil || second.CatalogID != first.CatalogID || string(second.Snapshot) != string(first.Snapshot) {
		t.Fatalf("idempotent Publish = %+v, %v; first = %+v", second, err, first)
	}
	var publishedSnapshot struct {
		Economics billing.EconomicsConfig `json:"economics"`
		Policy    struct {
			TaskFailureReversal billing.TaskFailureReversalPolicy `json:"task_failure_reversal"`
		} `json:"policy"`
	}
	if err := json.Unmarshal(first.Snapshot, &publishedSnapshot); err != nil {
		t.Fatalf("decode published catalog snapshot: %v", err)
	}
	if publishedSnapshot.Economics.CreditsPerCNY != bundle.Economics.CreditsPerCNY {
		t.Fatalf("published credits_per_cny = %d, want %d", publishedSnapshot.Economics.CreditsPerCNY, bundle.Economics.CreditsPerCNY)
	}
	if !publishedSnapshot.Policy.TaskFailureReversal.Enabled || len(publishedSnapshot.Policy.TaskFailureReversal.Reasons) != len(bundle.Policy.TaskFailureReversal.Reasons) {
		t.Fatalf("published reversal policy = %#v", publishedSnapshot.Policy.TaskFailureReversal)
	}

	conflicting := bundle
	conflicting.Products.SKUs = append([]billing.SKUConfig(nil), bundle.Products.SKUs...)
	conflicting.Products.SKUs[0].PriceCredits++
	_, err = NewBillingCatalogService(repo, &conflicting, BillingCatalogOptions{Now: func() time.Time { return now }}).Publish(ctx)
	if !errors.Is(err, ErrBillingConflict) {
		t.Fatalf("conflicting Publish error = %v, want ErrBillingConflict", err)
	}

	persisted, err := repo.Billing().FindSKU(ctx, bundle.Products.CatalogID, bundle.Products.SKUs[0].ID)
	if err != nil || persisted.PriceCredits != bundle.Products.SKUs[0].PriceCredits {
		t.Fatalf("immutable persisted SKU = %+v, %v", persisted, err)
	}
}

func TestPublishedRetailSnapshotMatchesContentAddress(t *testing.T) {
	bundle, err := billing.LoadBundle(filepath.Join("..", "billing"))
	if err != nil {
		t.Fatal(err)
	}
	repo := newBillingServiceRepository(t)
	published, err := NewBillingCatalogService(repo, bundle, BillingCatalogOptions{}).Publish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(published.Snapshot)
	if want := "retail-sha256-" + fmt.Sprintf("%x", sum); published.CatalogID != want {
		t.Fatalf("published catalog ID = %q, want snapshot hash %q", published.CatalogID, want)
	}
}

func TestBillingCatalogParsesProductionAndResolvesExactRoute(t *testing.T) {
	bundle, err := billing.LoadBundle(filepath.Join("..", "billing"))
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	repo := newBillingServiceRepository(t)
	svc := NewBillingCatalogService(repo, bundle, BillingCatalogOptions{})
	if _, err := svc.Publish(context.Background()); err != nil {
		t.Fatalf("Publish production catalog: %v", err)
	}

	taskSKU, err := svc.ResolveSKUForExecutionProfile(context.Background(), bundle.Products.CatalogID, "task.article", "balanced")
	if err != nil || taskSKU.SKUID != "task.article.balanced" {
		t.Fatalf("ResolveSKUForExecutionProfile task balanced = %+v, %v", taskSKU, err)
	}
	if _, err := svc.ResolveSKU(context.Background(), bundle.Products.CatalogID, "task.article", ""); !errors.Is(err, ErrBillingSKUNotFound) {
		t.Fatalf("route resolution matched task profile SKU: %v", err)
	}
	cover, err := svc.ResolveSKU(context.Background(), bundle.Products.CatalogID, "mcp.generate_image", "image_generation.cover")
	if err != nil || cover.SKUID != "image.seedream.cover" {
		t.Fatalf("ResolveSKU cover = %+v, %v", cover, err)
	}
	if _, err := svc.ResolveSKU(context.Background(), bundle.Products.CatalogID, "mcp.generate_image", ""); !errors.Is(err, ErrBillingSKUNotFound) {
		t.Fatalf("empty route fallback error = %v, want ErrBillingSKUNotFound", err)
	}
	if _, err := svc.ResolveSKU(context.Background(), bundle.Products.CatalogID, "missing", ""); !errors.Is(err, ErrBillingSKUNotFound) {
		t.Fatalf("unknown operation error = %v, want ErrBillingSKUNotFound", err)
	}
}

func TestBillingCatalogResolvesPriceBySKUIdentity(t *testing.T) {
	ctx := context.Background()
	bundle, err := billing.LoadBundle(filepath.Join("..", "billing"))
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	repo := newBillingServiceRepository(t)
	svc := NewBillingCatalogService(repo, bundle, BillingCatalogOptions{})
	if _, err := svc.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	price, err := svc.ResolvePriceBySKUID(ctx, bundle.Products.CatalogID, "image.seedream.designer", model.TierFree)
	if err != nil {
		t.Fatalf("ResolvePriceBySKUID: %v", err)
	}
	if price.SKU == nil || price.SKU.SKUID != "image.seedream.designer" || price.PriceCredits <= 0 {
		t.Fatalf("price = %+v, want positive price for exact SKU", price)
	}
	if _, err := svc.ResolvePriceBySKUID(ctx, bundle.Products.CatalogID, "missing-capability", model.TierFree); !errors.Is(err, ErrBillingSKUNotFound) {
		t.Fatalf("missing SKU error = %v, want ErrBillingSKUNotFound", err)
	}
}

func TestBillingCatalogQuoteSeparatesAgentProfilesFromOperationRoutes(t *testing.T) {
	ctx := context.Background()
	repo := newBillingServiceRepository(t)
	bundle, err := billing.LoadBundle(filepath.Join("..", "billing"))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewBillingCatalogService(repo, bundle, BillingCatalogOptions{})
	if _, err := svc.Publish(ctx); err != nil {
		t.Fatal(err)
	}

	base := QuoteRequest{
		UserID: billingCatalogUserID, RequestFingerprint: billingFingerprint("profile-contract"),
		IdempotencyScope: "profile-contract", IdempotencyKey: "profile-contract",
	}
	tests := []struct {
		name   string
		mutate func(*QuoteRequest)
	}{
		{name: "agent task requires profile", mutate: func(req *QuoteRequest) { req.Operation = "task.article" }},
		{name: "agent task rejects route", mutate: func(req *QuoteRequest) {
			req.Operation = "task.article"
			req.ExecutionProfile = "balanced"
			req.Route = "legacy"
		}},
		{name: "operation rejects profile", mutate: func(req *QuoteRequest) {
			req.Operation = "mcp.generate_image"
			req.ExecutionProfile = "balanced"
			req.Route = "image_generation.cover"
		}},
		{name: "designer rejects profile", mutate: func(req *QuoteRequest) {
			req.Operation = "designer.generate_image"
			req.ExecutionProfile = "balanced"
			req.Route = "image_generation.designer.seedream"
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := base
			req.IdempotencyKey = strings.ReplaceAll(tt.name, " ", "-")
			tt.mutate(&req)
			if _, err := svc.CreateQuote(ctx, req); !errors.Is(err, ErrBillingInvalid) {
				t.Fatalf("CreateQuote error = %v, want ErrBillingInvalid", err)
			}
		})
	}

	valid := base
	valid.Operation = "task.article"
	valid.ExecutionProfile = "balanced"
	valid.IdempotencyKey = "valid-balanced"
	quote, err := svc.CreateQuote(ctx, valid)
	if err != nil {
		t.Fatalf("CreateQuote balanced: %v", err)
	}
	var frozen billing.SKUConfig
	if err := json.Unmarshal(quote.SKUSnapshot, &frozen); err != nil {
		t.Fatal(err)
	}
	if frozen.ExecutionProfile != "balanced" || frozen.ID != "task.article.balanced" {
		t.Fatalf("frozen SKU = %#v", frozen)
	}
}

func TestCreateTaskQuoteRequiresAProfiledCurrentCatalogSKU(t *testing.T) {
	ctx := context.Background()
	repo := newBillingServiceRepository(t)
	oldBundle := testBillingBundle()
	oldBundle.Products.CatalogID = "retail-test-v4"
	oldBundle.Products.SKUs[0].ExecutionProfile = ""
	if _, err := NewBillingCatalogService(repo, &oldBundle, BillingCatalogOptions{Now: func() time.Time { return time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC) }}).Publish(ctx); err != nil {
		t.Fatal(err)
	}
	svc := NewBillingCatalogService(repo, &oldBundle, BillingCatalogOptions{})
	registry, err := NewAgentProfileRegistry(testAgentProfiles())
	if err != nil {
		t.Fatal(err)
	}
	svc.SetAgentProfileRegistry(registry)
	request := TaskQuoteRequest{
		UserID: billingCatalogUserID, TaskType: "article", RequestFingerprint: billingFingerprint("public-agent-task-quote"),
		IdempotencyScope: "public-agent-task-quote", IdempotencyKey: "public-agent-task-quote",
	}
	if _, err := svc.CreateTaskQuote(ctx, request); !errors.Is(err, ErrBillingInvalid) {
		t.Fatalf("profileless task quote error = %v, want ErrBillingInvalid", err)
	}

	currentBundle := testBillingBundle()
	currentBundle.Products.CatalogID = "retail-test-v5"
	if _, err := NewBillingCatalogService(repo, &currentBundle, BillingCatalogOptions{Now: func() time.Time { return time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC) }}).Publish(ctx); err != nil {
		t.Fatal(err)
	}
	user, err := repo.Users().FindByID(ctx, billingCatalogUserID)
	if err != nil {
		t.Fatal(err)
	}
	user.Tier = model.TierPro
	if err := repo.Users().Update(ctx, user); err != nil {
		t.Fatal(err)
	}
	if err := repo.Billing().CreateAccount(ctx, &model.BillingWalletAccount{UserID: billingCatalogUserID, PaidCredits: 500}); err != nil {
		t.Fatal(err)
	}
	request.ExecutionProfile = "balanced"
	quote, err := svc.CreateTaskQuote(ctx, request)
	if err != nil || quote.CatalogID != currentBundle.Products.CatalogID || quote.SKUID != "task.article.v1" {
		t.Fatalf("profiled public task quote = %#v, %v", quote, err)
	}
}

func TestCreateTaskQuoteViralAnalysisRequiresExactExecutionProfileSKU(t *testing.T) {
	ctx := context.Background()
	repo := newBillingServiceRepository(t)
	bundle, err := billing.LoadBundle(filepath.Join("..", "billing"))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewBillingCatalogService(repo, bundle, BillingCatalogOptions{})
	registry, err := NewAgentProfileRegistry(testAgentProfiles())
	if err != nil {
		t.Fatal(err)
	}
	svc.SetAgentProfileRegistry(registry)
	if _, err := svc.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	user, err := repo.Users().FindByID(ctx, billingCatalogUserID)
	if err != nil {
		t.Fatal(err)
	}
	user.Tier = model.TierEnterprise
	if err := repo.Users().Update(ctx, user); err != nil {
		t.Fatal(err)
	}
	if err := repo.Billing().CreateAccount(ctx, &model.BillingWalletAccount{UserID: billingCatalogUserID, PaidCredits: 10_000}); err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		profile string
		skuID   string
	}{
		{profile: "effective", skuID: "task.viral-analysis.effective"},
		{profile: "balanced", skuID: "task.viral-analysis.balanced"},
		{profile: "quality", skuID: "task.viral-analysis.quality"},
	} {
		t.Run(tt.profile, func(t *testing.T) {
			quote, err := svc.CreateTaskQuote(ctx, TaskQuoteRequest{
				UserID: billingCatalogUserID, TaskType: model.TaskTypeViralAnalysis, ExecutionProfile: tt.profile,
				RequestFingerprint: billingFingerprint("viral-" + tt.profile),
				IdempotencyScope:   "viral-quote", IdempotencyKey: tt.profile,
			})
			if err != nil {
				t.Fatalf("CreateTaskQuote: %v", err)
			}
			if quote.SKUID != tt.skuID {
				t.Fatalf("SKU = %q, want %q", quote.SKUID, tt.skuID)
			}
		})
	}
	_, err = svc.CreateTaskQuote(ctx, TaskQuoteRequest{
		UserID: billingCatalogUserID, TaskType: model.TaskTypeViralAnalysis,
		RequestFingerprint: billingFingerprint("viral-empty-profile"),
		IdempotencyScope:   "viral-quote", IdempotencyKey: "empty-profile",
	})
	if !errors.Is(err, ErrBillingInvalid) {
		t.Fatalf("empty execution profile error = %v, want ErrBillingInvalid", err)
	}
}

func TestCreateTaskQuoteEnforcesProfileAndWalletAdmissionBeforePersisting(t *testing.T) {
	ctx := context.Background()
	repo := newBillingServiceRepository(t)
	bundle := testBillingBundle()
	svc := NewBillingCatalogService(repo, &bundle, BillingCatalogOptions{})
	registry, err := NewAgentProfileRegistry(testAgentProfiles())
	if err != nil {
		t.Fatal(err)
	}
	svc.SetAgentProfileRegistry(registry)
	if _, err := svc.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	request := TaskQuoteRequest{
		UserID: billingCatalogUserID, TaskType: "article", ExecutionProfile: "balanced",
		RequestFingerprint: billingFingerprint("admission"), IdempotencyScope: "quote", IdempotencyKey: "admission",
	}

	if _, err := svc.CreateTaskQuote(ctx, request); !errors.Is(err, ErrAgentProfileAccessDenied) {
		t.Fatalf("free balanced quote error = %v, want ErrAgentProfileAccessDenied", err)
	}
	user, err := repo.Users().FindByID(ctx, billingCatalogUserID)
	if err != nil {
		t.Fatal(err)
	}
	user.Tier = model.TierPro
	if err := repo.Users().Update(ctx, user); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateTaskQuote(ctx, request); !errors.Is(err, ErrBillingLedgerInvalid) {
		t.Fatalf("missing wallet quote error = %v, want ErrBillingLedgerInvalid", err)
	}
	if err := repo.Billing().CreateAccount(ctx, &model.BillingWalletAccount{
		UserID: billingCatalogUserID, PaidCredits: 1_000, DebtCredits: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateTaskQuote(ctx, request); !errors.Is(err, ErrBillingDebtOutstanding) {
		t.Fatalf("debt quote error = %v, want ErrBillingDebtOutstanding", err)
	}
	account, err := repo.Billing().FindAccount(ctx, billingCatalogUserID)
	if err != nil {
		t.Fatal(err)
	}
	account.DebtCredits = 0
	account.PaidCredits = 449
	expectedVersion := account.Version
	account.Version++
	if err := repo.Billing().UpdateAccount(ctx, account, expectedVersion); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateTaskQuote(ctx, request); !errors.Is(err, ErrBillingInsufficientForTask) {
		t.Fatalf("insufficient quote error = %v, want ErrBillingInsufficientForTask", err)
	}
	account.PaidCredits = 450
	expectedVersion = account.Version
	account.Version++
	if err := repo.Billing().UpdateAccount(ctx, account, expectedVersion); err != nil {
		t.Fatal(err)
	}
	quote, err := svc.CreateTaskQuote(ctx, request)
	if err != nil || quote == nil {
		t.Fatalf("admitted quote = %#v, %v", quote, err)
	}
	var profileSnapshot model.AgentProfileSnapshot
	if err := json.Unmarshal(quote.AgentProfileSnapshot, &profileSnapshot); err != nil {
		t.Fatalf("decode quote profile snapshot: %v", err)
	}
	if profileSnapshot.ProfileID != "balanced" || profileSnapshot.Provider != "volcengine_ark" || profileSnapshot.Envs[model.ClaudeEnvModel] != "doubao-seed-evolving" || profileSnapshot.Protocol != "anthropic" {
		t.Fatalf("quote profile snapshot = %#v", profileSnapshot)
	}

	// Exact idempotent replay is durable evidence. Later entitlement, wallet,
	// provider availability, or catalog changes must not rewrite its outcome.
	user.Tier = model.TierFree
	if err := repo.Users().Update(ctx, user); err != nil {
		t.Fatal(err)
	}
	account.PaidCredits = 0
	account.DebtCredits = 1
	expectedVersion = account.Version
	account.Version++
	if err := repo.Billing().UpdateAccount(ctx, account, expectedVersion); err != nil {
		t.Fatal(err)
	}
	unavailableProfiles := testAgentProfiles()
	unavailableProfiles[1].Available = false
	unavailableProfiles[1].UnavailableReason = "provider_configuration_missing"
	unavailableRegistry, err := NewAgentProfileRegistry(unavailableProfiles)
	if err != nil {
		t.Fatal(err)
	}
	svc.SetAgentProfileRegistry(unavailableRegistry)
	nextBundle := bundle
	nextBundle.Products.CatalogID = "retail-test-v2"
	nextBundle.Products.SKUs = append([]billing.SKUConfig(nil), bundle.Products.SKUs...)
	nextBundle.Products.SKUs[0].PriceCredits = 700
	if _, err := NewBillingCatalogService(repo, &nextBundle, BillingCatalogOptions{Now: func() time.Time { return time.Now().Add(time.Hour) }}).Publish(ctx); err != nil {
		t.Fatal(err)
	}
	replay, err := svc.CreateTaskQuote(ctx, request)
	if err != nil || replay.ID != quote.ID || replay.CatalogID != quote.CatalogID || replay.PriceCredits != quote.PriceCredits {
		t.Fatalf("durable replay = %#v, %v; want original %#v", replay, err, quote)
	}
	if !sameJSONSemantic(replay.AgentProfileSnapshot, quote.AgentProfileSnapshot) {
		t.Fatalf("replay profile snapshot drifted: got=%s want=%s", replay.AgentProfileSnapshot, quote.AgentProfileSnapshot)
	}
	conflict := request
	conflict.RequestFingerprint = billingFingerprint("admission-conflict")
	if _, err := svc.CreateTaskQuote(ctx, conflict); !errors.Is(err, ErrBillingConflict) {
		t.Fatalf("identity conflict = %v, want ErrBillingConflict", err)
	}
}

func TestBillingCatalogQuoteReplayAndConflict(t *testing.T) {
	ctx := context.Background()
	repo := newBillingServiceRepository(t)
	bundle := testBillingBundle()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	svc := NewBillingCatalogService(repo, &bundle, BillingCatalogOptions{
		Now:      func() time.Time { return now },
		QuoteTTL: 2 * time.Minute,
	})
	if _, err := svc.Publish(ctx); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	req := QuoteRequest{
		UserID: billingCatalogUserID, Operation: "task.article", ExecutionProfile: "balanced", RequestFingerprint: billingFingerprint("quote-request"),
		IdempotencyScope: "quote", IdempotencyKey: "quote-key-1",
	}
	first, err := svc.CreateQuote(ctx, req)
	if err != nil {
		t.Fatalf("CreateQuote: %v", err)
	}
	second, err := svc.CreateQuote(ctx, req)
	if err != nil || second.ID != first.ID {
		t.Fatalf("exact replay = %+v, %v; want quote %s", second, err, first.ID)
	}
	if first.PriceCredits != bundle.Products.SKUs[0].PriceCredits || first.CatalogID != bundle.Products.CatalogID || len(first.SKUSnapshot) == 0 {
		t.Fatalf("quote did not pin catalog SKU: %+v", first)
	}

	conflict := req
	conflict.RequestFingerprint = billingFingerprint("different-request")
	if _, err := svc.CreateQuote(ctx, conflict); !errors.Is(err, ErrBillingConflict) {
		t.Fatalf("conflicting replay error = %v, want ErrBillingConflict", err)
	}
}

func TestBillingCatalogTierPricesAndQuoteFreeze(t *testing.T) {
	ctx := context.Background()
	repo := newBillingServiceRepository(t)
	bundle := tieredTestBillingBundle()
	svc := NewBillingCatalogService(repo, &bundle, BillingCatalogOptions{})
	if _, err := svc.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	user, err := repo.Users().FindByID(ctx, billingCatalogUserID)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		tier model.Tier
		want int64
	}{{model.TierFree, 500}, {model.TierPro, 450}, {model.TierEnterprise, 400}}
	for _, tt := range tests {
		user.Tier = tt.tier
		if err := repo.Users().Update(ctx, user); err != nil {
			t.Fatal(err)
		}
		quote, err := svc.CreateQuote(ctx, QuoteRequest{
			UserID: billingCatalogUserID, Operation: "task.article", ExecutionProfile: "balanced", RequestFingerprint: billingFingerprint("tier", string(tt.tier)),
			IdempotencyScope: "tier-quote", IdempotencyKey: string(tt.tier),
		})
		if err != nil {
			t.Fatal(err)
		}
		if quote.PricingTier != string(tt.tier) || quote.ListPriceCredits != 500 || quote.PriceCredits != tt.want || quote.DiscountCredits != 500-tt.want {
			t.Fatalf("%s quote = %#v", tt.tier, quote)
		}
		if quote.PricingRuleID != bundle.Products.CatalogID+":"+string(tt.tier)+":peak" {
			t.Fatalf("%s rule ID = %q", tt.tier, quote.PricingRuleID)
		}
		var snapshot struct {
			MembershipPercent int64  `json:"membership_percent"`
			TimePercent       int64  `json:"time_percent"`
			Rounding          string `json:"rounding"`
		}
		if err := json.Unmarshal(quote.PricingSnapshot, &snapshot); err != nil || snapshot.Rounding != "floor" || snapshot.TimePercent != 100 || snapshot.MembershipPercent != bundle.Products.TierRatesPercent[string(tt.tier)] {
			t.Fatalf("%s pricing snapshot = %s, %#v, %v", tt.tier, quote.PricingSnapshot, snapshot, err)
		}
	}

	user.Tier = model.TierPro
	if err := repo.Users().Update(ctx, user); err != nil {
		t.Fatal(err)
	}
	req := QuoteRequest{
		UserID: billingCatalogUserID, Operation: "task.article", ExecutionProfile: "balanced", RequestFingerprint: billingFingerprint("frozen-quote"),
		IdempotencyScope: "frozen-tier-quote", IdempotencyKey: "stable",
	}
	first, err := svc.CreateQuote(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	user.Tier = model.TierEnterprise
	if err := repo.Users().Update(ctx, user); err != nil {
		t.Fatal(err)
	}
	replay, err := svc.CreateQuote(ctx, req)
	if err != nil || replay.ID != first.ID || replay.PriceCredits != 450 || replay.PricingTier != string(model.TierPro) {
		t.Fatalf("frozen replay = %#v, %v", replay, err)
	}
	req.IdempotencyKey = "new"
	newQuote, err := svc.CreateQuote(ctx, req)
	if err != nil || newQuote.ID == first.ID || newQuote.PriceCredits != 400 || newQuote.PricingTier != string(model.TierEnterprise) {
		t.Fatalf("new tier quote = %#v, %v", newQuote, err)
	}
}

func TestBillingCatalogTaskTimePricingBoundariesAndTierFloor(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name       string
		now        time.Time
		tier       model.Tier
		wantPrice  int64
		wantPeriod string
	}{
		{name: "peak start inclusive", now: time.Date(2026, 7, 20, 1, 0, 0, 0, time.UTC), tier: model.TierPro, wantPrice: 90, wantPeriod: "peak"},
		{name: "peak end exclusive", now: time.Date(2026, 7, 20, 4, 0, 0, 0, time.UTC), tier: model.TierPro, wantPrice: 72, wantPeriod: "off_peak"},
		{name: "second peak start inclusive", now: time.Date(2026, 7, 20, 6, 0, 0, 0, time.UTC), tier: model.TierEnterprise, wantPrice: 80, wantPeriod: "peak"},
		{name: "off peak floors after membership", now: time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC), tier: model.TierEnterprise, wantPrice: 64, wantPeriod: "off_peak"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newBillingServiceRepository(t)
			bundle := testBillingBundle()
			bundle.Products.TaskTimePricing = billing.TaskTimePricing{Timezone: "Asia/Shanghai", PeakWindows: []billing.TimeWindow{{Start: "09:00", End: "12:00"}, {Start: "14:00", End: "18:00"}}, OffPeakWindows: []billing.TimeWindow{{Start: "00:00", End: "09:00"}, {Start: "12:00", End: "14:00"}, {Start: "18:00", End: "24:00"}}, OffPeakRatePercent: 80}
			bundle.Products.SKUs[0].PriceCredits = 101
			svc := NewBillingCatalogService(repo, &bundle, BillingCatalogOptions{Now: func() time.Time { return tt.now }})
			if _, err := svc.Publish(ctx); err != nil {
				t.Fatal(err)
			}
			user, err := repo.Users().FindByID(ctx, billingCatalogUserID)
			if err != nil {
				t.Fatal(err)
			}
			user.Tier = tt.tier
			if err := repo.Users().Update(ctx, user); err != nil {
				t.Fatal(err)
			}
			quote, err := svc.CreateQuote(ctx, QuoteRequest{
				UserID: billingCatalogUserID, Operation: "task.article", ExecutionProfile: "balanced",
				RequestFingerprint: billingFingerprint(tt.name), IdempotencyScope: "time", IdempotencyKey: tt.name,
			})
			if err != nil {
				t.Fatal(err)
			}
			if quote.PriceCredits != tt.wantPrice || quote.DiscountCredits != 101-tt.wantPrice {
				t.Fatalf("quote price=%d discount=%d, want %d/%d", quote.PriceCredits, quote.DiscountCredits, tt.wantPrice, 101-tt.wantPrice)
			}
			if quote.PricingRuleID != bundle.Products.CatalogID+":"+string(tt.tier)+":"+tt.wantPeriod {
				t.Fatalf("pricing rule ID = %q", quote.PricingRuleID)
			}
			var snapshot struct {
				Timezone          string               `json:"timezone"`
				PeakWindows       []billing.TimeWindow `json:"peak_windows"`
				OffPeakWindows    []billing.TimeWindow `json:"off_peak_windows"`
				MembershipPercent int64                `json:"membership_percent"`
				TimePercent       int64                `json:"time_percent"`
				EvaluatedAt       time.Time            `json:"evaluated_at"`
				Period            string               `json:"period"`
				Rounding          string               `json:"rounding"`
				FinalPrice        int64                `json:"final_price_credits"`
			}
			if err := json.Unmarshal(quote.PricingSnapshot, &snapshot); err != nil {
				t.Fatal(err)
			}
			wantTimeRate := int64(100)
			if tt.wantPeriod == "off_peak" {
				wantTimeRate = 80
			}
			_, offset := snapshot.EvaluatedAt.Zone()
			if snapshot.Timezone != "Asia/Shanghai" || snapshot.Period != tt.wantPeriod || snapshot.TimePercent != wantTimeRate ||
				snapshot.MembershipPercent != bundle.Products.TierRatesPercent[string(tt.tier)] || snapshot.Rounding != "floor" || snapshot.FinalPrice != tt.wantPrice ||
				len(snapshot.PeakWindows) != 2 || len(snapshot.OffPeakWindows) != 3 || offset != 8*60*60 || !snapshot.EvaluatedAt.Equal(tt.now) {
				t.Fatalf("pricing snapshot = %s", quote.PricingSnapshot)
			}
		})
	}
}

func TestBillingCatalogOperationSKUIsUnaffectedByOffPeak(t *testing.T) {
	ctx := context.Background()
	repo := newBillingServiceRepository(t)
	bundle := testBillingBundle()
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	svc := NewBillingCatalogService(repo, &bundle, BillingCatalogOptions{Now: func() time.Time { return now }})
	if _, err := svc.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	resolved, err := svc.ResolvePrice(ctx, billingCatalogUserID, bundle.Products.CatalogID, "mcp.generate_image", "image.cover")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.PriceCredits != 100 || resolved.PricingRuleID != bundle.Products.CatalogID+":free" {
		t.Fatalf("operation price = %#v", resolved)
	}
}

func TestBillingCatalogQuoteUsesOneServerTimestampForTimePriceAndCreation(t *testing.T) {
	ctx := context.Background()
	repo := newBillingServiceRepository(t)
	bundle := testBillingBundle()
	bundle.Products.TaskTimePricing = billing.TaskTimePricing{Timezone: "Asia/Shanghai", PeakWindows: []billing.TimeWindow{{Start: "09:00", End: "12:00"}}, OffPeakWindows: []billing.TimeWindow{{Start: "00:00", End: "09:00"}, {Start: "12:00", End: "24:00"}}, OffPeakRatePercent: 80}
	times := []time.Time{
		time.Date(2026, 7, 20, 0, 59, 59, 0, time.UTC),
		time.Date(2026, 7, 20, 1, 0, 0, 0, time.UTC),
	}
	call := 0
	svc := NewBillingCatalogService(repo, &bundle, BillingCatalogOptions{Now: func() time.Time {
		if call >= len(times) {
			return times[len(times)-1]
		}
		value := times[call]
		call++
		return value
	}})
	if _, err := svc.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	call = 0
	quote, err := svc.CreateQuote(ctx, QuoteRequest{UserID: billingCatalogUserID, Operation: "task.article", ExecutionProfile: "balanced", RequestFingerprint: billingFingerprint("single-time"), IdempotencyScope: "time", IdempotencyKey: "single-time"})
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		EvaluatedAt time.Time `json:"evaluated_at"`
	}
	if err := json.Unmarshal(quote.PricingSnapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	if !snapshot.EvaluatedAt.Equal(quote.CreatedAt) || quote.PriceCredits != 400 {
		t.Fatalf("evaluated_at=%s created_at=%s price=%d", snapshot.EvaluatedAt, quote.CreatedAt, quote.PriceCredits)
	}
}

func TestCreateTaskQuoteUsesOneServerTimestampForAdmissionAndQuote(t *testing.T) {
	ctx := context.Background()
	repo := newBillingServiceRepository(t)
	bundle := testBillingBundle()
	bundle.Products.TaskTimePricing = billing.TaskTimePricing{Timezone: "Asia/Shanghai", PeakWindows: []billing.TimeWindow{{Start: "09:00", End: "12:00"}}, OffPeakWindows: []billing.TimeWindow{{Start: "00:00", End: "09:00"}, {Start: "12:00", End: "24:00"}}, OffPeakRatePercent: 80}
	times := []time.Time{
		time.Date(2026, 7, 20, 0, 59, 59, 0, time.UTC),
		time.Date(2026, 7, 20, 1, 0, 0, 0, time.UTC),
	}
	call := 0
	svc := NewBillingCatalogService(repo, &bundle, BillingCatalogOptions{Now: func() time.Time {
		if call >= len(times) {
			return times[len(times)-1]
		}
		value := times[call]
		call++
		return value
	}})
	registry, err := NewAgentProfileRegistry(testAgentProfiles())
	if err != nil {
		t.Fatal(err)
	}
	svc.SetAgentProfileRegistry(registry)
	if _, err := svc.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	user, err := repo.Users().FindByID(ctx, billingCatalogUserID)
	if err != nil {
		t.Fatal(err)
	}
	user.Tier = model.TierPro
	if err := repo.Users().Update(ctx, user); err != nil {
		t.Fatal(err)
	}
	if err := repo.Billing().CreateAccount(ctx, &model.BillingWalletAccount{UserID: billingCatalogUserID, PaidCredits: 400}); err != nil {
		t.Fatal(err)
	}
	call = 0
	quote, err := svc.CreateTaskQuote(ctx, TaskQuoteRequest{
		UserID: billingCatalogUserID, TaskType: "article", ExecutionProfile: "balanced",
		RequestFingerprint: billingFingerprint("single-task-time"), IdempotencyScope: "time", IdempotencyKey: "single-task-time",
	})
	if err != nil {
		t.Fatal(err)
	}
	if quote.PriceCredits != 360 || !quote.CreatedAt.Equal(times[0]) || call != 1 {
		t.Fatalf("quote price=%d created_at=%s clock_calls=%d", quote.PriceCredits, quote.CreatedAt, call)
	}
}

func TestBillingCatalogTaskTimePricingNextTransitionWrapsToNextPeak(t *testing.T) {
	bundle := testBillingBundle()
	bundle.Products.TaskTimePricing = billing.TaskTimePricing{Timezone: "Asia/Shanghai", PeakWindows: []billing.TimeWindow{{Start: "09:00", End: "12:00"}, {Start: "14:00", End: "18:00"}}, OffPeakWindows: []billing.TimeWindow{{Start: "00:00", End: "09:00"}, {Start: "12:00", End: "14:00"}, {Start: "18:00", End: "24:00"}}, OffPeakRatePercent: 80}
	svc := NewBillingCatalogService(newBillingServiceRepository(t), &bundle, BillingCatalogOptions{Now: func() time.Time {
		return time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC) // 20:00 Asia/Shanghai.
	}})
	status, err := svc.CurrentTaskTimePricing()
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 7, 21, 9, 0, 0, 0, status.ServerTime.Location())
	if status.CurrentPeriod != "off_peak" || !status.NextTransitionAt.Equal(want) {
		t.Fatalf("status = %#v, want next transition %s", status, want)
	}
}

func TestBillingCatalogQuoteReplaySurvivesLatestCatalogRollover(t *testing.T) {
	ctx := context.Background()
	repo := newBillingServiceRepository(t)
	firstBundle := testBillingBundle()
	firstNow := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	firstService := NewBillingCatalogService(repo, &firstBundle, BillingCatalogOptions{Now: func() time.Time { return firstNow }})
	if _, err := firstService.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	req := QuoteRequest{
		UserID: billingCatalogUserID, Operation: "task.article", ExecutionProfile: "balanced", RequestFingerprint: billingFingerprint("rollover"),
		IdempotencyScope: "quote", IdempotencyKey: "rollover",
	}
	first, err := firstService.CreateQuote(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	secondBundle := testBillingBundle()
	secondBundle.Products.CatalogID = "retail-test-v2"
	secondBundle.Products.SKUs[0].PriceCredits++
	secondNow := firstNow.Add(time.Hour)
	secondService := NewBillingCatalogService(repo, &secondBundle, BillingCatalogOptions{Now: func() time.Time { return secondNow }})
	if _, err := secondService.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	replay, err := secondService.CreateQuote(ctx, req)
	if err != nil || replay.ID != first.ID || replay.CatalogID != firstBundle.Products.CatalogID {
		t.Fatalf("rollover replay = %+v, %v; want original %+v", replay, err, first)
	}

	conflict := req
	conflict.ExecutionProfile = "quality"
	if _, err := secondService.CreateQuote(ctx, conflict); !errors.Is(err, ErrBillingConflict) {
		t.Fatalf("rollover parameter drift error = %v, want conflict", err)
	}
}

type billingCatalogAccessGuard struct {
	repository.Repository
	calls int
}

func (r *billingCatalogAccessGuard) Billing() repository.BillingRepository {
	return &billingCatalogAccessGuardRepository{BillingRepository: r.Repository.Billing(), calls: &r.calls}
}

type billingCatalogAccessGuardRepository struct {
	repository.BillingRepository
	calls *int
}

func (r *billingCatalogAccessGuardRepository) FindQuoteByKey(ctx context.Context, scope, key string) (*model.BillingQuote, error) {
	*r.calls++
	return r.BillingRepository.FindQuoteByKey(ctx, scope, key)
}

func (r *billingCatalogAccessGuardRepository) FindLatestPublishedCatalog(ctx context.Context) (*model.BillingCatalogVersion, error) {
	*r.calls++
	return r.BillingRepository.FindLatestPublishedCatalog(ctx)
}

func (r *billingCatalogAccessGuardRepository) FindSKUByOperation(ctx context.Context, catalogID, operation, route string) (*model.BillingSKU, error) {
	*r.calls++
	return r.BillingRepository.FindSKUByOperation(ctx, catalogID, operation, route)
}

func TestBillingCatalogPublishUsesSemanticJSONAndCompleteSKUEvidence(t *testing.T) {
	t.Run("semantic JSON normalization", func(t *testing.T) {
		repo, db := newBillingServiceRepositoryWithDB(t)
		bundle := testBillingBundle()
		now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
		svc := NewBillingCatalogService(repo, &bundle, BillingCatalogOptions{Now: func() time.Time { return now }})
		if _, err := svc.Publish(context.Background()); err != nil {
			t.Fatal(err)
		}
		catalog, err := repo.Billing().FindCatalogVersion(context.Background(), bundle.Products.CatalogID)
		if err != nil {
			t.Fatal(err)
		}
		var catalogJSON any
		if err := json.Unmarshal(catalog.Snapshot, &catalogJSON); err != nil {
			t.Fatal(err)
		}
		indentedCatalog, err := json.MarshalIndent(catalogJSON, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&model.BillingCatalogVersion{}).Where("catalog_id = ?", catalog.CatalogID).Update("snapshot", indentedCatalog).Error; err != nil {
			t.Fatal(err)
		}
		skus, err := repo.Billing().ListSKUsByCatalog(context.Background(), catalog.CatalogID)
		if err != nil {
			t.Fatal(err)
		}
		for _, sku := range skus {
			var skuJSON any
			if err := json.Unmarshal(sku.Snapshot, &skuJSON); err != nil {
				t.Fatal(err)
			}
			indentedSKU, err := json.MarshalIndent(skuJSON, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&model.BillingSKU{}).Where("id = ?", sku.ID).Update("snapshot", indentedSKU).Error; err != nil {
				t.Fatal(err)
			}
		}
		if _, err := svc.Publish(context.Background()); err != nil {
			t.Fatalf("semantic replay: %v", err)
		}
	})

	for _, tt := range []struct {
		name   string
		mutate func(*testing.T, repository.Repository, *gorm.DB, billing.Bundle)
	}{
		{
			name: "missing SKU",
			mutate: func(t *testing.T, repo repository.Repository, db *gorm.DB, bundle billing.Bundle) {
				skus, err := repo.Billing().ListSKUsByCatalog(context.Background(), bundle.Products.CatalogID)
				if err != nil || len(skus) == 0 {
					t.Fatalf("list SKUs = %+v, %v", skus, err)
				}
				if err := db.Delete(&model.BillingSKU{}, "id = ?", skus[0].ID).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "extra SKU",
			mutate: func(t *testing.T, _ repository.Repository, db *gorm.DB, bundle billing.Bundle) {
				extra := model.BillingSKU{
					ID: uuid.NewString(), CatalogID: bundle.Products.CatalogID, SKUID: "extra.v1", Operation: "extra",
					PriceCredits: 1, Policy: "task_admission", Delivery: "task", Snapshot: []byte(`{"ID":"extra.v1"}`),
				}
				if err := db.Create(&extra).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "drifted SKU",
			mutate: func(t *testing.T, repo repository.Repository, db *gorm.DB, bundle billing.Bundle) {
				skus, err := repo.Billing().ListSKUsByCatalog(context.Background(), bundle.Products.CatalogID)
				if err != nil || len(skus) == 0 {
					t.Fatalf("list SKUs = %+v, %v", skus, err)
				}
				if err := db.Model(&model.BillingSKU{}).Where("id = ?", skus[0].ID).Update("price_credits", skus[0].PriceCredits+1).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "drifted execution profile column",
			mutate: func(t *testing.T, repo repository.Repository, db *gorm.DB, bundle billing.Bundle) {
				skus, err := repo.Billing().ListSKUsByCatalog(context.Background(), bundle.Products.CatalogID)
				if err != nil || len(skus) == 0 {
					t.Fatalf("list SKUs = %+v, %v", skus, err)
				}
				if err := db.Model(&model.BillingSKU{}).Where("id = ?", skus[0].ID).Update("execution_profile", "quality").Error; err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo, db := newBillingServiceRepositoryWithDB(t)
			bundle := testBillingBundle()
			now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
			svc := NewBillingCatalogService(repo, &bundle, BillingCatalogOptions{Now: func() time.Time { return now }})
			if _, err := svc.Publish(context.Background()); err != nil {
				t.Fatal(err)
			}
			tt.mutate(t, repo, db, bundle)
			if _, err := svc.Publish(context.Background()); !errors.Is(err, ErrBillingConflict) {
				t.Fatalf("evidence drift error = %v, want conflict", err)
			}
		})
	}
}

func TestSameJSONSemanticPreservesIntegerPrecision(t *testing.T) {
	left := []byte(`{"price_credits":9007199254740992}`)
	right := []byte(`{"price_credits":9007199254740993}`)
	if sameJSONSemantic(left, right) {
		t.Fatal("distinct int64 JSON values compared equal")
	}
}

func newBillingServiceRepository(t *testing.T) repository.Repository {
	t.Helper()
	repo, _ := newBillingServiceRepositoryWithDB(t)
	return repo
}

func newBillingServiceRepositoryWithDB(t *testing.T) (repository.Repository, *gorm.DB) {
	t.Helper()
	dsn := "file:" + uuid.NewString() + "?mode=memory&cache=shared&_busy_timeout=10000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	repo := repository.New(db)
	if err := repo.Users().Create(context.Background(), &model.User{
		ID: billingCatalogUserID, Email: "catalog-pricing@example.com", Password: "fixture", InviteCode: "CATALOG1",
	}); err != nil {
		t.Fatalf("create billing catalog user: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	return repo, db
}

func testBillingBundle() billing.Bundle {
	return billing.Bundle{
		Economics: billing.EconomicsConfig{CreditsPerCNY: 1_000},
		Policy: billing.PolicySnapshot{
			TaskAdmission: billing.TaskAdmissionPolicy{RequireZeroDebt: true, RequireFullPrice: true},
			AcceptedTask:  billing.AcceptedTaskPolicy{ContinueWhenBalanceNegative: true, OperationChargeMayCreateDebt: true},
			TopUp:         billing.TopUpPolicy{RepayDebtFirst: true},
			Promotions:    billing.PromotionsPolicy{MayRepayDebt: false},
			TaskFailureReversal: billing.TaskFailureReversalPolicy{
				Enabled: true,
				Reasons: []string{"platform_error", "provider_error", "execution_timeout", "infrastructure_cancelled"},
			},
		},
		Products: billing.ProductCatalog{
			CatalogID: "retail-test-v1", Currency: "credits", TierRatesPercent: map[string]int64{"free": 100, "pro": 90, "enterprise": 80},
			TaskTimePricing: billing.TaskTimePricing{Timezone: "Asia/Shanghai", PeakWindows: []billing.TimeWindow{{Start: "00:15", End: "24:00"}}, OffPeakWindows: []billing.TimeWindow{{Start: "00:00", End: "00:15"}}, OffPeakRatePercent: 80},
			SKUs: []billing.SKUConfig{
				{ID: "task.article.v1", Operation: "task.article", ExecutionProfile: "balanced", ChargePolicy: "task_admission", PriceCredits: 500, Delivery: "article"},
				{ID: "image.cover.v1", Operation: "mcp.generate_image", ChargePolicy: "accepted_task_operation", PriceCredits: 100, Route: "image.cover", Delivery: "image"},
				{ID: "image.standalone.v1", Operation: "designer.generate_image", ChargePolicy: "standalone_operation", PriceCredits: 100, Route: "image.designer", Delivery: "image"},
			},
		},
	}
}

func tieredTestBillingBundle() billing.Bundle {
	return testBillingBundle()
}
