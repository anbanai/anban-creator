package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	serverbilling "github.com/anbanai/anban-creator/server/billing"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type MarginService struct {
	repo          repository.BillingMarginRepository
	appRepo       repository.Repository
	creditsPerCNY int64
	now           func() time.Time
}

type MarginServiceOptions struct {
	Now func() time.Time
}

type MarginReconciliationReport struct {
	ChargeSources          int64 `json:"charge_sources"`
	TopUpSources           int64 `json:"topup_sources"`
	ProviderCostSources    int64 `json:"provider_cost_sources"`
	FactCount              int64 `json:"fact_count"`
	InsertedFacts          int64 `json:"inserted_facts"`
	MissingFacts           int64 `json:"missing_facts"`
	UnreconciledExecutions int64 `json:"unreconciled_executions"`
	UnsettledOutbox        int64 `json:"unsettled_outbox"`
}

type MarginTotals struct {
	CashMicroCNY                int64 `json:"cash_micro_cny"`
	DeferredPaidMicroCNY        int64 `json:"deferred_paid_micro_cny"`
	RecognizedRevenueMicroCNY   int64 `json:"recognized_revenue_micro_cny"`
	PromotionMicroCNY           int64 `json:"promotion_micro_cny"`
	ReceivableCreatedMicroCNY   int64 `json:"receivable_created_micro_cny"`
	ReceivableCollectedMicroCNY int64 `json:"receivable_collected_micro_cny"`
	ProviderCostMicroCNY        int64 `json:"provider_cost_micro_cny"`
	FailureCostMicroCNY         int64 `json:"failure_cost_micro_cny"`
	ContributionMarginMicroCNY  int64 `json:"contribution_margin_micro_cny"`
}

type MarginDisplay struct {
	Cash                string `json:"cash_cny"`
	DeferredPaid        string `json:"deferred_paid_cny"`
	RecognizedRevenue   string `json:"recognized_revenue_cny"`
	Promotion           string `json:"promotion_cny"`
	ReceivableCreated   string `json:"receivable_created_cny"`
	ReceivableCollected string `json:"receivable_collected_cny"`
	ProviderCost        string `json:"provider_cost_cny"`
	FailureCost         string `json:"failure_cost_cny"`
	ContributionMargin  string `json:"contribution_margin_cny"`
}

type MarginReportRow struct {
	Group   string        `json:"group"`
	Facts   int64         `json:"facts"`
	Totals  MarginTotals  `json:"totals"`
	Display MarginDisplay `json:"display"`
}

type MarginReport struct {
	GroupBy string            `json:"group_by"`
	From    *time.Time        `json:"from,omitempty"`
	To      *time.Time        `json:"to,omitempty"`
	Rows    []MarginReportRow `json:"rows"`
	Totals  MarginTotals      `json:"totals"`
	Display MarginDisplay     `json:"display"`
}

func NewMarginService(repo repository.BillingMarginRepository, appRepo repository.Repository, bundle *serverbilling.Bundle, opts MarginServiceOptions) *MarginService {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	creditsPerCNY := int64(0)
	if bundle != nil {
		creditsPerCNY = bundle.Policy.CreditsPerCNY
	}
	return &MarginService{repo: repo, appRepo: appRepo, creditsPerCNY: creditsPerCNY, now: now}
}

func (s *MarginService) RecordRetailCharge(ctx context.Context, chargeID string) error {
	charge, err := s.repo.FindCharge(ctx, strings.TrimSpace(chargeID))
	if err != nil {
		return err
	}
	fact, err := s.factFromCharge(ctx, charge)
	if err != nil {
		return err
	}
	_, err = s.repo.AppendFact(ctx, fact)
	return err
}

func (s *MarginService) RecordRetailReversal(ctx context.Context, chargeID string) error {
	charge, err := s.repo.FindCharge(ctx, strings.TrimSpace(chargeID))
	if err != nil {
		return err
	}
	if charge.Kind != model.BillingChargeKindReversal {
		return fmt.Errorf("charge %s is not a reversal", charge.ID)
	}
	fact, err := s.factFromCharge(ctx, charge)
	if err != nil {
		return err
	}
	_, err = s.repo.AppendFact(ctx, fact)
	return err
}

