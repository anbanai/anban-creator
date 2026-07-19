package service

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/billing"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/mattn/go-sqlite3"
	"gorm.io/gorm"
)

var (
	ErrBillingDebtOutstanding                    = errors.New("billing debt outstanding")
	ErrBillingInsufficientForTask                = errors.New("billing insufficient for task")
	ErrBillingInsufficientForStandaloneOperation = errors.New("billing insufficient for standalone operation")
	ErrBillingQuoteMismatch                      = errors.New("billing quote mismatch")
	ErrBillingReversalNotAllowed                 = errors.New("billing reversal not allowed")
	ErrBillingLedgerInvalid                      = errors.New("billing ledger is not conserved")
	errBillingSpendInsufficient                  = errors.New("billing spendable credits are insufficient")
)

type BillingWalletOptions struct {
	Now               func() time.Time
	PromotionEligible func(model.BillingCreditLot, model.BillingSKU) bool
}

type BillingWalletService struct {
	repo              repository.Repository
	bundle            billing.Bundle
	now               func() time.Time
	promotionEligible func(model.BillingCreditLot, model.BillingSKU) bool
}

type TaskChargeRequest struct {
	UserID             string
	TaskID             string
	QuoteID            string
	CatalogID          string
	SKUID              string
	RequestFingerprint string
	IdempotencyScope   string
	IdempotencyKey     string
	ActorType          string
	ActorID            string
	SourceService      string
	RequestID          string
	CorrelationID      string
}

type OperationChargeRequest struct {
	UserID             string
	QuoteID            string
	CatalogID          string
	SKUID              string
	TaskID             string
	AttemptID          string
	ToolCallID         string
	ResourceType       string
	ResourceID         string
	RequestFingerprint string
	IdempotencyScope   string
	IdempotencyKey     string
	ActorType          string
	ActorID            string
	SourceService      string
	RequestID          string
	CorrelationID      string
}

type TopUpRequest struct {
	UserID             string
	Credits            int64
	ExternalSourceType string
	ExternalSourceID   string
	CatalogID          string
	RequestFingerprint string
	IdempotencyScope   string
	IdempotencyKey     string
	ActorType          string
	ActorID            string
	SourceService      string
	RequestID          string
	CorrelationID      string
}

type TopUpResult struct {
	EntryID    string
	LotID      string
	DebtRepaid int64
	PaidAdded  int64
}

type SettlementIntent struct {
	Action             model.BillingSettlementAction
	UserID             string
	ResourceType       string
	ResourceID         string
	TaskID             string
	AttemptID          string
	ToolCallID         string
	ChargeID           string
	CatalogID          string
	SKUID              string
	Reason             string
	RequestFingerprint string
	IdempotencyScope   string
	IdempotencyKey     string
}

func NewBillingWalletService(repo repository.Repository, bundle *billing.Bundle, opts BillingWalletOptions) *BillingWalletService {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	eligible := opts.PromotionEligible
	if eligible == nil {
		eligible = func(model.BillingCreditLot, model.BillingSKU) bool { return true }
	}
	var snapshot billing.Bundle
	if bundle != nil {
		snapshot = *bundle
		snapshot.Products.SKUs = append([]billing.SKUConfig(nil), bundle.Products.SKUs...)
	}
	return &BillingWalletService{repo: repo, bundle: snapshot, now: now, promotionEligible: eligible}
}

func (s *BillingWalletService) ChargeTaskAdmission(ctx context.Context, req TaskChargeRequest) (*model.BillingCharge, error) {
	var result *model.BillingCharge
	err := s.withTx(ctx, func(tx repository.Repository) error {
		var err error
		result, err = s.ChargeTaskAdmissionInTx(ctx, tx, req)
		return err
	})
	if err != nil {
		if replay, replayErr := findTaskChargeReplay(ctx, s.repo.Billing(), req); replayErr != nil || replay != nil {
			return replay, replayErr
		}
		return nil, err
	}
	return result, nil
}

func (s *BillingWalletService) ChargeTaskAdmissionInTx(ctx context.Context, tx repository.Repository, req TaskChargeRequest) (*model.BillingCharge, error) {
	if strings.TrimSpace(req.UserID) == "" || strings.TrimSpace(req.TaskID) == "" || strings.TrimSpace(req.QuoteID) == "" ||
		strings.TrimSpace(req.CatalogID) == "" || strings.TrimSpace(req.SKUID) == "" || !validBillingFingerprint(req.RequestFingerprint) ||
		strings.TrimSpace(req.IdempotencyScope) == "" || strings.TrimSpace(req.IdempotencyKey) == "" || tx == nil {
		return nil, fmt.Errorf("%w: incomplete task charge identity", ErrBillingInvalid)
	}
	billingRepo := tx.Billing()
	if replay, replayErr := findTaskChargeReplay(ctx, billingRepo, req); replayErr != nil || replay != nil {
		return replay, replayErr
	}
	sku, err := billingRepo.FindSKU(ctx, req.CatalogID, req.SKUID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrBillingSKUNotFound
		}
		return nil, err
	}
	if sku.Policy != "task_admission" {
		return nil, fmt.Errorf("%w: SKU policy is %q", ErrBillingInvalid, sku.Policy)
	}
	expectedCharge := newTaskCharge(req, sku, s.now().UTC())
	if replay, replayErr := findExpectedChargeReplay(ctx, billingRepo, expectedCharge); replayErr != nil || replay != nil {
		return replay, replayErr
	}
	if _, findErr := billingRepo.FindChargeByTask(ctx, req.TaskID); findErr == nil {
		return nil, ErrBillingConflict
	} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return nil, findErr
	}
	account, lockErr := billingRepo.LockAccount(ctx, req.UserID)
	if lockErr != nil {
		return nil, lockErr
	}
	if replay, replayErr := findLockedTaskChargeReplay(ctx, billingRepo, expectedCharge); replayErr != nil || replay != nil {
		return replay, replayErr
	}
	quote, lockErr := billingRepo.LockQuote(ctx, req.QuoteID)
	if lockErr != nil {
		return nil, lockErr
	}
	now := s.now().UTC()
	if err := validateAdmissionQuote(quote, req.UserID, sku, req.RequestFingerprint, now); err != nil {
		return nil, err
	}
	if account.DebtCredits > 0 {
		return nil, ErrBillingDebtOutstanding
	}
	charge := newTaskCharge(req, sku, now)
	allocations, entries, paid, promotional, _, spendErr := s.consumeForCharge(ctx, billingRepo, account, *sku, charge, false)
	if spendErr != nil {
		if errors.Is(spendErr, errBillingSpendInsufficient) {
			return nil, ErrBillingInsufficientForTask
		}
		return nil, spendErr
	}
	charge.PaidCredits, charge.PromotionalCredits = paid, promotional
	if err := charge.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBillingInvalid, err)
	}
	consumed, consumeErr := billingRepo.MarkQuoteConsumed(ctx, quote.ID, now, "task", req.TaskID)
	if consumeErr != nil {
		return nil, consumeErr
	}
	if !consumed {
		return nil, ErrBillingQuoteConsumed
	}
	if err := billingRepo.CreateCharge(ctx, charge, allocations); err != nil {
		return nil, err
	}
	if err := appendWalletEntries(ctx, billingRepo, entries); err != nil {
		return nil, err
	}
	if err := updateBillingAccount(ctx, billingRepo, account); err != nil {
		return nil, err
	}
	return charge, nil
}

func (s *BillingWalletService) ChargeAcceptedOperation(ctx context.Context, req OperationChargeRequest) (*model.BillingCharge, error) {
	return s.chargeOperation(ctx, req, true)
}

