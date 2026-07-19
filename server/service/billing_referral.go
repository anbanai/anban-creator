package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/billing"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	BillingReferralStatusIssued = model.BillingReferralStatusIssued
	BillingReferralStatusCapped = model.BillingReferralStatusCapped
)

type BillingReferralOptions struct {
	Now func() time.Time
}

type BillingReferralService struct {
	repo   repository.Repository
	wallet *BillingWalletService
	bundle billing.Bundle
	now    func() time.Time
}

type BillingReferralTopUpResult struct {
	TopUp    *TopUpResult
	Referral *model.BillingReferralIssue
}

func NewBillingReferralService(repo repository.Repository, wallet *BillingWalletService, bundle *billing.Bundle, opts BillingReferralOptions) *BillingReferralService {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	var snapshot billing.Bundle
	if bundle != nil {
		snapshot = *bundle
		snapshot.Promotions.Programs = append([]billing.ReferralProgram(nil), bundle.Promotions.Programs...)
	}
	return &BillingReferralService{repo: repo, wallet: wallet, bundle: snapshot, now: now}
}

// TopUp atomically applies the paid top-up and, when eligible, the fixed
// first-top-up referral promotion. Invitation identity is resolved before the
// transaction so both account rows can always be locked in stable order.
func (s *BillingReferralService) TopUp(ctx context.Context, req TopUpRequest) (*BillingReferralTopUpResult, error) {
	if s == nil || s.repo == nil || s.wallet == nil {
		return nil, fmt.Errorf("%w: referral service is not configured", ErrBillingInvalid)
	}
	var err error
	req, err = s.wallet.normalizeTopUpRequest(req)
	if err != nil {
		return nil, err
	}

	inviterID, err := s.resolveInviter(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	program, hasProgram := s.activeProgram()
	qualifies := hasProgram && qualifiesReferralTopUp(req.Credits, program.MinimumTopUpCNY, s.bundle.Policy.CreditsPerCNY)
	var result *BillingReferralTopUpResult
	err = s.wallet.withTx(ctx, func(tx repository.Repository) error {
		if inviterID != "" && hasProgram {
			if err := lockReferralAccounts(ctx, tx.Billing(), req.UserID, inviterID); err != nil {
				return err
			}
		}
		topUp, err := s.wallet.topUpInTx(ctx, tx, req)
		if err != nil {
			return err
		}
		result = &BillingReferralTopUpResult{TopUp: topUp}
		if !qualifies {
			return nil
		}
		if inviterID == "" {
			if _, err := tx.Billing().FindReferralIssue(ctx, req.UserID, program.ID); err == nil {
				return ErrBillingConflict
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			return nil
		}
		issue, err := s.issueOrReplay(ctx, tx.Billing(), req, topUp, inviterID, program)
		if err != nil {
			return err
		}
		result.Referral = issue
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *BillingReferralService) resolveInviter(ctx context.Context, inviteeID string) (string, error) {
	invitee, err := s.repo.Users().FindByID(ctx, inviteeID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	inviterID := strings.TrimSpace(invitee.InvitedBy)
	if inviterID == "" || inviterID == inviteeID {
		return "", nil
	}
	if _, err := s.repo.Users().FindByID(ctx, inviterID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return inviterID, nil
}

func (s *BillingReferralService) activeProgram() (billing.ReferralProgram, bool) {
	for _, program := range s.bundle.Promotions.Programs {
		if program.Trigger == "invitee_first_paid_topup" {
			return program, true
		}
	}
	return billing.ReferralProgram{}, false
}

func (s *BillingReferralService) issueOrReplay(ctx context.Context, repo repository.BillingRepository, req TopUpRequest, topUp *TopUpResult, inviterID string, program billing.ReferralProgram) (*model.BillingReferralIssue, error) {
	existing, err := repo.FindReferralIssue(ctx, req.UserID, program.ID)
	if err == nil {
		if existing.InviteeUserID != req.UserID || existing.InviterUserID != inviterID ||
			existing.ProgramID != program.ID || existing.CatalogID != s.bundle.Promotions.CatalogID {
			return nil, ErrBillingConflict
		}
		if existing.QualifyingTopUpEntryID == topUp.EntryID {
			wantFingerprint := referralFingerprint(req, topUp.EntryID, inviterID, program.ID, s.bundle.Promotions.CatalogID)
			if existing.RequestFingerprint != wantFingerprint {
				return nil, ErrBillingConflict
			}
		}
		return existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	now := s.now().UTC()
	issue := &model.BillingReferralIssue{
		ID:                     uuid.NewString(),
		ProgramID:              program.ID,
		CatalogID:              s.bundle.Promotions.CatalogID,
		InviteeUserID:          req.UserID,
		InviterUserID:          inviterID,
		QualifyingTopUpEntryID: topUp.EntryID,
		RequestFingerprint:     referralFingerprint(req, topUp.EntryID, inviterID, program.ID, s.bundle.Promotions.CatalogID),
		CreatedAt:              now,
		UpdatedAt:              now,
	}
	issued, err := repo.CountIssuedReferrals(ctx, inviterID, program.ID)
	if err != nil {
		return nil, err
	}
	if issued >= program.MaxInviterRewards {
		issue.Status = model.BillingReferralStatusCapped
		if err := repo.CreateReferralIssue(ctx, issue); err != nil {
			return nil, err
		}
		return issue, nil
	}

	inviteeLotID, inviterLotID := uuid.NewString(), uuid.NewString()
	issue.InviteeLotID = &inviteeLotID
	issue.InviterLotID = &inviterLotID
	issue.Status = model.BillingReferralStatusIssued
	issue.IssuedAt = &now
	if err := repo.CreateReferralIssue(ctx, issue); err != nil {
		return nil, err
	}

	accounts := make(map[string]*model.BillingWalletAccount, 2)
	for _, userID := range orderedDistinctUserIDs(req.UserID, inviterID) {
		account, err := repo.LockAccount(ctx, userID)
		if err != nil {
			return nil, err
		}
		accounts[userID] = account
	}
	expiresAt := now.Add(program.ExpiresAfter)
	rewards := []struct {
		role    string
		userID  string
		lotID   string
		credits int64
	}{
		{role: "invitee", userID: req.UserID, lotID: inviteeLotID, credits: program.InviteeCredits},
		{role: "inviter", userID: inviterID, lotID: inviterLotID, credits: program.InviterCredits},
	}
	for _, reward := range rewards {
		if err := s.appendReward(ctx, repo, issue, req, reward.role, reward.userID, reward.lotID, reward.credits, expiresAt, now); err != nil {
			return nil, err
		}
		account := accounts[reward.userID]
		if reward.credits <= 0 || account.PromotionalCredits > math.MaxInt64-reward.credits {
			return nil, ErrBillingLedgerInvalid
		}
		account.PromotionalCredits += reward.credits
	}
	for _, userID := range orderedDistinctUserIDs(req.UserID, inviterID) {
		if err := updateBillingAccount(ctx, repo, accounts[userID]); err != nil {
			return nil, err
		}
	}
	return issue, nil
}

func (s *BillingReferralService) appendReward(ctx context.Context, repo repository.BillingRepository, issue *model.BillingReferralIssue, req TopUpRequest, role, userID, lotID string, credits int64, expiresAt, now time.Time) error {
	sourceID := issue.ID + ":" + role
	lot := &model.BillingCreditLot{
		ID: lotID, UserID: userID, Kind: model.BillingCreditLotKindPromotional,
		SourceType: "referral", SourceID: sourceID, ProgramID: issue.ProgramID, CatalogID: issue.CatalogID,
		OriginalCredits: credits, AvailableCredits: credits, ExpiresAt: &expiresAt, CreatedAt: now,
	}
	if err := lot.Validate(); err != nil {
		return err
	}
	if err := repo.CreateLot(ctx, lot); err != nil {
		return err
	}
	return repo.AppendEntry(ctx, &model.BillingWalletEntry{
		ID: uuid.NewString(), UserID: userID, EventKind: model.BillingWalletEventKindPromotion,
		PromotionalDelta: credits, CatalogID: issue.CatalogID,
		RequestFingerprint: billingFingerprint("referral-reward", issue.ID, role, userID, fmt.Sprint(credits), expiresAt.Format(time.RFC3339Nano)),
		LotID:              &lotID, ResourceType: "referral", ResourceID: issue.ID,
		SourceType: stringPtr("referral"), SourceID: stringPtr(sourceID),
		IdempotencyScope: "referral-reward:" + issue.ID, IdempotencyKey: role,
		ActorType: "system", ActorID: issue.ProgramID, SourceService: "billing-referral",
		RequestID: req.RequestID, CorrelationID: req.CorrelationID, CreatedAt: now,
	})
}

func lockReferralAccounts(ctx context.Context, repo repository.BillingRepository, userIDs ...string) error {
	ordered := orderedDistinctUserIDs(userIDs...)
	for _, userID := range ordered {
		if err := repo.EnsureAccount(ctx, userID); err != nil {
			return err
		}
	}
	for _, userID := range ordered {
		if _, err := repo.LockAccount(ctx, userID); err != nil {
			return err
		}
	}
	return nil
}

func orderedDistinctUserIDs(userIDs ...string) []string {
	seen := make(map[string]struct{}, len(userIDs))
	ordered := make([]string, 0, len(userIDs))
	for _, userID := range userIDs {
		userID = strings.TrimSpace(userID)
		if userID == "" {
			continue
		}
		if _, exists := seen[userID]; exists {
			continue
		}
		seen[userID] = struct{}{}
		ordered = append(ordered, userID)
	}
	sort.Strings(ordered)
	return ordered
}

func qualifiesReferralTopUp(credits int64, minimum billing.MicroCNY, creditsPerCNY int64) bool {
	if credits <= 0 || minimum <= 0 || creditsPerCNY <= 0 {
		return false
	}
	const microCNYPerCNY = int64(1_000_000)
	whole, remainder := int64(minimum)/microCNYPerCNY, int64(minimum)%microCNYPerCNY
	if whole > math.MaxInt64/creditsPerCNY {
		return false
	}
	minimumCredits := whole * creditsPerCNY
	if remainder > 0 {
		if remainder > math.MaxInt64/creditsPerCNY {
			return false
		}
		product := remainder * creditsPerCNY
		fractionCredits := product / microCNYPerCNY
		if product%microCNYPerCNY != 0 {
			fractionCredits++
		}
		if minimumCredits > math.MaxInt64-fractionCredits {
			return false
		}
		minimumCredits += fractionCredits
	}
	return credits >= minimumCredits
}

func referralFingerprint(req TopUpRequest, topUpEntryID, inviterID, programID, catalogID string) string {
	return billingFingerprint("referral", req.UserID, inviterID, programID, catalogID, topUpEntryID, req.RequestFingerprint)
}