func (s *MarginService) RecordProviderCost(ctx context.Context, eventID string) error {
	event, err := s.repo.FindProviderCostEvent(ctx, strings.TrimSpace(eventID))
	if err != nil {
		return err
	}
	fact, err := s.factFromProviderCost(event)
	if err != nil {
		return err
	}
	_, err = s.repo.AppendFact(ctx, fact)
	return err
}

func (s *MarginService) RecordTopUp(ctx context.Context, entryID string) error {
	entry, err := s.repo.FindTopUpEntry(ctx, strings.TrimSpace(entryID))
	if err != nil {
		return err
	}
	fact, err := s.factFromTopUp(ctx, entry)
	if err != nil {
		return err
	}
	_, err = s.repo.AppendFact(ctx, fact)
	return err
}

func (s *MarginService) Reconcile(ctx context.Context, _ time.Time) (*MarginReconciliationReport, error) {
	if s == nil || s.repo == nil || s.creditsPerCNY <= 0 {
		return nil, errors.New("margin service is not configured")
	}
	before, err := s.repo.ListFacts(ctx, time.Time{}, time.Time{})
	if err != nil {
		return nil, err
	}
	chargeCount, err := s.repo.CountPostedCharges(ctx)
	if err != nil {
		return nil, err
	}
	charges, err := s.repo.ListUnprojectedCharges(ctx)
	if err != nil {
		return nil, err
	}
	for index := range charges {
		fact, buildErr := s.factFromCharge(ctx, &charges[index])
		if buildErr != nil {
			return nil, buildErr
		}
		if _, appendErr := s.repo.AppendFact(ctx, fact); appendErr != nil {
			return nil, appendErr
		}
	}
	topUpCount, err := s.repo.CountTopUpEntries(ctx)
	if err != nil {
		return nil, err
	}
	topups, err := s.repo.ListUnprojectedTopUpEntries(ctx)
	if err != nil {
		return nil, err
	}
	for index := range topups {
		fact, buildErr := s.factFromTopUp(ctx, &topups[index])
		if buildErr != nil {
			return nil, buildErr
		}
		if _, appendErr := s.repo.AppendFact(ctx, fact); appendErr != nil {
			return nil, appendErr
		}
	}
	providerCostCount, err := s.repo.CountProviderCostEvents(ctx)
	if err != nil {
		return nil, err
	}
	costs, err := s.repo.ListUnprojectedProviderCostEvents(ctx)
	if err != nil {
		return nil, err
	}
	for index := range costs {
		fact, buildErr := s.factFromProviderCost(&costs[index])
		if buildErr != nil {
			return nil, buildErr
		}
		if _, appendErr := s.repo.AppendFact(ctx, fact); appendErr != nil {
			return nil, appendErr
		}
	}
	after, err := s.repo.ListFacts(ctx, time.Time{}, time.Time{})
	if err != nil {
		return nil, err
	}
	unreconciled, err := s.repo.CountUnreconciledExecutionStatuses(ctx)
	if err != nil {
		return nil, err
	}
	unsettled, err := s.repo.CountUnsettledOutbox(ctx)
	if err != nil {
		return nil, err
	}
	sources, ok := checkedInt64Add(chargeCount, topUpCount)
	if !ok {
		return nil, errors.New("margin source count overflows int64")
	}
	sources, ok = checkedInt64Add(sources, providerCostCount)
	if !ok {
		return nil, errors.New("margin source count overflows int64")
	}
	missing := sources - int64(len(after))
	if missing < 0 {
		missing = 0
	}
	return &MarginReconciliationReport{
		ChargeSources: chargeCount, TopUpSources: topUpCount, ProviderCostSources: providerCostCount,
		FactCount: int64(len(after)), InsertedFacts: int64(len(after) - len(before)), MissingFacts: missing,
		UnreconciledExecutions: unreconciled, UnsettledOutbox: unsettled,
	}, nil
}