func (s *BillingWalletService) ChargeStandaloneOperation(ctx context.Context, req OperationChargeRequest) (*model.BillingCharge, error) {
	return s.chargeOperation(ctx, req, false)
}

func (s *BillingWalletService) chargeOperation(ctx context.Context, req OperationChargeRequest, accepted bool) (*model.BillingCharge, error) {
	if strings.TrimSpace(req.UserID) == "" || strings.TrimSpace(req.CatalogID) == "" || strings.TrimSpace(req.SKUID) == "" ||
		strings.TrimSpace(req.ResourceType) == "" || strings.TrimSpace(req.ResourceID) == "" || !validBillingFingerprint(req.RequestFingerprint) ||
		strings.TrimSpace(req.IdempotencyScope) == "" || strings.TrimSpace(req.IdempotencyKey) == "" {
		return nil, fmt.Errorf("%w: incomplete operation charge identity", ErrBillingInvalid)
	}
	if accepted && (strings.TrimSpace(req.TaskID) == "" || strings.TrimSpace(req.AttemptID) == "" || strings.TrimSpace(req.ToolCallID) == "") {
		return nil, fmt.Errorf("%w: accepted operation identity is required", ErrBillingInvalid)
	}
	if !accepted && strings.TrimSpace(req.QuoteID) == "" {
		return nil, fmt.Errorf("%w: standalone operation quote is required", ErrBillingInvalid)
	}
	if replay, replayErr := findOperationChargeReplay(ctx, s.repo.Billing(), req, accepted); replayErr != nil || replay != nil {
		return replay, replayErr
	}
	sku, err := s.repo.Billing().FindSKU(ctx, req.CatalogID, req.SKUID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrBillingSKUNotFound
		}
		return nil, err
	}
	wantPolicy := "standalone_operation"
	if accepted {
		wantPolicy = "accepted_task_operation"
	}
	if sku.Policy != wantPolicy {
		return nil, fmt.Errorf("%w: SKU policy is %q", ErrBillingInvalid, sku.Policy)
	}
	var result *model.BillingCharge
	err = s.withTx(ctx, func(tx repository.Repository) error {
		billingRepo := tx.Billing()
		expectedCharge := newOperationCharge(req, sku, accepted, s.now().UTC())
		if replay, replayErr := findExpectedChargeReplay(ctx, billingRepo, expectedCharge); replayErr != nil || replay != nil {
			result = replay
			return replayErr
		}
		if accepted {
			if _, findErr := billingRepo.FindChargeByOperation(ctx, req.TaskID, req.AttemptID, req.ToolCallID, req.CatalogID, req.SKUID); findErr == nil {
				return ErrBillingConflict
			} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
				return findErr
			}
		}
		account, lockErr := billingRepo.LockAccount(ctx, req.UserID)
		if lockErr != nil {
			return lockErr
		}
		if replay, replayErr := findLockedOperationChargeReplay(ctx, billingRepo, expectedCharge, accepted); replayErr != nil || replay != nil {
			result = replay
			return replayErr
		}
		now := s.now().UTC()
		if !accepted {
			quote, quoteErr := billingRepo.LockQuote(ctx, req.QuoteID)
			if quoteErr != nil {
				return quoteErr
			}
			if err := validateAdmissionQuote(quote, req.UserID, sku, req.RequestFingerprint, now); err != nil {
				return err
			}
			if account.DebtCredits > 0 {
				return ErrBillingDebtOutstanding
			}
		}
		charge := newOperationCharge(req, sku, accepted, now)
		allocations, entries, paid, promotional, debt, spendErr := s.consumeForCharge(ctx, billingRepo, account, *sku, charge, accepted)
		if spendErr != nil {
			if accepted || !errors.Is(spendErr, errBillingSpendInsufficient) {
				return spendErr
			}
			return ErrBillingInsufficientForStandaloneOperation
		}
		charge.PaidCredits, charge.PromotionalCredits, charge.DebtCredits = paid, promotional, debt
		if err := charge.Validate(); err != nil {
			return fmt.Errorf("%w: %v", ErrBillingInvalid, err)
		}
		if !accepted {
			consumed, consumeErr := billingRepo.MarkQuoteConsumed(ctx, req.QuoteID, now, req.ResourceType, req.ResourceID)
			if consumeErr != nil {
				return consumeErr
			}
			if !consumed {
				return ErrBillingQuoteConsumed
			}
		}
		if err := billingRepo.CreateCharge(ctx, charge, allocations); err != nil {
			return err
		}
		if err := appendWalletEntries(ctx, billingRepo, entries); err != nil {
			return err
		}
		if err := updateBillingAccount(ctx, billingRepo, account); err != nil {
			return err
		}
		result = charge
		return nil
	})
	if err != nil {
		if replay, replayErr := findOperationChargeReplay(ctx, s.repo.Billing(), req, accepted); replayErr != nil || replay != nil {
			return replay, replayErr
		}
		return nil, err
	}
	return result, nil
}

func (s *BillingWalletService) consumeForCharge(ctx context.Context, repo repository.BillingRepository, account *model.BillingWalletAccount, sku model.BillingSKU, charge *model.BillingCharge, allowDebt bool) ([]model.BillingChargeAllocation, []*model.BillingWalletEntry, int64, int64, int64, error) {
	remaining := charge.PriceCredits
	allocations := make([]model.BillingChargeAllocation, 0)
	entries := make([]*model.BillingWalletEntry, 0)
	var paid, promotional int64
	now := s.now().UTC()
	type eligibleLot struct {
		kind model.BillingCreditLotKind
		lot  model.BillingCreditLot
	}
	eligible := make([]eligibleLot, 0)
	var available int64
	for _, kind := range []model.BillingCreditLotKind{model.BillingCreditLotKindPromotional, model.BillingCreditLotKindPaid} {
		lots, err := repo.ListSpendableLots(ctx, account.UserID, string(kind), now)
		if err != nil {
			return nil, nil, 0, 0, 0, err
		}
		for _, lot := range lots {
			if kind == model.BillingCreditLotKindPromotional && !s.promotionEligible(lot, sku) {
				continue
			}
			var ok bool
			if available, ok = checkedBillingAdd(available, lot.AvailableCredits); !ok {
				return nil, nil, 0, 0, 0, ErrBillingLedgerInvalid
			}
			eligible = append(eligible, eligibleLot{kind: kind, lot: lot})
		}
	}
	if !allowDebt && available < remaining {
		return nil, nil, 0, 0, 0, errBillingSpendInsufficient
	}
	for i := range eligible {
		if remaining == 0 {
			break
		}
		kind := eligible[i].kind
		lot := &eligible[i].lot
		used := minInt64(remaining, lot.AvailableCredits)
		if used <= 0 {
			continue
		}
		lot.AvailableCredits -= used
		lot.ConsumedCredits += used
		if err := lot.Validate(); err != nil {
			return nil, nil, 0, 0, 0, err
		}
		if err := repo.UpdateLot(ctx, lot); err != nil {
			return nil, nil, 0, 0, 0, err
		}
		allocation := model.BillingChargeAllocation{ID: uuid.NewString(), ChargeID: charge.ID, LotID: lot.ID, Credits: used, CreatedAt: now}
		allocations = append(allocations, allocation)
		entry := chargeEntry(account.UserID, charge, "lot:"+lot.ID, now)
		entry.LotID = stringPtr(lot.ID)
		if kind == model.BillingCreditLotKindPromotional {
			promotional += used
			account.PromotionalCredits -= used
			entry.PromotionalDelta = -used
		} else {
			paid += used
			account.PaidCredits -= used
			entry.PaidDelta = -used
		}
		entries = append(entries, entry)
		remaining -= used
	}
	if remaining > 0 {
		if !allowDebt {
			return nil, nil, 0, 0, 0, errBillingSpendInsufficient
		}
		if account.DebtCredits > math.MaxInt64-remaining {
			return nil, nil, 0, 0, 0, ErrBillingLedgerInvalid
		}
		account.DebtCredits += remaining
		entry := chargeEntry(account.UserID, charge, "debt", now)
		entry.EventKind = model.BillingWalletEventKindDebtCreated
		entry.DebtDelta = remaining
		entries = append(entries, entry)
	}
	return allocations, entries, paid, promotional, remaining, nil
}

