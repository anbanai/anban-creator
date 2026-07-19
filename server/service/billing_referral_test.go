package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/billing"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"gorm.io/gorm"
)

func TestBillingReferral(t *testing.T) {
	t.Run("first qualifying paid topup issues both rewards exactly once", func(t *testing.T) {
		f := newBillingReferralFixture(t, 10)
		f.createUser(t, "inviter", "")
		f.createUser(t, "invitee", "inviter")

		below := f.topUpRequest("invitee", 9_999, "below")
		if got, err := f.referrals.TopUp(context.Background(), below); err != nil || got.Referral != nil {
			t.Fatalf("below-threshold TopUp = %+v, %v; want no referral", got, err)
		}
		f.assertAccount(t, "inviter", 0, 0, 0)
		f.assertAccount(t, "invitee", 9_999, 0, 0)

		qualifying := f.topUpRequest("invitee", 10_000, "qualifying")
		first, err := f.referrals.TopUp(context.Background(), qualifying)
		if err != nil || first.Referral == nil || first.Referral.Status != BillingReferralStatusIssued {
			t.Fatalf("qualifying TopUp = %+v, %v; want issued referral", first, err)
		}
		if first.Referral.ProgramID != "referral-test-v1" || first.Referral.CatalogID != "promotion-test-v1" ||
			first.Referral.InviterUserID != "inviter" || first.Referral.InviteeUserID != "invitee" {
			t.Fatalf("referral identity = %+v", first.Referral)
		}
		f.assertAccount(t, "inviter", 0, 1_000, 0)
		f.assertAccount(t, "invitee", 19_999, 1_000, 0)
		f.assertReferralLot(t, first.Referral.InviterLotID, "inviter", first.Referral.ID+":inviter")
		f.assertReferralLot(t, first.Referral.InviteeLotID, "invitee", first.Referral.ID+":invitee")

		replay, err := f.referrals.TopUp(context.Background(), qualifying)
		if err != nil || replay.Referral == nil || replay.Referral.ID != first.Referral.ID || replay.TopUp.EntryID != first.TopUp.EntryID {
			t.Fatalf("exact replay = %+v, %v; want topup %s referral %s", replay, err, first.TopUp.EntryID, first.Referral.ID)
		}
		secondTopUp, err := f.referrals.TopUp(context.Background(), f.topUpRequest("invitee", 20_000, "later"))
		if err != nil || secondTopUp.Referral == nil || secondTopUp.Referral.ID != first.Referral.ID {
			t.Fatalf("later topup = %+v, %v; want existing referral %s", secondTopUp, err, first.Referral.ID)
		}
		f.assertAccount(t, "inviter", 0, 1_000, 0)
		f.assertAccount(t, "invitee", 39_999, 1_000, 0)
		f.assertRewardEntryCount(t, "inviter", 1)
		f.assertRewardEntryCount(t, "invitee", 1)
	})

	t.Run("no invitation and self invitation never issue rewards", func(t *testing.T) {
		for _, tc := range []struct {
			name      string
			invitedBy string
		}{
			{name: "no invitation"},
			{name: "self invitation", invitedBy: "invitee"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				f := newBillingReferralFixture(t, 10)
				f.createUser(t, "invitee", tc.invitedBy)
				got, err := f.referrals.TopUp(context.Background(), f.topUpRequest("invitee", 10_000, tc.name))
				if err != nil || got.Referral != nil {
					t.Fatalf("TopUp = %+v, %v; want no referral", got, err)
				}
				f.assertAccount(t, "invitee", 10_000, 0, 0)
				f.assertRewardEntryCount(t, "invitee", 0)
			})
		}
	})

	t.Run("inviter cap is serialized and does not block paid topup", func(t *testing.T) {
		f := newBillingReferralFixture(t, 1)
		f.createUser(t, "inviter", "")
		f.createUser(t, "invitee-1", "inviter")
		f.createUser(t, "invitee-2", "inviter")

		first, err := f.referrals.TopUp(context.Background(), f.topUpRequest("invitee-1", 10_000, "cap-1"))
		if err != nil || first.Referral == nil || first.Referral.Status != BillingReferralStatusIssued {
			t.Fatalf("first referral = %+v, %v", first, err)
		}
		second, err := f.referrals.TopUp(context.Background(), f.topUpRequest("invitee-2", 10_000, "cap-2"))
		if err != nil || second.Referral == nil || second.Referral.Status != BillingReferralStatusCapped {
			t.Fatalf("capped referral = %+v, %v", second, err)
		}
		if second.Referral.InviterLotID != nil || second.Referral.InviteeLotID != nil {
			t.Fatalf("capped referral has reward lots: %+v", second.Referral)
		}
		f.assertAccount(t, "inviter", 0, 1_000, 0)
		f.assertAccount(t, "invitee-1", 10_000, 1_000, 0)
		f.assertAccount(t, "invitee-2", 10_000, 0, 0)
	})

	t.Run("promotion never repays remaining debt", func(t *testing.T) {
		f := newBillingReferralFixtureWithDebt(t, 15_000)
		f.createUser(t, "inviter", "")
		f.createUser(t, "u1", "inviter")

		got, err := f.referrals.TopUp(context.Background(), f.topUpRequest("u1", 10_000, "debt"))
		if err != nil || got.Referral == nil || got.TopUp.DebtRepaid != 10_000 || got.TopUp.PaidAdded != 0 {
			t.Fatalf("debt TopUp = %+v, %v", got, err)
		}
		f.assertAccount(t, "u1", 0, 1_000, 5_000)
		if got.Referral.InviteeLotID == nil {
			t.Fatal("debt invitee reward lot missing")
		}
		lot, err := f.repo.Billing().LockLotByID(context.Background(), *got.Referral.InviteeLotID)
		if !errors.Is(err, repository.ErrBillingRequiresTransaction) {
			t.Fatalf("root LockLotByID error = %v, want transaction requirement", err)
		}
		lot, err = f.repo.Billing().FindLotBySource(context.Background(), "referral", got.Referral.ID+":invitee")
		if err != nil || lot.AvailableCredits != 1_000 || lot.ConsumedCredits != 0 {
			t.Fatalf("debt invitee reward lot = %+v, %v", lot, err)
		}
	})

	t.Run("exact concurrent replay has one topup and one reward pair", func(t *testing.T) {
		f := newBillingReferralFixture(t, 10)
		f.createUser(t, "inviter", "")
		f.createUser(t, "invitee", "inviter")
		req := f.topUpRequest("invitee", 10_000, "concurrent")

		const workers = 8
		results := make(chan *BillingReferralTopUpResult, workers)
		errs := make(chan error, workers)
		var wg sync.WaitGroup
		for range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				result, err := f.referrals.TopUp(context.Background(), req)
				results <- result
				errs <- err
			}()
		}
		wg.Wait()
		close(results)
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("concurrent TopUp: %v", err)
			}
		}
		var topUpID, referralID string
		for result := range results {
			if result == nil || result.Referral == nil {
				t.Fatalf("concurrent result = %+v", result)
			}
			if topUpID == "" {
				topUpID, referralID = result.TopUp.EntryID, result.Referral.ID
			}
			if result.TopUp.EntryID != topUpID || result.Referral.ID != referralID {
				t.Fatalf("concurrent identities = topup %s referral %s, want %s %s", result.TopUp.EntryID, result.Referral.ID, topUpID, referralID)
			}
		}
		f.assertAccount(t, "inviter", 0, 1_000, 0)
		f.assertAccount(t, "invitee", 10_000, 1_000, 0)
		f.assertRewardEntryCount(t, "inviter", 1)
		f.assertRewardEntryCount(t, "invitee", 1)
	})

	t.Run("topup and rewards roll back together", func(t *testing.T) {
		f := newBillingReferralFixture(t, 10)
		f.createUser(t, "inviter", "")
		f.createUser(t, "invitee", "inviter")
		if err := f.db.Exec(`CREATE TRIGGER fail_referral_lot BEFORE INSERT ON billing_credit_lots WHEN NEW.kind = 'promotional' BEGIN SELECT RAISE(ABORT, 'forced referral lot failure'); END`).Error; err != nil {
			t.Fatal(err)
		}

		if _, err := f.referrals.TopUp(context.Background(), f.topUpRequest("invitee", 10_000, "rollback")); err == nil {
			t.Fatal("TopUp error = nil, want forced referral failure")
		}
		if _, err := f.repo.Billing().FindEntryBySource(context.Background(), "api", "rollback"); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("rolled-back topup lookup = %v", err)
		}
		f.assertAccountAbsent(t, "inviter")
		f.assertAccountAbsent(t, "invitee")
	})

	t.Run("identity drift is a typed conflict", func(t *testing.T) {
		f := newBillingReferralFixture(t, 10)
		f.createUser(t, "inviter-1", "")
		f.createUser(t, "inviter-2", "")
		f.createUser(t, "invitee", "inviter-1")
		req := f.topUpRequest("invitee", 10_000, "drift")
		if _, err := f.referrals.TopUp(context.Background(), req); err != nil {
			t.Fatal(err)
		}
		user, err := f.repo.Users().FindByID(context.Background(), "invitee")
		if err != nil {
			t.Fatal(err)
		}
		user.InvitedBy = "inviter-2"
		if err := f.repo.Users().Update(context.Background(), user); err != nil {
			t.Fatal(err)
		}
		if _, err := f.referrals.TopUp(context.Background(), req); !errors.Is(err, ErrBillingConflict) {
			t.Fatalf("identity drift error = %v, want ErrBillingConflict", err)
		}
		user.InvitedBy = ""
		if err := f.repo.Users().Update(context.Background(), user); err != nil {
			t.Fatal(err)
		}
		if _, err := f.referrals.TopUp(context.Background(), req); !errors.Is(err, ErrBillingConflict) {
			t.Fatalf("cleared invitation error = %v, want ErrBillingConflict", err)
		}
	})

	t.Run("caller transaction rollback includes topup", func(t *testing.T) {
		f := newBillingReferralFixture(t, 10)
		req := f.topUpRequest("standalone", 10_000, "caller-tx")
		errRollback := errors.New("rollback caller transaction")
		err := f.repo.WithTx(context.Background(), func(tx repository.Repository) error {
			if _, err := f.wallet.TopUpInTx(context.Background(), tx, req); err != nil {
				return err
			}
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("caller transaction error = %v", err)
		}
		if _, err := f.repo.Billing().FindEntryBySource(context.Background(), "api", "caller-tx"); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("rolled-back caller topup lookup = %v", err)
		}
	})
}