func (s *MarginService) Report(ctx context.Context, from, to time.Time, groupBy string, providerOnly bool) (*MarginReport, error) {
	if _, err := s.Reconcile(ctx, s.now().UTC()); err != nil {
		return nil, err
	}
	groupBy = strings.ToLower(strings.TrimSpace(groupBy))
	if groupBy == "" {
		if providerOnly {
			groupBy = "provider"
		} else {
			groupBy = "day"
		}
	}
	switch groupBy {
	case "day", "task", "sku", "provider", "model":
	default:
		return nil, fmt.Errorf("%w: unsupported margin report grouping %q", ErrBillingInvalid, groupBy)
	}
	facts, err := s.repo.ListFacts(ctx, from, to)
	if err != nil {
		return nil, err
	}
	failedTasks := make(map[string]bool)
	rows := make(map[string]*MarginReportRow)
	report := &MarginReport{GroupBy: groupBy, Rows: []MarginReportRow{}}
	if !from.IsZero() {
		value := from.UTC()
		report.From = &value
	}
	if !to.IsZero() {
		value := to.UTC()
		report.To = &value
	}
	for index := range facts {
		fact := facts[index]
		if providerOnly && fact.Kind != model.BillingMarginFactProviderCost {
			continue
		}
		key := marginGroupKey(fact, groupBy)
		row := rows[key]
		if row == nil {
			row = &MarginReportRow{Group: key}
			rows[key] = row
		}
		failureCost, statusErr := s.failureCost(ctx, fact, failedTasks)
		if statusErr != nil {
			return nil, statusErr
		}
		if err := addMarginFact(&row.Totals, fact, failureCost); err != nil {
			return nil, err
		}
		row.Facts++
		if err := addMarginTotals(&report.Totals, rowDelta(fact, failureCost)); err != nil {
			return nil, err
		}
	}
	keys := make([]string, 0, len(rows))
	for key := range rows {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		row := rows[key]
		row.Display = marginDisplay(row.Totals)
		report.Rows = append(report.Rows, *row)
	}
	report.Display = marginDisplay(report.Totals)
	return report, nil
}

func (s *MarginService) factFromCharge(ctx context.Context, charge *model.BillingCharge) (*model.BillingMarginFact, error) {
	if charge == nil || charge.Status != model.BillingChargeStatusPosted {
		return nil, errors.New("posted charge is required for margin projection")
	}
	kind := model.BillingMarginFactRetailCharge
	sign := int64(1)
	original := charge
	if charge.Kind == model.BillingChargeKindReversal {
		kind, sign = model.BillingMarginFactRetailReversal, -1
		if charge.ReversalOfID == nil {
			return nil, errors.New("margin reversal is missing original charge")
		}
		var err error
		original, err = s.repo.FindCharge(ctx, *charge.ReversalOfID)
		if err != nil {
			return nil, err
		}
	}
	paid, err := s.creditsToMicroCNY(charge.PaidCredits)
	if err != nil {
		return nil, err
	}
	promotional, err := s.creditsToMicroCNY(charge.PromotionalCredits)
	if err != nil {
		return nil, err
	}
	debt, err := s.creditsToMicroCNY(charge.DebtCredits)
	if err != nil {
		return nil, err
	}
	revenue, ok := checkedInt64Add(paid, debt)
	if !ok {
		return nil, errors.New("margin revenue overflows int64")
	}
	taskID := ""
	if original.TaskID != nil {
		taskID = *original.TaskID
	} else if original.OperationTaskID != nil {
		taskID = *original.OperationTaskID
	}
	fact := &model.BillingMarginFact{
		ID: uuid.NewString(), Kind: kind, SourceKind: "charge", SourceID: charge.ID,
		UserID: charge.UserID, TaskID: taskID, CatalogID: charge.CatalogID, SKUID: charge.SKUID,
		DeferredPaidMicroCNY: -sign * paid, RecognizedRevenueMicroCNY: sign * revenue,
		PromotionMicroCNY: sign * promotional, ReceivableCreatedMicroCNY: sign * debt,
		ContributionMarginMicroCNY: sign * revenue, OccurredAt: charge.CreatedAt.UTC(), CreatedAt: s.now().UTC(),
	}
	if kind == model.BillingMarginFactRetailReversal {
		fact.DeferredPaidMicroCNY = paid
	}
	return s.finalizeFact(fact)
}