func (s *BillingWalletService) TopUp(ctx context.Context, req TopUpRequest) (*TopUpResult, error) {
	var err error
	req, err = CanonicalTopUpRequest(req)
	if err != nil {
		return nil, err
	}
	var result *TopUpResult
	err = s.withTx(ctx, func(tx repository.Repository) error {
		var topUpErr error
		result, topUpErr = s.topUpInTx(ctx, tx, req)
		return topUpErr
	})
	if err != nil {
		if errors.Is(err, ErrBillingUserNotFound) {
			return nil, err
		}
		if replay, replayErr := findTopUpReplay(ctx, s.repo.Billing(), req); replayErr != nil || replay != nil {
			return replay, replayErr
		}
		if sourceEntry, sourceErr := s.repo.Billing().FindEntryBySource(ctx, req.ExternalSourceType, req.ExternalSourceID); sourceErr == nil {
			return topUpResultFromEntry(ctx, s.repo.Billing(), sourceEntry, req)
		}
		return nil, err
	}
	return result, nil
}

// TopUpInTx applies a top-up inside the caller's transaction. The caller owns
// commit or rollback, allowing top-up and acquisition rewards to be atomic.
func (s *BillingWalletService) TopUpInTx(ctx context.Context, tx repository.Repository, req TopUpRequest) (*TopUpResult, error) {
	if tx == nil {
		return nil, fmt.Errorf("%w: top-up transaction is required", ErrBillingInvalid)
	}
	var err error
	req, err = CanonicalTopUpRequest(req)
	if err != nil {
		return nil, err
	}
	return s.topUpInTx(ctx, tx, req)
}

func CanonicalTopUpRequest(req TopUpRequest) (TopUpRequest, error) {
	var ok bool
	if req.UserID, ok = canonicalBillingUUID(req.UserID); !ok || req.Credits <= 0 {
		return TopUpRequest{}, fmt.Errorf("%w: invalid top-up", ErrBillingInvalid)
	}
	if req.ExternalSourceType, ok = canonicalBillingText(req.ExternalSourceType, 40, true); !ok {
		return TopUpRequest{}, fmt.Errorf("%w: invalid top-up", ErrBillingInvalid)
	}
	if req.ExternalSourceID, ok = canonicalBillingText(req.ExternalSourceID, 128, true); !ok {
		return TopUpRequest{}, fmt.Errorf("%w: invalid top-up", ErrBillingInvalid)
	}
	if req.CatalogID, ok = canonicalBillingText(req.CatalogID, 128, true); !ok {
		return TopUpRequest{}, fmt.Errorf("%w: invalid top-up", ErrBillingInvalid)
	}
	req.RequestFingerprint = strings.TrimSpace(req.RequestFingerprint)
	if !validBillingFingerprint(req.RequestFingerprint) {
		return TopUpRequest{}, fmt.Errorf("%w: invalid top-up", ErrBillingInvalid)
	}
	if req.IdempotencyScope, ok = canonicalBillingText(req.IdempotencyScope, 80, true); !ok {
		return TopUpRequest{}, fmt.Errorf("%w: invalid top-up", ErrBillingInvalid)
	}
	if req.IdempotencyKey, ok = canonicalBillingText(req.IdempotencyKey, 128, true); !ok {
		return TopUpRequest{}, fmt.Errorf("%w: invalid top-up", ErrBillingInvalid)
	}
	if req.ActorType, ok = canonicalBillingText(req.ActorType, 40, false); !ok {
		return TopUpRequest{}, fmt.Errorf("%w: invalid top-up", ErrBillingInvalid)
	}
	if req.ActorID, ok = canonicalBillingText(req.ActorID, 128, false); !ok {
		return TopUpRequest{}, fmt.Errorf("%w: invalid top-up", ErrBillingInvalid)
	}
	if req.SourceService, ok = canonicalBillingText(req.SourceService, 80, false); !ok {
		return TopUpRequest{}, fmt.Errorf("%w: invalid top-up", ErrBillingInvalid)
	}
	if req.RequestID, ok = canonicalBillingText(req.RequestID, 128, false); !ok {
		return TopUpRequest{}, fmt.Errorf("%w: invalid top-up", ErrBillingInvalid)
	}
	if req.CorrelationID, ok = canonicalBillingText(req.CorrelationID, 128, false); !ok {
		return TopUpRequest{}, fmt.Errorf("%w: invalid top-up", ErrBillingInvalid)
	}
	return req, nil
}

func (s *BillingWalletService) topUpInTx(ctx context.Context, tx repository.Repository, req TopUpRequest) (*TopUpResult, error) {
	if _, err := tx.Users().LockByID(ctx, req.UserID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrBillingUserNotFound
		}
		return nil, err
	}
	return s.topUpInTxAfterUserLock(ctx, tx, req)
}

