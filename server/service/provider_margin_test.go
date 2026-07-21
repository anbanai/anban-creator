package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/datatypes"
)

func TestMarginReconciliationProjectsRevenueReceivablesCostAndAdjustments(t *testing.T) {
	repo, db := newBillingServiceRepositoryWithDB(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC)
	bundle := testBillingBundle()
	bundle.Policy.CreditsPerCNY = 1_000
	userID, taskID := uuid.NewString(), uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: userID + "@margin.test", Password: "x", InviteCode: "MARGIN01"}); err != nil {
		t.Fatal(err)
	}
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusFailed}); err != nil {
		t.Fatal(err)
	}
	attemptID, toolCallID := uuid.NewString(), "ark-margin-1"
	accepted := &model.BillingCharge{
		ID: uuid.NewString(), UserID: userID, CatalogID: "retail-test-v1", SKUID: "video.margin.v1",
		ResourceType: "video", ResourceID: uuid.NewString(), Kind: model.BillingChargeKindOperation,
		Policy: "accepted_task_operation", Status: model.BillingChargeStatusPosted,
		PriceCredits: 1000, PaidCredits: 600, PromotionalCredits: 100, DebtCredits: 300,
		OperationTaskID: &taskID, AttemptID: &attemptID, ToolCallID: &toolCallID,
		IdempotencyScope: "margin-charge", IdempotencyKey: "accepted", RequestFingerprint: strings.Repeat("a", 64), CreatedAt: now,
	}
	if err := db.Create(accepted).Error; err != nil {
		t.Fatal(err)
	}
	originalTaskID := uuid.NewString()
	original := &model.BillingCharge{
		ID: uuid.NewString(), UserID: userID, CatalogID: "retail-test-v1", SKUID: "task.margin.v1",
		ResourceType: "task", ResourceID: originalTaskID, Kind: model.BillingChargeKindTask, Policy: "task_admission",
		Status: model.BillingChargeStatusPosted, PriceCredits: 500, PaidCredits: 400, PromotionalCredits: 100,
		TaskID: &originalTaskID, IdempotencyScope: "margin-charge", IdempotencyKey: "task", RequestFingerprint: strings.Repeat("b", 64), CreatedAt: now.Add(time.Minute),
	}
	if err := db.Create(original).Error; err != nil {
		t.Fatal(err)
	}
	reversal := &model.BillingCharge{
		ID: uuid.NewString(), UserID: userID, CatalogID: original.CatalogID, SKUID: original.SKUID,
		ResourceType: original.ResourceType, ResourceID: original.ResourceID, Kind: model.BillingChargeKindReversal,
		Policy: "reversal", Status: model.BillingChargeStatusPosted, PriceCredits: 500, PaidCredits: 400, PromotionalCredits: 100,
		ReversalOfID: &original.ID, IdempotencyScope: "reversal", IdempotencyKey: "task-reversal",
		RequestFingerprint: strings.Repeat("c", 64), CreatedAt: now.Add(2 * time.Minute),
	}
	if err := db.Create(reversal).Error; err != nil {
		t.Fatal(err)
	}
	topup := &model.BillingWalletEntry{
		ID: uuid.NewString(), UserID: userID, EventKind: model.BillingWalletEventKindTopUp, PaidDelta: 200,
		CatalogID: "retail-test-v1", RequestFingerprint: strings.Repeat("d", 64),
		SourceType: stringPtr("manual_api"), SourceID: stringPtr("payment-margin-1"),
		IdempotencyScope: "topup", IdempotencyKey: "payment-margin-1", CreatedAt: now.Add(3 * time.Minute),
	}
	if err := db.Create(topup).Error; err != nil {
		t.Fatal(err)
	}
	repaymentEntryID := uuid.NewString()
	if err := db.Create(&model.BillingDebtAllocation{
		ID: uuid.NewString(), UserID: userID, ChargeID: accepted.ID, EntryID: repaymentEntryID,
		SourceEntryID: topup.ID, Kind: model.BillingDebtAllocationKindRepayment, Credits: 300, CreatedAt: topup.CreatedAt,
	}).Error; err != nil {
		t.Fatal(err)
	}
	baseIdentity, err := model.ProviderCostBaseIdentityKey(model.BillingProviderCostIdentityProviderRequest, "", "volcengine_ark", "doubao-seed-evolving", "ark-margin-cost")
	if err != nil {
		t.Fatal(err)
	}
	baseCost := &model.BillingProviderCostEvent{
		ID: uuid.NewString(), EventKind: model.BillingProviderCostEventKindBase, IdentityKind: model.BillingProviderCostIdentityProviderRequest,
		ProviderRequestID: "ark-margin-cost", TaskID: taskID, Provider: "volcengine_ark", Model: "doubao-seed-evolving", CatalogID: "cost-v1",
		IdempotencyScope: "provider-cost", IdempotencyKey: "ark-margin-cost", BaseIdentityKey: &baseIdentity,
		RequestFingerprint: strings.Repeat("e", 64), Source: model.BillingProviderCostSourceProviderResponse,
		Status: model.BillingProviderCostStatusReconciled, CostMicroCNY: 200_000,
		UsageEvidence: datatypes.JSON(`{"kind":"video_output"}`), CalculationSnapshot: datatypes.JSON(`{"version":1}`), CreatedAt: now.Add(4 * time.Minute),
	}
	if err := db.Create(baseCost).Error; err != nil {
		t.Fatal(err)
	}
	adjustmentOriginal := baseCost.ID
	adjustment := &model.BillingProviderCostEvent{
		ID: uuid.NewString(), EventKind: model.BillingProviderCostEventKindAdjustment, TaskID: taskID,
		Provider: baseCost.Provider, Model: baseCost.Model, CatalogID: baseCost.CatalogID,
		IdempotencyScope: "provider-cost-adjustment", IdempotencyKey: "invoice-credit", RequestFingerprint: strings.Repeat("f", 64),
		OriginalEventID: &adjustmentOriginal, Source: model.BillingProviderCostSourceInvoiceAdjustment,
		Status: model.BillingProviderCostStatusReconciled, CostMicroCNY: -50_000,
		UsageEvidence: datatypes.JSON(`{"kind":"invoice_adjustment"}`), CalculationSnapshot: datatypes.JSON(`{"version":1}`), CreatedAt: now.Add(5 * time.Minute),
	}
	if err := db.Create(adjustment).Error; err != nil {
		t.Fatal(err)
	}

	margin := NewMarginService(repository.NewBillingMarginRepository(db), repo, &bundle, MarginServiceOptions{Now: func() time.Time { return now.Add(time.Hour) }})
	first, err := margin.Reconcile(ctx, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if first.InsertedFacts != 6 || first.MissingFacts != 0 {
		t.Fatalf("first reconciliation = %+v", first)
	}
	second, err := margin.Reconcile(ctx, now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if second.InsertedFacts != 0 || second.FactCount != 6 {
		t.Fatalf("idempotent reconciliation = %+v", second)
	}
	report, err := margin.Report(ctx, time.Time{}, time.Time{}, "day", false)
	if err != nil {
		t.Fatal(err)
	}
	want := MarginTotals{
		CashMicroCNY: 500_000, DeferredPaidMicroCNY: -400_000, RecognizedRevenueMicroCNY: 900_000,
		PromotionMicroCNY: 100_000, ReceivableCreatedMicroCNY: 300_000, ReceivableCollectedMicroCNY: 300_000,
		ProviderCostMicroCNY: 150_000, FailureCostMicroCNY: 150_000, ContributionMarginMicroCNY: 750_000,
	}
	if report.Totals != want {
		t.Fatalf("margin totals = %+v, want %+v", report.Totals, want)
	}
	if report.Display.RecognizedRevenue != "0.900000" || report.Display.ProviderCost != "0.150000" || report.Display.ContributionMargin != "0.750000" {
		t.Fatalf("margin display = %+v", report.Display)
	}
	costs, err := margin.Report(ctx, time.Time{}, time.Time{}, "provider", true)
	if err != nil {
		t.Fatal(err)
	}
	if costs.Totals.ProviderCostMicroCNY != 150_000 || len(costs.Rows) != 1 || costs.Rows[0].Group != "volcengine_ark" {
		t.Fatalf("cost report = %+v", costs)
	}
}