func (s *MarginService) factFromTopUp(ctx context.Context, entry *model.BillingWalletEntry) (*model.BillingMarginFact, error) {
	if entry == nil || entry.EventKind != model.BillingWalletEventKindTopUp || entry.PaidDelta < 0 {
		return nil, errors.New("top-up entry is required for margin projection")
	}
	repaidCredits, err := s.repo.SumDebtRepaymentForTopUp(ctx, entry.ID)
	if err != nil {
		return nil, err
	}
	totalCredits, ok := checkedInt64Add(entry.PaidDelta, repaidCredits)
	if !ok {
		return nil, errors.New("top-up credits overflow int64")
	}
	cash, err := s.creditsToMicroCNY(totalCredits)
	if err != nil {
		return nil, err
	}
	deferred, err := s.creditsToMicroCNY(entry.PaidDelta)
	if err != nil {
		return nil, err
	}
	collected, err := s.creditsToMicroCNY(repaidCredits)
	if err != nil {
		return nil, err
	}
	return s.finalizeFact(&model.BillingMarginFact{
		ID: uuid.NewString(), Kind: model.BillingMarginFactTopUp, SourceKind: "topup_entry", SourceID: entry.ID,
		UserID: entry.UserID, CatalogID: entry.CatalogID, CashMicroCNY: cash, DeferredPaidMicroCNY: deferred,
		ReceivableCollectedMicroCNY: collected, OccurredAt: entry.CreatedAt.UTC(), CreatedAt: s.now().UTC(),
	})
}

func (s *MarginService) factFromProviderCost(event *model.BillingProviderCostEvent) (*model.BillingMarginFact, error) {
	if event == nil {
		return nil, errors.New("provider cost event is required for margin projection")
	}
	return s.finalizeFact(&model.BillingMarginFact{
		ID: uuid.NewString(), Kind: model.BillingMarginFactProviderCost, SourceKind: "provider_cost_event", SourceID: event.ID,
		TaskID: event.TaskID, ExecutionID: event.ExecutionID, CatalogID: event.CatalogID,
		Provider: event.Provider, Model: event.Model, ProviderCostMicroCNY: event.CostMicroCNY,
		ContributionMarginMicroCNY: -event.CostMicroCNY, OccurredAt: event.CreatedAt.UTC(), CreatedAt: s.now().UTC(),
	})
}

func (s *MarginService) finalizeFact(fact *model.BillingMarginFact) (*model.BillingMarginFact, error) {
	payload := *fact
	payload.ID, payload.SourceFingerprint, payload.CreatedAt = "", "", time.Time{}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(encoded)
	fact.SourceFingerprint = hex.EncodeToString(digest[:])
	if err := fact.Validate(); err != nil {
		return nil, err
	}
	return fact, nil
}

func (s *MarginService) creditsToMicroCNY(credits int64) (int64, error) {
	if credits < 0 || s.creditsPerCNY <= 0 {
		return 0, errors.New("invalid credits-to-CNY conversion")
	}
	const unit = int64(1_000_000)
	whole := credits / s.creditsPerCNY
	remainder := credits % s.creditsPerCNY
	if whole > math.MaxInt64/unit || remainder > math.MaxInt64/unit {
		return 0, errors.New("credits-to-CNY conversion overflows int64")
	}
	fractionNumerator := remainder * unit
	if fractionNumerator%s.creditsPerCNY != 0 {
		return 0, errors.New("credits_per_cny cannot represent exact micro-CNY revenue")
	}
	return whole*unit + fractionNumerator/s.creditsPerCNY, nil
}

func (s *MarginService) failureCost(ctx context.Context, fact model.BillingMarginFact, cache map[string]bool) (int64, error) {
	if fact.Kind != model.BillingMarginFactProviderCost || fact.TaskID == "" || s.appRepo == nil {
		return 0, nil
	}
	failed, ok := cache[fact.TaskID]
	if !ok {
		task, err := s.appRepo.Tasks().FindByID(ctx, fact.TaskID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			cache[fact.TaskID] = false
			return 0, nil
		}
		if err != nil {
			return 0, err
		}
		failed = task.Status == model.TaskStatusFailed || task.Status == model.TaskStatusCancelled
		cache[fact.TaskID] = failed
	}
	if failed {
		return fact.ProviderCostMicroCNY, nil
	}
	return 0, nil
}