func (s *BillingWalletService) topUpInTxAfterUserLock(ctx context.Context, tx repository.Repository, req TopUpRequest) (*TopUpResult, error) {
	billingRepo := tx.Billing()
	if err := requirePublishedBillingCatalog(ctx, billingRepo, req.CatalogID); err != nil {
		return nil, err
	}
	account, err := lockOrCreateBillingAccount(ctx, billingRepo, req.UserID)
	if err != nil {
		return nil, err
	}
	if replay, replayErr := findLockedTopUpReplay(ctx, billingRepo, req); replayErr != nil || replay != nil {
		return replay, replayErr
	}
	now := s.now().UTC()
	debtPositions, err := billingRepo.ListOutstandingDebtCharges(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	var attributedDebt int64
	for _, position := range debtPositions {
		var ok bool
		if attributedDebt, ok = checkedBillingAdd(attributedDebt, position.OutstandingCredits); !ok {
			return nil, ErrBillingLedgerInvalid
		}
	}
	if attributedDebt != account.DebtCredits {
		return nil, ErrBillingLedgerInvalid
	}
	debtRepaid := minInt64(req.Credits, account.DebtCredits)
	paidAdded := req.Credits - debtRepaid
	entry := &model.BillingWalletEntry{
		ID: uuid.NewString(), UserID: req.UserID, EventKind: model.BillingWalletEventKindTopUp,
		PaidDelta: paidAdded, CatalogID: req.CatalogID, RequestFingerprint: req.RequestFingerprint,
		SourceType: stringPtr(req.ExternalSourceType), SourceID: stringPtr(req.ExternalSourceID),
		IdempotencyScope: req.IdempotencyScope, IdempotencyKey: req.IdempotencyKey,
		ActorType: req.ActorType, ActorID: req.ActorID, SourceService: req.SourceService,
		RequestID: req.RequestID, CorrelationID: req.CorrelationID, CreatedAt: now,
	}
	lotID := ""
	if paidAdded > 0 {
		lotID = uuid.NewString()
		lot := &model.BillingCreditLot{
			ID: lotID, UserID: req.UserID, Kind: model.BillingCreditLotKindPaid,
			SourceType: req.ExternalSourceType, SourceID: req.ExternalSourceID, CatalogID: req.CatalogID,
			OriginalCredits: paidAdded, AvailableCredits: paidAdded, CreatedAt: now,
		}
		if err := lot.Validate(); err != nil {
			return nil, err
		}
		if err := billingRepo.CreateLot(ctx, lot); err != nil {
			return nil, err
		}
		entry.LotID = &lotID
	}
	account.DebtCredits -= debtRepaid
	if account.PaidCredits > math.MaxInt64-paidAdded {
		return nil, ErrBillingLedgerInvalid
	}
	account.PaidCredits += paidAdded
	if err := billingRepo.AppendEntry(ctx, entry); err != nil {
		return nil, err
	}
	remainingRepayment := debtRepaid
	for index, position := range debtPositions {
		if remainingRepayment == 0 {
			break
		}
		repaid := minInt64(remainingRepayment, position.OutstandingCredits)
		if repaid == 0 {
			continue
		}
		repayment := &model.BillingWalletEntry{
			ID: uuid.NewString(), UserID: req.UserID, EventKind: model.BillingWalletEventKindDebtRepayment,
			DebtDelta: -repaid, CatalogID: req.CatalogID, RequestFingerprint: req.RequestFingerprint,
			ChargeID: &position.ChargeID, ResourceType: "topup", ResourceID: entry.ID,
			IdempotencyScope: "topup-repayment:" + entry.ID,
			IdempotencyKey:   fmt.Sprintf("%06d:%s", index, position.ChargeID),
			ActorType:        req.ActorType, ActorID: req.ActorID, SourceService: req.SourceService,
			RequestID: req.RequestID, CorrelationID: req.CorrelationID, CreatedAt: now,
		}
		if err := billingRepo.AppendEntry(ctx, repayment); err != nil {
			return nil, err
		}
		if err := billingRepo.CreateDebtAllocation(ctx, &model.BillingDebtAllocation{
			ID: uuid.NewString(), UserID: req.UserID, ChargeID: position.ChargeID, EntryID: repayment.ID,
			SourceEntryID: entry.ID, Kind: model.BillingDebtAllocationKindRepayment, Credits: repaid, CreatedAt: now,
		}); err != nil {
			return nil, err
		}
		remainingRepayment -= repaid
	}
	if remainingRepayment != 0 {
		return nil, ErrBillingLedgerInvalid
	}
	if err := updateBillingAccount(ctx, billingRepo, account); err != nil {
		return nil, err
	}
	return &TopUpResult{EntryID: entry.ID, LotID: lotID, DebtRepaid: debtRepaid, PaidAdded: paidAdded}, nil
}

func requirePublishedBillingCatalog(ctx context.Context, repo repository.BillingRepository, catalogID string) error {
	catalog, err := repo.FindCatalogVersion(ctx, catalogID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrBillingCatalogNotFound
	}
	if err != nil {
		return err
	}
	if catalog.Status != "published" {
		return ErrBillingCatalogNotFound
	}
	return nil
}

func (s *BillingWalletService) Reverse(ctx context.Context, chargeID, reason, key string) (*model.BillingCharge, error) {
	chargeID, reason, key = strings.TrimSpace(chargeID), strings.TrimSpace(reason), strings.TrimSpace(key)
	if chargeID == "" || reason == "" || key == "" {
		return nil, fmt.Errorf("%w: reversal charge, reason, and key are required", ErrBillingInvalid)
	}
	if !taskReversalReasonAllowed(s.bundle.Policy.TaskFailureReversal, reason) {
		return nil, ErrBillingReversalNotAllowed
	}
	fingerprint := billingFingerprint(chargeID, reason)
	var result *model.BillingCharge
	err := s.withTx(ctx, func(tx repository.Repository) error {
		billingRepo := tx.Billing()
		original, err := billingRepo.FindChargeByID(ctx, chargeID)
		if err != nil {
			return err
		}
		if original.Kind != model.BillingChargeKindTask || original.Policy != "task_admission" ||
			original.Status != model.BillingChargeStatusPosted || original.DebtCredits != 0 {
			return ErrBillingReversalNotAllowed
		}
		if replay, replayErr := findReversalChargeReplay(ctx, billingRepo, original, reason, key); replayErr != nil || replay != nil {
			result = replay
			return replayErr
		}
		if _, err := billingRepo.FindReversal(ctx, chargeID); err == nil {
			return ErrBillingConflict
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		account, err := billingRepo.LockAccount(ctx, original.UserID)
		if err != nil {
			return err
		}
		expectedReversal := newReversalCharge(original, key, fingerprint, s.now().UTC())
		if replay, replayErr := findLockedReversalChargeReplay(ctx, billingRepo, expectedReversal); replayErr != nil || replay != nil {
			result = replay
			return replayErr
		}
		allocations, err := billingRepo.ListChargeAllocations(ctx, original.ID)
		if err != nil {
			return err
		}
		sortChargeAllocationsForLocking(allocations)
		now := s.now().UTC()
		reversal := newReversalCharge(original, key, fingerprint, now)
		entries := make([]*model.BillingWalletEntry, 0, len(allocations)+1)
		reversalAllocations := make([]model.BillingChargeAllocation, 0, len(allocations))
		for _, allocation := range allocations {
			lot, lockErr := billingRepo.LockLotByID(ctx, allocation.LotID)
			if lockErr != nil {
				return lockErr
			}
			if lot.UserID != original.UserID || lot.ConsumedCredits < allocation.Credits {
				return ErrBillingLedgerInvalid
			}
			lot.ConsumedCredits -= allocation.Credits
			entry := reversalEntry(original.UserID, reversal, "lot:"+lot.ID, now)
			entry.LotID = stringPtr(lot.ID)
			switch lot.Kind {
			case model.BillingCreditLotKindPaid:
				if lot.AvailableCredits > math.MaxInt64-allocation.Credits || account.PaidCredits > math.MaxInt64-allocation.Credits {
					return ErrBillingLedgerInvalid
				}
				lot.AvailableCredits += allocation.Credits
				account.PaidCredits += allocation.Credits
				entry.PaidDelta = allocation.Credits
			case model.BillingCreditLotKindPromotional:
				if lot.ExpiresAt != nil && !lot.ExpiresAt.After(now) {
					if lot.ExpiredCredits > math.MaxInt64-allocation.Credits {
						return ErrBillingLedgerInvalid
					}
					lot.ExpiredCredits += allocation.Credits
				} else {
					if lot.AvailableCredits > math.MaxInt64-allocation.Credits || account.PromotionalCredits > math.MaxInt64-allocation.Credits {
						return ErrBillingLedgerInvalid
					}
					lot.AvailableCredits += allocation.Credits
					account.PromotionalCredits += allocation.Credits
					entry.PromotionalDelta = allocation.Credits
				}
			default:
				return ErrBillingLedgerInvalid
			}
			if err := lot.Validate(); err != nil {
				return fmt.Errorf("%w: %v", ErrBillingLedgerInvalid, err)
			}
			if err := billingRepo.UpdateLot(ctx, lot); err != nil {
				return err
			}
			entries = append(entries, entry)
			reversalAllocations = append(reversalAllocations, model.BillingChargeAllocation{
				ID: uuid.NewString(), ChargeID: reversal.ID, LotID: lot.ID, Credits: allocation.Credits, CreatedAt: now,
			})
		}
		if err := reversal.Validate(); err != nil {
			return err
		}
		if err := billingRepo.CreateCharge(ctx, reversal, reversalAllocations); err != nil {
			return err
		}
		if err := appendWalletEntries(ctx, billingRepo, entries); err != nil {
			return err
		}
		if err := updateBillingAccount(ctx, billingRepo, account); err != nil {
			return err
		}
		result = reversal
		return nil
	})
	if err != nil {
		original, findErr := s.repo.Billing().FindChargeByID(ctx, chargeID)
		if findErr == nil {
			if replay, replayErr := findReversalChargeReplay(ctx, s.repo.Billing(), original, reason, key); replayErr != nil || replay != nil {
				return replay, replayErr
			}
		}
		return nil, err
	}
	return result, nil
}

func (s *BillingWalletService) RebuildProjection(ctx context.Context, userID string) (*model.BillingWalletAccount, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, fmt.Errorf("%w: user is required", ErrBillingInvalid)
	}
	var result *model.BillingWalletAccount
	err := s.withTx(ctx, func(tx repository.Repository) error {
		billingRepo := tx.Billing()
		account, err := billingRepo.LockAccount(ctx, userID)
		if err != nil {
			return err
		}
		var paid, promotional, debt int64
		for offset := 0; ; offset += 500 {
			entries, listErr := billingRepo.ListEntriesByUser(ctx, userID, offset, 500)
			if listErr != nil {
				return listErr
			}
			for _, entry := range entries {
				var ok bool
				if paid, ok = checkedBillingAdd(paid, entry.PaidDelta); !ok {
					return ErrBillingLedgerInvalid
				}
				if promotional, ok = checkedBillingAdd(promotional, entry.PromotionalDelta); !ok {
					return ErrBillingLedgerInvalid
				}
				if debt, ok = checkedBillingAdd(debt, entry.DebtDelta); !ok {
					return ErrBillingLedgerInvalid
				}
			}
			if len(entries) < 500 {
				break
			}
		}
		if paid < 0 || promotional < 0 || debt < 0 {
			return ErrBillingLedgerInvalid
		}
		account.PaidCredits, account.PromotionalCredits, account.DebtCredits = paid, promotional, debt
		if err := updateBillingAccount(ctx, billingRepo, account); err != nil {
			return err
		}
		result = account
		return nil
	})
	return result, err
}

func (s *BillingWalletService) ExpirePromotionalCredits(ctx context.Context, now time.Time, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	now = now.UTC()
	var candidates []model.BillingCreditLot
	// Release the global candidate locks before taking any account lock. Each
	// mutation below re-locks account then lot and rechecks the expiry predicate.
	if err := s.withTx(ctx, func(tx repository.Repository) error {
		var err error
		candidates, err = tx.Billing().ListExpiredPromotionalLotsForUpdate(ctx, now, limit)
		return err
	}); err != nil {
		return 0, err
	}
	expiredCount := 0
	for _, candidate := range candidates {
		candidate := candidate
		applied := false
		err := s.withTx(ctx, func(tx repository.Repository) error {
			billingRepo := tx.Billing()
			if _, err := billingRepo.FindEntryByKey(ctx, "expiry", candidate.ID); err == nil {
				return nil
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			account, err := billingRepo.LockAccount(ctx, candidate.UserID)
			if err != nil {
				return err
			}
			lot, err := billingRepo.LockLotByID(ctx, candidate.ID)
			if err != nil {
				return err
			}
			if lot.UserID != account.UserID || lot.Kind != model.BillingCreditLotKindPromotional || lot.AvailableCredits <= 0 ||
				lot.ExpiresAt == nil || lot.ExpiresAt.After(now) {
				return nil
			}
			credits := lot.AvailableCredits
			if account.PromotionalCredits < credits || lot.ExpiredCredits > math.MaxInt64-credits {
				return ErrBillingLedgerInvalid
			}
			lot.AvailableCredits = 0
			lot.ExpiredCredits += credits
			account.PromotionalCredits -= credits
			if err := lot.Validate(); err != nil {
				return fmt.Errorf("%w: %v", ErrBillingLedgerInvalid, err)
			}
			if err := billingRepo.UpdateLot(ctx, lot); err != nil {
				return err
			}
			entry := &model.BillingWalletEntry{
				ID: uuid.NewString(), UserID: account.UserID, EventKind: model.BillingWalletEventKindExpiry,
				PromotionalDelta: -credits, LotID: &lot.ID, ResourceType: "credit_lot", ResourceID: lot.ID,
				CatalogID: lot.CatalogID, RequestFingerprint: billingFingerprint("expiry", lot.ID),
				IdempotencyScope: "expiry", IdempotencyKey: lot.ID, CreatedAt: now,
			}
			if err := billingRepo.AppendEntry(ctx, entry); err != nil {
				return err
			}
			if err := updateBillingAccount(ctx, billingRepo, account); err != nil {
				return err
			}
			applied = true
			return nil
		})
		if err != nil {
			return expiredCount, err
		}
		if applied {
			expiredCount++
		}
	}
	return expiredCount, nil
}

func (s *BillingWalletService) EnqueueSettlementInTx(ctx context.Context, tx repository.Repository, req SettlementIntent) (*model.BillingSettlementOutbox, error) {
	if tx == nil || strings.TrimSpace(req.ResourceType) == "" || strings.TrimSpace(req.ResourceID) == "" ||
		strings.TrimSpace(req.IdempotencyScope) == "" || strings.TrimSpace(req.IdempotencyKey) == "" || !validBillingFingerprint(req.RequestFingerprint) {
		return nil, fmt.Errorf("%w: invalid settlement intent", ErrBillingInvalid)
	}
	switch req.Action {
	case model.BillingSettlementActionChargeOperation:
		if strings.TrimSpace(req.TaskID) == "" || strings.TrimSpace(req.AttemptID) == "" || strings.TrimSpace(req.ToolCallID) == "" ||
			strings.TrimSpace(req.CatalogID) == "" || strings.TrimSpace(req.SKUID) == "" {
			return nil, fmt.Errorf("%w: incomplete operation settlement", ErrBillingInvalid)
		}
	case model.BillingSettlementActionReverseTask:
		if strings.TrimSpace(req.ChargeID) == "" || strings.TrimSpace(req.Reason) == "" {
			return nil, fmt.Errorf("%w: reversal settlement requires charge and reason", ErrBillingInvalid)
		}
	default:
		return nil, fmt.Errorf("%w: unsupported settlement action %q", ErrBillingInvalid, req.Action)
	}
	billingRepo := tx.Billing()
	existing, err := billingRepo.FindSettlementByKey(ctx, req.IdempotencyScope, req.IdempotencyKey)
	if err == nil {
		if !sameSettlementIntent(existing, req) {
			return nil, ErrBillingConflict
		}
		return existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	now := s.now().UTC()
	settlement := &model.BillingSettlementOutbox{
		ID: uuid.NewString(), Action: req.Action, ResourceType: req.ResourceType, ResourceID: req.ResourceID,
		CatalogID: req.CatalogID, SKUID: req.SKUID, IdempotencyScope: req.IdempotencyScope,
		IdempotencyKey: req.IdempotencyKey, Reason: req.Reason, Status: "pending", RequestFingerprint: req.RequestFingerprint,
		CreatedAt: now, UpdatedAt: now,
	}
	if req.TaskID != "" {
		settlement.TaskID = stringPtr(req.TaskID)
	}
	if req.AttemptID != "" {
		settlement.AttemptID = stringPtr(req.AttemptID)
	}
	if req.ToolCallID != "" {
		settlement.ToolCallID = stringPtr(req.ToolCallID)
	}
	if req.ChargeID != "" {
		settlement.ChargeID = stringPtr(req.ChargeID)
	}
	if err := billingRepo.EnqueueSettlement(ctx, settlement); err != nil {
		return nil, err
	}
	return settlement, nil
}

func (s *BillingWalletService) ProcessSettlementOutbox(ctx context.Context, limit int) (int, error) {
	processed, _, err := s.processSettlementOutboxBatch(ctx, limit)
	return processed, err
}

func (s *BillingWalletService) processSettlementOutboxBatch(ctx context.Context, limit int) (int, int, error) {
	if limit <= 0 {
		return 0, 0, nil
	}
	now := s.now().UTC()
	var claimed []model.BillingSettlementOutbox
	if err := s.withTx(ctx, func(tx repository.Repository) error {
		var err error
		claimed, err = tx.Billing().ClaimSettlements(ctx, now, limit)
		return err
	}); err != nil {
		return 0, 0, err
	}
	processed := 0
	for _, settlement := range claimed {
		settleErr := s.applySettlement(ctx, settlement)
		if settleErr != nil {
			if errors.Is(settleErr, context.Canceled) || errors.Is(settleErr, context.DeadlineExceeded) {
				return processed, len(claimed), settleErr
			}
			if isRetryableBillingDBError(settleErr) {
				retryAt := now.Add(settlementRetryDelay(settlement.Attempts))
				markErr := s.withTx(ctx, func(tx repository.Repository) error {
					return tx.Billing().MarkSettlementRetry(ctx, settlement.ID, settlement.Attempts, retryAt, settleErr.Error())
				})
				if errors.Is(markErr, gorm.ErrRecordNotFound) {
					continue
				}
				if markErr != nil {
					return processed, len(claimed), markErr
				}
				continue
			}
			markErr := s.withTx(ctx, func(tx repository.Repository) error {
				return tx.Billing().MarkSettlementFailed(ctx, settlement.ID, settlement.Attempts, s.now().UTC(), settleErr.Error())
			})
			if errors.Is(markErr, gorm.ErrRecordNotFound) {
				continue
			}
			if markErr != nil {
				return processed, len(claimed), markErr
			}
			processed++
			continue
		}
		markErr := s.withTx(ctx, func(tx repository.Repository) error {
			return tx.Billing().MarkSettlementProcessed(ctx, settlement.ID, settlement.Attempts, s.now().UTC())
		})
		if errors.Is(markErr, gorm.ErrRecordNotFound) {
			continue
		}
		if markErr != nil {
			return processed, len(claimed), markErr
		}
		processed++
	}
	return processed, len(claimed), nil
}

func (s *BillingWalletService) applySettlement(ctx context.Context, settlement model.BillingSettlementOutbox) error {
	switch settlement.Action {
	case model.BillingSettlementActionChargeOperation:
		if settlement.TaskID == nil || settlement.AttemptID == nil || settlement.ToolCallID == nil {
			return ErrBillingInvalid
		}
		task, err := s.repo.Tasks().FindByID(ctx, *settlement.TaskID)
		if err != nil {
			return err
		}
		_, err = s.ChargeAcceptedOperation(ctx, OperationChargeRequest{
			UserID: task.UserID, CatalogID: settlement.CatalogID, SKUID: settlement.SKUID,
			TaskID: *settlement.TaskID, AttemptID: *settlement.AttemptID, ToolCallID: *settlement.ToolCallID,
			ResourceType: settlement.ResourceType, ResourceID: settlement.ResourceID,
			RequestFingerprint: settlement.RequestFingerprint,
			IdempotencyScope:   "outbox-charge", IdempotencyKey: settlement.ID,
		})
		return err
	case model.BillingSettlementActionReverseTask:
		if settlement.ChargeID == nil {
			return ErrBillingInvalid
		}
		_, err := s.Reverse(ctx, *settlement.ChargeID, settlement.Reason, settlement.ID)
		return err
	default:
		return ErrBillingInvalid
	}
}

func sameSettlementIntent(existing *model.BillingSettlementOutbox, req SettlementIntent) bool {
	if existing == nil || existing.Action != req.Action || existing.ResourceType != req.ResourceType || existing.ResourceID != req.ResourceID ||
		existing.CatalogID != req.CatalogID || existing.SKUID != req.SKUID || existing.Reason != req.Reason ||
		existing.RequestFingerprint != req.RequestFingerprint {
		return false
	}
	return optionalStringEqual(existing.TaskID, req.TaskID) && optionalStringEqual(existing.AttemptID, req.AttemptID) &&
		optionalStringEqual(existing.ToolCallID, req.ToolCallID) && optionalStringEqual(existing.ChargeID, req.ChargeID)
}

func optionalStringEqual(value *string, expected string) bool {
	if expected == "" {
		return value == nil
	}
	return value != nil && *value == expected
}

func settlementRetryDelay(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	if attempts > 10 {
		attempts = 10
	}
	return time.Duration(1<<uint(attempts-1)) * time.Second
}

func validateAdmissionQuote(quote *model.BillingQuote, userID string, sku *model.BillingSKU, fingerprint string, now time.Time) error {
	if quote == nil || sku == nil || quote.UserID != userID || quote.CatalogID != sku.CatalogID || quote.SKUID != sku.SKUID ||
		quote.PriceCredits != sku.PriceCredits || quote.RequestFingerprint != fingerprint || string(quote.SKUSnapshot) != string(sku.Snapshot) {
		return ErrBillingQuoteMismatch
	}
	if quote.ConsumedAt != nil {
		return ErrBillingQuoteConsumed
	}
	if !quote.ExpiresAt.After(now) {
		return ErrBillingQuoteExpired
	}
	return nil
}

func newTaskCharge(req TaskChargeRequest, sku *model.BillingSKU, now time.Time) *model.BillingCharge {
	taskID, quoteID := req.TaskID, req.QuoteID
	return &model.BillingCharge{
		ID: uuid.NewString(), UserID: req.UserID, CatalogID: sku.CatalogID, SKUID: sku.SKUID, QuoteID: &quoteID,
		ResourceType: "task", ResourceID: req.TaskID, Kind: model.BillingChargeKindTask, Policy: sku.Policy,
		Status: model.BillingChargeStatusPosted, PriceCredits: sku.PriceCredits, TaskID: &taskID,
		IdempotencyScope: req.IdempotencyScope, IdempotencyKey: req.IdempotencyKey, RequestFingerprint: req.RequestFingerprint,
		ActorType: req.ActorType, ActorID: req.ActorID, SourceService: req.SourceService,
		RequestID: req.RequestID, CorrelationID: req.CorrelationID, CreatedAt: now,
	}
}

func newOperationCharge(req OperationChargeRequest, sku *model.BillingSKU, accepted bool, now time.Time) *model.BillingCharge {
	charge := &model.BillingCharge{
		ID: uuid.NewString(), UserID: req.UserID, CatalogID: sku.CatalogID, SKUID: sku.SKUID,
		ResourceType: req.ResourceType, ResourceID: req.ResourceID, Kind: model.BillingChargeKindOperation,
		Policy: sku.Policy, Status: model.BillingChargeStatusPosted, PriceCredits: sku.PriceCredits,
		IdempotencyScope: req.IdempotencyScope, IdempotencyKey: req.IdempotencyKey, RequestFingerprint: req.RequestFingerprint,
		ActorType: req.ActorType, ActorID: req.ActorID, SourceService: req.SourceService,
		RequestID: req.RequestID, CorrelationID: req.CorrelationID, CreatedAt: now,
	}
	if accepted {
		charge.OperationTaskID, charge.AttemptID, charge.ToolCallID = stringPtr(req.TaskID), stringPtr(req.AttemptID), stringPtr(req.ToolCallID)
	} else {
		charge.QuoteID = stringPtr(req.QuoteID)
	}
	return charge
}

func taskReversalReasonAllowed(policy billing.TaskFailureReversalPolicy, reason string) bool {
	if !policy.Enabled {
		return false
	}
	for _, allowed := range policy.Reasons {
		if reason == allowed {
			return true
		}
	}
	return false
}

func newReversalCharge(original *model.BillingCharge, key, fingerprint string, now time.Time) *model.BillingCharge {
	return &model.BillingCharge{
		ID: uuid.NewString(), UserID: original.UserID, CatalogID: original.CatalogID, SKUID: original.SKUID,
		ResourceType: original.ResourceType, ResourceID: original.ResourceID,
		Kind: model.BillingChargeKindReversal, Policy: "reversal", Status: model.BillingChargeStatusPosted,
		PriceCredits: original.PriceCredits, PaidCredits: original.PaidCredits, PromotionalCredits: original.PromotionalCredits,
		ReversalOfID: &original.ID, IdempotencyScope: "reversal", IdempotencyKey: key,
		RequestFingerprint: fingerprint, CreatedAt: now,
	}
}

func chargeEntry(userID string, charge *model.BillingCharge, key string, now time.Time) *model.BillingWalletEntry {
	return &model.BillingWalletEntry{
		ID: uuid.NewString(), UserID: userID, EventKind: model.BillingWalletEventKindCharge, ChargeID: &charge.ID,
		CatalogID: charge.CatalogID, RequestFingerprint: charge.RequestFingerprint,
		ResourceType: charge.ResourceType, ResourceID: charge.ResourceID,
		IdempotencyScope: "charge:" + charge.ID, IdempotencyKey: key,
		ActorType: charge.ActorType, ActorID: charge.ActorID, SourceService: charge.SourceService,
		RequestID: charge.RequestID, CorrelationID: charge.CorrelationID, CreatedAt: now,
	}
}

func reversalEntry(userID string, charge *model.BillingCharge, key string, now time.Time) *model.BillingWalletEntry {
	entry := chargeEntry(userID, charge, key, now)
	entry.EventKind = model.BillingWalletEventKindReversal
	entry.IdempotencyScope = "reversal:" + charge.ID
	return entry
}

func findChargeReplay(ctx context.Context, repo repository.BillingRepository, scope, key, fingerprint string) (*model.BillingCharge, error) {
	existing, err := repo.FindChargeByKey(ctx, scope, key)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if existing.RequestFingerprint != fingerprint {
		return nil, ErrBillingConflict
	}
	return existing, nil
}

func findExpectedChargeReplay(ctx context.Context, repo repository.BillingRepository, expected *model.BillingCharge) (*model.BillingCharge, error) {
	existing, err := repo.FindChargeByKey(ctx, expected.IdempotencyScope, expected.IdempotencyKey)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !sameChargeImmutableIdentity(existing, expected) {
		return nil, ErrBillingConflict
	}
	return existing, nil
}

func findTaskChargeReplay(ctx context.Context, repo repository.BillingRepository, req TaskChargeRequest) (*model.BillingCharge, error) {
	existing, err := repo.FindChargeByKey(ctx, req.IdempotencyScope, req.IdempotencyKey)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	expected := newTaskCharge(req, &model.BillingSKU{
		CatalogID: req.CatalogID, SKUID: req.SKUID, PriceCredits: existing.PriceCredits, Policy: "task_admission",
	}, existing.CreatedAt)
	if !sameChargeImmutableIdentity(existing, expected) {
		return nil, ErrBillingConflict
	}
	return existing, nil
}

func findOperationChargeReplay(ctx context.Context, repo repository.BillingRepository, req OperationChargeRequest, accepted bool) (*model.BillingCharge, error) {
	existing, err := repo.FindChargeByKey(ctx, req.IdempotencyScope, req.IdempotencyKey)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	policy := "standalone_operation"
	if accepted {
		policy = "accepted_task_operation"
	}
	expected := newOperationCharge(req, &model.BillingSKU{
		CatalogID: req.CatalogID, SKUID: req.SKUID, PriceCredits: existing.PriceCredits, Policy: policy,
	}, accepted, existing.CreatedAt)
	if !sameChargeImmutableIdentity(existing, expected) {
		return nil, ErrBillingConflict
	}
	return existing, nil
}

func findReversalChargeReplay(ctx context.Context, repo repository.BillingRepository, original *model.BillingCharge, reason, key string) (*model.BillingCharge, error) {
	expected := newReversalCharge(original, key, billingFingerprint(original.ID, reason), original.CreatedAt)
	return findExpectedChargeReplay(ctx, repo, expected)
}

func findLockedTaskChargeReplay(ctx context.Context, repo repository.BillingRepository, expected *model.BillingCharge) (*model.BillingCharge, error) {
	byKey, keyErr := repo.LockChargeByKey(ctx, expected.IdempotencyScope, expected.IdempotencyKey)
	byTask, taskErr := repo.LockChargeByTask(ctx, *expected.TaskID)
	return resolveLockedChargeReplay(byKey, keyErr, byTask, taskErr, expected)
}

func findLockedOperationChargeReplay(ctx context.Context, repo repository.BillingRepository, expected *model.BillingCharge, accepted bool) (*model.BillingCharge, error) {
	byKey, keyErr := repo.LockChargeByKey(ctx, expected.IdempotencyScope, expected.IdempotencyKey)
	var byIdentity *model.BillingCharge
	var identityErr error
	if accepted {
		byIdentity, identityErr = repo.LockChargeByOperation(ctx, *expected.OperationTaskID, *expected.AttemptID, *expected.ToolCallID, expected.CatalogID, expected.SKUID)
	} else {
		byIdentity, identityErr = repo.LockChargeByQuote(ctx, *expected.QuoteID)
	}
	return resolveLockedChargeReplay(byKey, keyErr, byIdentity, identityErr, expected)
}

func findLockedReversalChargeReplay(ctx context.Context, repo repository.BillingRepository, expected *model.BillingCharge) (*model.BillingCharge, error) {
	byKey, keyErr := repo.LockChargeByKey(ctx, expected.IdempotencyScope, expected.IdempotencyKey)
	byOriginal, originalErr := repo.LockReversal(ctx, *expected.ReversalOfID)
	return resolveLockedChargeReplay(byKey, keyErr, byOriginal, originalErr, expected)
}

func resolveLockedChargeReplay(byKey *model.BillingCharge, keyErr error, byIdentity *model.BillingCharge, identityErr error, expected *model.BillingCharge) (*model.BillingCharge, error) {
	keyFound, err := billingCurrentReadFound(keyErr)
	if err != nil {
		return nil, err
	}
	identityFound, err := billingCurrentReadFound(identityErr)
	if err != nil {
		return nil, err
	}
	if !keyFound && !identityFound {
		return nil, nil
	}
	if !keyFound || !identityFound || byKey == nil || byIdentity == nil || byKey.ID != byIdentity.ID ||
		!sameChargeImmutableIdentity(byKey, expected) || !sameChargeImmutableIdentity(byIdentity, expected) {
		return nil, ErrBillingConflict
	}
	return byKey, nil
}

func billingCurrentReadFound(err error) (bool, error) {
	if err == nil {
		return true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return false, err
}

func sameChargeImmutableIdentity(left, right *model.BillingCharge) bool {
	return left != nil && right != nil &&
		left.UserID == right.UserID && left.CatalogID == right.CatalogID && left.SKUID == right.SKUID &&
		optionalStringPointersEqual(left.QuoteID, right.QuoteID) && left.ResourceType == right.ResourceType && left.ResourceID == right.ResourceID &&
		left.Kind == right.Kind && left.Policy == right.Policy && left.Status == right.Status && left.PriceCredits == right.PriceCredits &&
		optionalStringPointersEqual(left.TaskID, right.TaskID) && optionalStringPointersEqual(left.OperationTaskID, right.OperationTaskID) &&
		optionalStringPointersEqual(left.AttemptID, right.AttemptID) && optionalStringPointersEqual(left.ToolCallID, right.ToolCallID) &&
		left.IdempotencyScope == right.IdempotencyScope && left.IdempotencyKey == right.IdempotencyKey &&
		optionalStringPointersEqual(left.ReversalOfID, right.ReversalOfID) && left.RequestFingerprint == right.RequestFingerprint &&
		left.ActorType == right.ActorType && left.ActorID == right.ActorID && left.SourceService == right.SourceService &&
		left.RequestID == right.RequestID && left.CorrelationID == right.CorrelationID
}

func optionalStringPointersEqual(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func findTopUpReplay(ctx context.Context, repo repository.BillingRepository, req TopUpRequest) (*TopUpResult, error) {
	entry, err := repo.FindEntryByKey(ctx, req.IdempotencyScope, req.IdempotencyKey)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return topUpResultFromEntry(ctx, repo, entry, req)
}

func findLockedTopUpReplay(ctx context.Context, repo repository.BillingRepository, req TopUpRequest) (*TopUpResult, error) {
	byKey, keyErr := repo.LockEntryByKey(ctx, req.IdempotencyScope, req.IdempotencyKey)
	bySource, sourceErr := repo.LockEntryBySource(ctx, req.ExternalSourceType, req.ExternalSourceID)
	keyFound, err := billingCurrentReadFound(keyErr)
	if err != nil {
		return nil, err
	}
	sourceFound, err := billingCurrentReadFound(sourceErr)
	if err != nil {
		return nil, err
	}
	if !keyFound && !sourceFound {
		return nil, nil
	}
	if !keyFound || !sourceFound || byKey == nil || bySource == nil || byKey.ID != bySource.ID {
		return nil, ErrBillingConflict
	}
	allocations, err := repo.LockDebtAllocationsBySourceEntryID(ctx, byKey.UserID, byKey.ID)
	if err != nil {
		return nil, err
	}
	var debtRepaid int64
	for _, allocation := range allocations {
		var ok bool
		if allocation.UserID != byKey.UserID || allocation.SourceEntryID != byKey.ID || allocation.Credits <= 0 {
			return nil, ErrBillingLedgerInvalid
		}
		if debtRepaid, ok = checkedBillingAdd(debtRepaid, allocation.Credits); !ok {
			return nil, ErrBillingLedgerInvalid
		}
	}
	return topUpResultFromEntryWithDebt(byKey, req, debtRepaid)
}

func topUpResultFromEntry(ctx context.Context, repo repository.BillingRepository, entry *model.BillingWalletEntry, req TopUpRequest) (*TopUpResult, error) {
	if !topUpEntryMatchesRequest(entry, req) {
		return nil, ErrBillingConflict
	}
	debtRepaid, err := repo.SumDebtAllocationsBySourceEntryID(ctx, entry.UserID, entry.ID)
	if err != nil {
		return nil, err
	}
	return topUpResultFromEntryWithDebt(entry, req, debtRepaid)
}

func topUpResultFromEntryWithDebt(entry *model.BillingWalletEntry, req TopUpRequest, debtRepaid int64) (*TopUpResult, error) {
	if !topUpEntryMatchesRequest(entry, req) {
		return nil, ErrBillingConflict
	}
	if debtRepaid < 0 {
		return nil, ErrBillingLedgerInvalid
	}
	credits, ok := checkedBillingAdd(entry.PaidDelta, debtRepaid)
	if !ok || credits != req.Credits {
		return nil, ErrBillingConflict
	}
	lotID := ""
	if entry.LotID != nil {
		lotID = *entry.LotID
	}
	return &TopUpResult{EntryID: entry.ID, LotID: lotID, DebtRepaid: debtRepaid, PaidAdded: entry.PaidDelta}, nil
}

func topUpEntryMatchesRequest(entry *model.BillingWalletEntry, req TopUpRequest) bool {
	return entry != nil && entry.UserID == req.UserID && entry.EventKind == model.BillingWalletEventKindTopUp && entry.PaidDelta >= 0 && entry.DebtDelta == 0 &&
		entry.CatalogID == req.CatalogID && entry.RequestFingerprint == req.RequestFingerprint &&
		entry.IdempotencyScope == req.IdempotencyScope && entry.IdempotencyKey == req.IdempotencyKey &&
		entry.ActorType == req.ActorType && entry.ActorID == req.ActorID && entry.SourceService == req.SourceService &&
		entry.RequestID == req.RequestID && entry.CorrelationID == req.CorrelationID &&
		entry.SourceType != nil && *entry.SourceType == req.ExternalSourceType && entry.SourceID != nil && *entry.SourceID == req.ExternalSourceID
}

func lockOrCreateBillingAccount(ctx context.Context, repo repository.BillingRepository, userID string) (*model.BillingWalletAccount, error) {
	if err := repo.EnsureAccount(ctx, userID); err != nil {
		return nil, err
	}
	return repo.LockAccount(ctx, userID)
}

func appendWalletEntries(ctx context.Context, repo repository.BillingRepository, entries []*model.BillingWalletEntry) error {
	for _, entry := range entries {
		if err := repo.AppendEntry(ctx, entry); err != nil {
			return err
		}
	}
	return nil
}

func updateBillingAccount(ctx context.Context, repo repository.BillingRepository, account *model.BillingWalletAccount) error {
	if err := account.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrBillingLedgerInvalid, err)
	}
	expected := account.Version
	if expected == math.MaxInt64 {
		return repository.ErrBillingVersionConflict
	}
	account.Version++
	return repo.UpdateAccount(ctx, account, expected)
}

func (s *BillingWalletService) withTx(ctx context.Context, fn func(repository.Repository) error) error {
	var err error
	for attempt := 0; attempt < 20; attempt++ {
		err = s.repo.WithTx(ctx, fn)
		if err == nil || !isRetryableBillingDBError(err) {
			return err
		}
		timer := time.NewTimer(time.Duration(attempt+1) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return err
}

func isRetryableBillingDBError(err error) bool {
	if errors.Is(err, driver.ErrBadConn) {
		return true
	}
	if isRetryableSQLiteError(err) {
		return true
	}
	var mysqlErr *mysqlDriver.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1205 || mysqlErr.Number == 1213
	}
	return false
}

func isRetryableSQLiteError(err error) bool {
	var sqliteErr sqlite3.Error
	return errors.As(err, &sqliteErr) && (sqliteErr.Code == sqlite3.ErrBusy || sqliteErr.Code == sqlite3.ErrLocked)
}

func sortChargeAllocationsForLocking(allocations []model.BillingChargeAllocation) {
	sort.Slice(allocations, func(i, j int) bool {
		if allocations[i].LotID == allocations[j].LotID {
			return allocations[i].ID < allocations[j].ID
		}
		return allocations[i].LotID < allocations[j].LotID
	})
}

func checkedBillingAdd(left, right int64) (int64, bool) {
	if (right > 0 && left > math.MaxInt64-right) || (right < 0 && left < math.MinInt64-right) {
		return 0, false
	}
	return left + right, true
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}