type billingReferralFixture struct {
	repo      repository.Repository
	db        *gorm.DB
	wallet    *BillingWalletService
	referrals *BillingReferralService
	bundle    billing.Bundle
	now       time.Time
}

func newBillingReferralFixture(t *testing.T, maxInviterRewards int64) *billingReferralFixture {
	t.Helper()
	return newBillingReferralFixtureWithDebtAndCap(t, 0, maxInviterRewards)
}

func newBillingReferralFixtureWithDebt(t *testing.T, debt int64) *billingReferralFixture {
	t.Helper()
	return newBillingReferralFixtureWithDebtAndCap(t, debt, 10)
}

func newBillingReferralFixtureWithDebtAndCap(t *testing.T, debt, maxInviterRewards int64) *billingReferralFixture {
	t.Helper()
	repo, db := newBillingServiceRepositoryWithDB(t)
	bundle := testBillingBundle()
	bundle.Policy.CreditsPerCNY = 1_000
	bundle.Promotions = billing.PromotionCatalog{
		CatalogID: "promotion-test-v1",
		Programs: []billing.ReferralProgram{{
			ID: "referral-test-v1", Trigger: "invitee_first_paid_topup", MinimumTopUpCNY: billing.MicroCNY(10_000_000),
			InviterCredits: 1_000, InviteeCredits: 1_000, ExpiresAfter: 30 * 24 * time.Hour,
			MaxInviterRewards: maxInviterRewards,
		}},
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	wallet := NewBillingWalletService(repo, &bundle, BillingWalletOptions{Now: func() time.Time { return now }})
	fixture := &billingReferralFixture{
		repo: repo, db: db, wallet: wallet, bundle: bundle, now: now,
		referrals: NewBillingReferralService(repo, wallet, &bundle, BillingReferralOptions{Now: func() time.Time { return now }}),
	}
	if debt > 0 {
		base := newBillingWalletFixtureWithRepository(t, repo, 0, 0, debt)
		fixture.wallet = NewBillingWalletService(repo, &bundle, BillingWalletOptions{Now: func() time.Time { return now }})
		fixture.referrals = NewBillingReferralService(repo, fixture.wallet, &bundle, BillingReferralOptions{Now: func() time.Time { return now }})
		_ = base
	}
	return fixture
}

func (f *billingReferralFixture) createUser(t *testing.T, userID, invitedBy string) {
	t.Helper()
	if err := f.repo.Users().Create(context.Background(), &model.User{
		ID: userID, Email: userID + "@example.test", Password: "fixture", InviteCode: "code-" + userID, InvitedBy: invitedBy,
	}); err != nil {
		t.Fatal(err)
	}
}

func (f *billingReferralFixture) topUpRequest(userID string, credits int64, identity string) TopUpRequest {
	return TopUpRequest{
		UserID: userID, Credits: credits, ExternalSourceType: "api", ExternalSourceID: identity,
		CatalogID: f.bundle.Products.CatalogID, RequestFingerprint: billingFingerprint("topup", userID, fmt.Sprint(credits), identity),
		IdempotencyScope: "admin-topup", IdempotencyKey: identity, ActorType: "admin", ActorID: "admin-1", SourceService: "billing-api",
	}
}

func (f *billingReferralFixture) assertAccount(t *testing.T, userID string, paid, promotional, debt int64) {
	t.Helper()
	account, err := f.repo.Billing().FindAccount(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	if account.PaidCredits != paid || account.PromotionalCredits != promotional || account.DebtCredits != debt {
		t.Fatalf("account %s = %+v, want paid=%d promotional=%d debt=%d", userID, account, paid, promotional, debt)
	}
}

func (f *billingReferralFixture) assertAccountAbsent(t *testing.T, userID string) {
	t.Helper()
	if _, err := f.repo.Billing().FindAccount(context.Background(), userID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("account %s lookup = %v, want not found", userID, err)
	}
}

func (f *billingReferralFixture) assertReferralLot(t *testing.T, lotID *string, userID, sourceID string) {
	t.Helper()
	if lotID == nil {
		t.Fatalf("reward lot for %s is nil", userID)
	}
	lot, err := f.repo.Billing().FindLotBySource(context.Background(), "referral", sourceID)
	if err != nil {
		t.Fatal(err)
	}
	wantExpiry := f.now.Add(30 * 24 * time.Hour)
	if lot.ID != *lotID || lot.UserID != userID || lot.ProgramID != "referral-test-v1" || lot.CatalogID != "promotion-test-v1" ||
		lot.OriginalCredits != 1_000 || lot.AvailableCredits != 1_000 || lot.ExpiresAt == nil || !lot.ExpiresAt.Equal(wantExpiry) {
		t.Fatalf("reward lot = %+v, want user=%s source=%s expiry=%s", lot, userID, sourceID, wantExpiry)
	}
}

func (f *billingReferralFixture) assertRewardEntryCount(t *testing.T, userID string, want int) {
	t.Helper()
	entries, err := f.repo.Billing().ListEntriesByUser(context.Background(), userID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	got := 0
	for _, entry := range entries {
		if entry.EventKind == model.BillingWalletEventKindPromotion {
			got++
		}
	}
	if got != want {
		t.Fatalf("promotion entry count for %s = %d, want %d; entries=%+v", userID, got, want, entries)
	}
}