func marginGroupKey(fact model.BillingMarginFact, groupBy string) string {
	switch groupBy {
	case "task":
		if fact.TaskID != "" {
			return fact.TaskID
		}
	case "sku":
		if fact.SKUID != "" {
			return fact.SKUID
		}
	case "provider":
		if fact.Provider != "" {
			return fact.Provider
		}
	case "model":
		if fact.Model != "" {
			return fact.Model
		}
	}
	if groupBy == "day" {
		return fact.OccurredAt.UTC().Format("2006-01-02")
	}
	return "unattributed"
}

func addMarginFact(target *MarginTotals, fact model.BillingMarginFact, failureCost int64) error {
	return addMarginTotals(target, rowDelta(fact, failureCost))
}

func rowDelta(fact model.BillingMarginFact, failureCost int64) MarginTotals {
	return MarginTotals{
		CashMicroCNY: fact.CashMicroCNY, DeferredPaidMicroCNY: fact.DeferredPaidMicroCNY,
		RecognizedRevenueMicroCNY: fact.RecognizedRevenueMicroCNY, PromotionMicroCNY: fact.PromotionMicroCNY,
		ReceivableCreatedMicroCNY: fact.ReceivableCreatedMicroCNY, ReceivableCollectedMicroCNY: fact.ReceivableCollectedMicroCNY,
		ProviderCostMicroCNY: fact.ProviderCostMicroCNY, FailureCostMicroCNY: failureCost,
		ContributionMarginMicroCNY: fact.ContributionMarginMicroCNY,
	}
}

func addMarginTotals(target *MarginTotals, delta MarginTotals) error {
	fields := [][2]*int64{
		{&target.CashMicroCNY, &delta.CashMicroCNY}, {&target.DeferredPaidMicroCNY, &delta.DeferredPaidMicroCNY},
		{&target.RecognizedRevenueMicroCNY, &delta.RecognizedRevenueMicroCNY}, {&target.PromotionMicroCNY, &delta.PromotionMicroCNY},
		{&target.ReceivableCreatedMicroCNY, &delta.ReceivableCreatedMicroCNY}, {&target.ReceivableCollectedMicroCNY, &delta.ReceivableCollectedMicroCNY},
		{&target.ProviderCostMicroCNY, &delta.ProviderCostMicroCNY}, {&target.FailureCostMicroCNY, &delta.FailureCostMicroCNY},
		{&target.ContributionMarginMicroCNY, &delta.ContributionMarginMicroCNY},
	}
	for _, field := range fields {
		value, ok := checkedInt64Add(*field[0], *field[1])
		if !ok {
			return errors.New("margin report total overflows int64")
		}
		*field[0] = value
	}
	return nil
}

func checkedInt64Add(left, right int64) (int64, bool) {
	if right > 0 && left > math.MaxInt64-right {
		return 0, false
	}
	if right < 0 && left < math.MinInt64-right {
		return 0, false
	}
	return left + right, true
}

func marginDisplay(totals MarginTotals) MarginDisplay {
	return MarginDisplay{
		Cash: formatMicroCNY(totals.CashMicroCNY), DeferredPaid: formatMicroCNY(totals.DeferredPaidMicroCNY),
		RecognizedRevenue: formatMicroCNY(totals.RecognizedRevenueMicroCNY), Promotion: formatMicroCNY(totals.PromotionMicroCNY),
		ReceivableCreated: formatMicroCNY(totals.ReceivableCreatedMicroCNY), ReceivableCollected: formatMicroCNY(totals.ReceivableCollectedMicroCNY),
		ProviderCost: formatMicroCNY(totals.ProviderCostMicroCNY), FailureCost: formatMicroCNY(totals.FailureCostMicroCNY),
		ContributionMargin: formatMicroCNY(totals.ContributionMarginMicroCNY),
	}
}

func formatMicroCNY(value int64) string {
	sign := ""
	magnitude := value
	if value < 0 {
		sign = "-"
		if value == math.MinInt64 {
			whole := uint64(math.MaxInt64)/1_000_000 + 1
			fraction := uint64(math.MaxInt64)%1_000_000 + 1
			if fraction >= 1_000_000 {
				whole++
				fraction -= 1_000_000
			}
			return sign + strconv.FormatUint(whole, 10) + "." + fmt.Sprintf("%06d", fraction)
		}
		magnitude = -value
	}
	return sign + strconv.FormatInt(magnitude/1_000_000, 10) + "." + fmt.Sprintf("%06d", magnitude%1_000_000)
}
