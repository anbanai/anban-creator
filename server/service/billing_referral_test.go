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

const (
	billingReferralInviterID     = "20000000-0000-4000-8000-000000000001"
	billingReferralInviteeID     = "20000000-0000-4000-8000-000000000002"
	billingReferralInviteeOneID  = "20000000-0000-4000-8000-000000000003"
	billingReferralInviteeTwoID  = "20000000-0000-4000-8000-000000000004"
	billingReferralInviterOneID  = "20000000-0000-4000-8000-000000000005"
	billingReferralInviterTwoID  = "20000000-0000-4000-8000-000000000006"
	billingReferralDebtInviteeID = billingWalletUserID
	billingReferralStandaloneID  = "20000000-0000-4000-8000-000000000007"
)

func TestBillingReferral(t *testing.T) {
	t.Run("first qualifying paid topup issues both rewards exactly once", func(t *testing.T) {
		f := newBillingReferralFixture(t, 10)
		f.createUser(t, billingReferralInviterID, "")
		f.createUser(t, billingReferralInviteeID, billingReferralInviterID)

		below := f.topUpRequest(billingReferralInviteeID, 9_999, "below")
		if got, err := f.referrals.TopUp(context.Background(), below); err != nil || got.Referral != nil {
			t.Fatalf("below-threshold TopUp = %+v, %v; want no referral", got, err)
		}
		f.assertAccount(t, billingReferralInviterID, 0, 0, 0)
		f.assertAccount(t, billingReferralInviteeID, 9_999, 0, 0)

		qualifying := f.topUpRequest(billingReferralInviteeID, 10_000, "qualifying")
		first, err := f.referrals.TopUp(context.Background(), qualifying)
		if err != nil || first.Referral == nil || first.Referral.Status != BillingReferralStatusIssued {
			t.Fatalf("qualifying TopUp = %+v, %v; want issued referral", first, err)
		}
		if first.Referral.ProgramID != billing.ReferralFirstTopUpProgramID || first.Referral.CatalogID != "promotion-test-v1" ||
			first.Referral.InviterUserID != billingReferralInviterID || first.Referral.InviteeUserID != billingReferralInviteeID {
			t.Fatalf("referral identity = %+v", first.Referral)
		}
		f.assertAccount(t, billingReferralInviterID, 0, 1_000, 0)
		f.assertAccount(t, billingReferralInviteeID, 19_999, 1_000, 0)
		f.assertReferralLot(t, first.Referral.InviterLotID, billingReferralInviterID, first.Referral.ID+":inviter")
		f.assertReferralLot(t, first.Referral.InviteeLotID, billingReferralInviteeID, first.Referral.ID+":invitee")

		replay, err := f.referrals.TopUp(context.Background(), qualifying)
		if err != nil || replay.Referral == nil || replay.Referral.ID != first.Referral.ID || replay.TopUp.EntryID != first.TopUp.EntryID {
			t.Fatalf("exact replay = %+v, %v; want topup %s referral %s", replay, err, first.TopUp.EntryID, first.Referral.ID)
		}
		secondTopUp, err := f.referrals.TopUp(context.Background(), f.topUpRequest(billingReferralInviteeID, 20_000, "later"))
		if err != nil || secondTopUp.Referral == nil || secondTopUp.Referral.ID != first.Referral.ID {
			t.Fatalf("later topup = %+v, %v; want existing referral %s", secondTopUp, err, first.Referral.ID)
		}
		f.assertAccount(t, billingReferralInviterID, 0, 1_000, 0)
		f.assertAccount(t, billingReferralInviteeID, 39_999, 1_000, 0)
		f.assertRewardEntryCount(t, billingReferralInviterID, 1)
		f.assertRewardEntryCount(t, billingReferralInviteeID, 1)
	})

	t.Run("no invitation and self invitation never issue rewards", func(t *testing.T) {
		for _, tc := range []struct {
			name      string
			invitedBy string
		}{
			{name: "no invitation"},
			{name: "self invitation", invitedBy: billingReferralInviteeID},
		} {
			t.Run(tc.name, func(t *testing.T) {
				f := newBillingReferralFixture(t, 10)
				f.createUser(t, billingReferralInviteeID, tc.invitedBy)
				got, err := f.referrals.TopUp(context.Background(), f.topUpRequest(billingReferralInviteeID, 10_000, tc.name))
				if err != nil || got.Referral != nil {
					t.Fatalf("TopUp = %+v, %v; want no referral", got, err)
				}
				f.assertAccount(t, billingReferralInviteeID, 10_000, 0, 0)
				f.assertRewardEntryCount(t, billingReferralInviteeID, 0)
			})
		}
	})

	t.Run("inviter cap is serialized and does not block paid topup", func(t *testing.T) {
		f := newBillingReferralFixture(t, 1)
		f.createUser(t, billingReferralInviterID, "")
		f.createUser(t, billingReferralInviteeOneID, billingReferralInviterID)
		f.createUser(t, billingReferralInviteeTwoID, billingReferralInviterID)

		first, err := f.referrals.TopUp(context.Background(), f.topUpRequest(billingReferralInviteeOneID, 10_000, "cap-1"))
		if err != nil || first.Referral == nil || first.Referral.Status != BillingReferralStatusIssued {
			t.Fatalf("first referral = %+v, %v", first, err)
		}
		second, err := f.referrals.TopUp(context.Background(), f.topUpRequest(billingReferralInviteeTwoID, 10_000, "cap-2"))
		if err != nil || second.Referral == nil || second.Referral.Status != BillingReferralStatusCapped {
			t.Fatalf("capped referral = %+v, %v", second, err)
		}
		if second.Referral.InviterLotID != nil || second.Referral.InviteeLotID != nil {
			t.Fatalf("capped referral has reward lots: %+v", second.Referral)
		}
		f.assertAccount(t, billingReferralInviterID, 0, 1_000, 0)
		f.assertAccount(t, billingReferralInviteeOneID, 10_000, 1_000, 0)
		f.assertAccount(t, billingReferralInviteeTwoID, 10_000, 0, 0)
	})

	t.Run("promotion never repays remaining debt", func(t *testing.T) {
		f := newBillingReferralFixtureWithDebt(t, 15_000)
		f.createUser(t, billingReferralInviterID, "")
		f.createUser(t, billingReferralDebtInviteeID, billingReferralInviterID)

		got, err := f.referrals.TopUp(context.Background(), f.topUpRequest(billingReferralDebtInviteeID, 10_000, "debt"))
		if err != nil || got.Referral == nil || got.TopUp.DebtRepaid != 10_000 || got.TopUp.PaidAdded != 0 {
			t.Fatalf("debt TopUp = %+v, %v", got, err)
		}
		f.assertAccount(t, billingReferralDebtInviteeID, 0, 1_000, 5_000)
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
		f.createUser(t, billingReferralInviterID, "")
		f.createUser(t, billingReferralInviteeID, billingReferralInviterID)
		req := f.topUpRequest(billingReferralInviteeID, 10_000, "concurrent")

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
		f.assertAccount(t, billingReferralInviterID, 0, 1_000, 0)
		f.assertAccount(t, billingReferralInviteeID, 10_000, 1_000, 0)
		f.assertRewardEntryCount(t, billingReferralInviterID, 1)
		f.assertRewardEntryCount(t, billingReferralInviteeID, 1)
	})

	t.Run("historical issue survives catalog and threshold changes", func(t *testing.T) {
		f := newBillingReferralFixture(t, 10)
		f.createUser(t, billingReferralInviterID, "")
		f.createUser(t, billingReferralInviteeID, billingReferralInviterID)
		exact := f.topUpRequest(billingReferralInviteeID, 10_000, "historical-exact")
		first, err := f.referrals.TopUp(context.Background(), exact)
		if err != nil || first.Referral == nil {
			t.Fatalf("initial referral = %+v, %v", first, err)
		}

		f.bundle.Promotions.CatalogID = "promotion-sha256-current"
		f.bundle.Promotions.Programs[0].MinimumTopUpCNY = billing.MicroCNY(20_000_000)
		f.wallet = NewBillingWalletService(f.repo, &f.bundle, BillingWalletOptions{Now: func() time.Time { return f.now }})
		f.referrals = NewBillingReferralService(f.repo, f.wallet, &f.bundle, BillingReferralOptions{Now: func() time.Time { return f.now }})

		replay, err := f.referrals.TopUp(context.Background(), exact)
		if err != nil || replay.Referral == nil || replay.Referral.ID != first.Referral.ID || replay.Referral.CatalogID != first.Referral.CatalogID {
			t.Fatalf("historical exact replay = %+v, %v; want issue %s catalog %s", replay, err, first.Referral.ID, first.Referral.CatalogID)
		}
		nonqualifying, err := f.referrals.TopUp(context.Background(), f.topUpRequest(billingReferralInviteeID, 15_000, "historical-below-current"))
		if err != nil || nonqualifying.Referral != nil {
			t.Fatalf("nonqualifying later topup = %+v, %v; want ordinary topup", nonqualifying, err)
		}
		qualifying, err := f.referrals.TopUp(context.Background(), f.topUpRequest(billingReferralInviteeID, 20_000, "historical-current"))
		if err != nil || qualifying.Referral == nil || qualifying.Referral.ID != first.Referral.ID {
			t.Fatalf("qualifying later topup = %+v, %v; want historical issue %s", qualifying, err, first.Referral.ID)
		}
	})

	t.Run("disabled promotions still replay the exact historical issue", func(t *testing.T) {
		f := newBillingReferralFixture(t, 10)
		f.createUser(t, billingReferralInviterID, "")
		f.createUser(t, billingReferralInviteeID, billingReferralInviterID)
		exact := f.topUpRequest(billingReferralInviteeID, 10_000, "disabled-exact")
		first, err := f.referrals.TopUp(context.Background(), exact)
		if err != nil || first.Referral == nil {
			t.Fatalf("initial referral = %+v, %v", first, err)
		}
		f.bundle.Promotions = billing.PromotionCatalog{CatalogID: "promotion-sha256-disabled", Programs: []billing.ReferralProgram{}}
		f.referrals = NewBillingReferralService(f.repo, f.wallet, &f.bundle, BillingReferralOptions{Now: func() time.Time { return f.now }})
		replay, err := f.referrals.TopUp(context.Background(), exact)
		if err != nil || replay.Referral == nil || replay.Referral.ID != first.Referral.ID {
			t.Fatalf("disabled exact replay = %+v, %v; want historical issue %s", replay, err, first.Referral.ID)
		}
		later, err := f.referrals.TopUp(context.Background(), f.topUpRequest(billingReferralInviteeID, 20_000, "disabled-later"))
		if err != nil || later.Referral != nil {
			t.Fatalf("disabled later topup = %+v, %v; want ordinary topup", later, err)
		}
	})

	t.Run("cross catalog concurrent winner is accepted", func(t *testing.T) {
		f := newBillingReferralFixture(t, 10)
		f.createUser(t, billingReferralInviterID, "")
		f.createUser(t, billingReferralInviteeID, billingReferralInviterID)
		first, err := f.referrals.TopUp(context.Background(), f.topUpRequest(billingReferralInviteeID, 10_000, "winner"))
		if err != nil || first.Referral == nil {
			t.Fatalf("winner referral = %+v, %v", first, err)
		}
		f.bundle.Promotions.CatalogID = "promotion-sha256-loser"
		loser := NewBillingReferralService(f.repo, f.wallet, &f.bundle, BillingReferralOptions{Now: func() time.Time { return f.now }})
		err = f.repo.WithTx(context.Background(), func(tx repository.Repository) error {
			got, err := loser.issueOrReplay(context.Background(), tx.Billing(), f.topUpRequest(billingReferralInviteeID, 20_000, "loser"), &TopUpResult{EntryID: "other-topup"}, billingReferralInviterID, f.bundle.Promotions.Programs[0])
			if err != nil || got.ID != first.Referral.ID || got.CatalogID != first.Referral.CatalogID {
				t.Fatalf("concurrent winner = %+v, %v; want historical issue", got, err)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("inviter cap spans promotion catalogs", func(t *testing.T) {
		f := newBillingReferralFixture(t, 1)
		f.createUser(t, billingReferralInviterID, "")
		f.createUser(t, billingReferralInviteeOneID, billingReferralInviterID)
		f.createUser(t, billingReferralInviteeTwoID, billingReferralInviterID)
		first, err := f.referrals.TopUp(context.Background(), f.topUpRequest(billingReferralInviteeOneID, 10_000, "catalog-cap-one"))
		if err != nil || first.Referral == nil || first.Referral.Status != BillingReferralStatusIssued {
			t.Fatalf("first referral = %+v, %v", first, err)
		}
		f.bundle.Promotions.CatalogID = "promotion-sha256-next"
		f.wallet = NewBillingWalletService(f.repo, &f.bundle, BillingWalletOptions{Now: func() time.Time { return f.now }})
		f.referrals = NewBillingReferralService(f.repo, f.wallet, &f.bundle, BillingReferralOptions{Now: func() time.Time { return f.now }})
		second, err := f.referrals.TopUp(context.Background(), f.topUpRequest(billingReferralInviteeTwoID, 10_000, "catalog-cap-two"))
		if err != nil || second.Referral == nil || second.Referral.Status != BillingReferralStatusCapped {
			t.Fatalf("cross-catalog capped referral = %+v, %v", second, err)
		}
	})

	t.Run("topup and rewards roll back together", func(t *testing.T) {
		f := newBillingReferralFixture(t, 10)
		f.createUser(t, billingReferralInviterID, "")
		f.createUser(t, billingReferralInviteeID, billingReferralInviterID)
		if err := f.db.Exec(`CREATE TRIGGER fail_referral_lot BEFORE INSERT ON billing_credit_lots WHEN NEW.kind = 'promotional' BEGIN SELECT RAISE(ABORT, 'forced referral lot failure'); END`).Error; err != nil {
			t.Fatal(err)
		}

		if _, err := f.referrals.TopUp(context.Background(), f.topUpRequest(billingReferralInviteeID, 10_000, "rollback")); err == nil {
			t.Fatal("TopUp error = nil, want forced referral failure")
		}
		if _, err := f.repo.Billing().FindEntryBySource(context.Background(), "api", "rollback"); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("rolled-back topup lookup = %v", err)
		}
		f.assertAccount(t, billingReferralInviterID, 0, 0, 0)
		f.assertAccount(t, billingReferralInviteeID, 0, 0, 0)
	})

	t.Run("identity drift is a typed conflict", func(t *testing.T) {
		f := newBillingReferralFixture(t, 10)
		f.createUser(t, billingReferralInviterOneID, "")
		f.createUser(t, billingReferralInviterTwoID, "")
		f.createUser(t, billingReferralInviteeID, billingReferralInviterOneID)
		req := f.topUpRequest(billingReferralInviteeID, 10_000, "drift")
		if _, err := f.referrals.TopUp(context.Background(), req); err != nil {
			t.Fatal(err)
		}
		user, err := f.repo.Users().FindByID(context.Background(), billingReferralInviteeID)
		if err != nil {
			t.Fatal(err)
		}
		user.InvitedBy = billingReferralInviterTwoID
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
		f.createUser(t, billingReferralStandaloneID, "")
		req := f.topUpRequest(billingReferralStandaloneID, 10_000, "caller-tx")
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

func TestBillingReferralLocksUsersBeforeWalletMutation(t *testing.T) {
	t.Run("referral issue candidate is read before transaction locks", func(t *testing.T) {
		f, recorder := newRecordingBillingReferralFixture(t)
		if _, err := f.referrals.TopUp(context.Background(), f.topUpRequest(billingReferralInviteeID, 10_000, "issue-read-order")); err != nil {
			t.Fatal(err)
		}
		if len(recorder.events) == 0 || recorder.events[0] != "referral-find:root" {
			t.Fatalf("first referral event = %v, want root candidate read", recorder.events)
		}
	})

	t.Run("first qualifying topup locks both users in stable order", func(t *testing.T) {
		f, recorder := newRecordingBillingReferralFixture(t)
		if _, err := f.referrals.TopUp(context.Background(), f.topUpRequest(billingReferralInviteeID, 10_000, "lock-order")); err != nil {
			t.Fatal(err)
		}
		assertReferralUserLocks(t, recorder.events, []string{
			"user-lock:" + billingReferralInviterID,
			"user-lock:" + billingReferralInviteeID,
		})
		assertAllReferralUserLocksPrecedeWallet(t, recorder.events)
	})

	t.Run("nonqualifying topup only locks invitee", func(t *testing.T) {
		f, recorder := newRecordingBillingReferralFixture(t)
		if _, err := f.referrals.TopUp(context.Background(), f.topUpRequest(billingReferralInviteeID, 9_999, "nonqualifying-locks")); err != nil {
			t.Fatal(err)
		}
		assertReferralUserLocks(t, recorder.events, []string{"user-lock:" + billingReferralInviteeID})
		assertNoReferralWalletCallForUser(t, recorder.events, billingReferralInviterID)
	})

	t.Run("existing issue replay only locks invitee", func(t *testing.T) {
		f, recorder := newRecordingBillingReferralFixture(t)
		if _, err := f.referrals.TopUp(context.Background(), f.topUpRequest(billingReferralInviteeID, 10_000, "issue-first")); err != nil {
			t.Fatal(err)
		}
		recorder.reset()
		if _, err := f.referrals.TopUp(context.Background(), f.topUpRequest(billingReferralInviteeID, 20_000, "issue-replay")); err != nil {
			t.Fatal(err)
		}
		assertReferralUserLocks(t, recorder.events, []string{"user-lock:" + billingReferralInviteeID})
		assertNoReferralWalletCallForUser(t, recorder.events, billingReferralInviterID)
	})

	t.Run("catalog provenance precedes wallet mutation", func(t *testing.T) {
		f, recorder := newRecordingBillingReferralFixture(t)
		req := f.topUpRequest(billingReferralInviteeID, 10_000, "missing-catalog")
		req.CatalogID = "missing-catalog-v1"
		if _, err := f.referrals.TopUp(context.Background(), req); !errors.Is(err, ErrBillingCatalogNotFound) {
			t.Fatalf("TopUp error = %v, want ErrBillingCatalogNotFound", err)
		}
		for _, event := range recorder.events {
			if len(event) >= 7 && event[:7] == "wallet-" {
				t.Fatalf("wallet mutation before catalog rejection: %v", recorder.events)
			}
		}
	})

	t.Run("locked invitation drift conflicts before wallet mutation", func(t *testing.T) {
		f, recorder := newRecordingBillingReferralFixture(t)
		f.createUser(t, billingReferralInviterTwoID, "")
		recorder.lockedInviteeInvitedBy = billingReferralInviterTwoID
		if _, err := f.referrals.TopUp(context.Background(), f.topUpRequest(billingReferralInviteeID, 10_000, "locked-drift")); !errors.Is(err, ErrBillingConflict) {
			t.Fatalf("TopUp error = %v, want ErrBillingConflict", err)
		}
		for _, event := range recorder.events {
			if len(event) >= 7 && event[:7] == "wallet-" {
				t.Fatalf("wallet mutation before identity conflict: %v", recorder.events)
			}
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

type referralLockRecorder struct {
	events                 []string
	lockedInviteeInvitedBy string
}

func (r *referralLockRecorder) reset() { r.events = nil }

type recordingReferralRepository struct {
	repository.Repository
	recorder *referralLockRecorder
	txBound  bool
}

func (r *recordingReferralRepository) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return r.Repository.WithTx(ctx, func(tx repository.Repository) error {
		return fn(&recordingReferralRepository{Repository: tx, recorder: r.recorder, txBound: true})
	})
}

func (r *recordingReferralRepository) Users() repository.UserRepository {
	return &recordingReferralUserRepository{UserRepository: r.Repository.Users(), recorder: r.recorder}
}

func (r *recordingReferralRepository) Billing() repository.BillingRepository {
	return &recordingReferralBillingRepository{BillingRepository: r.Repository.Billing(), recorder: r.recorder, txBound: r.txBound}
}

type recordingReferralUserRepository struct {
	repository.UserRepository
	recorder *referralLockRecorder
}

func (r *recordingReferralUserRepository) LockByID(ctx context.Context, userID string) (*model.User, error) {
	r.recorder.events = append(r.recorder.events, "user-lock:"+userID)
	user, err := r.UserRepository.LockByID(ctx, userID)
	if err == nil && userID == billingReferralInviteeID && r.recorder.lockedInviteeInvitedBy != "" {
		copy := *user
		copy.InvitedBy = r.recorder.lockedInviteeInvitedBy
		return &copy, nil
	}
	return user, err
}

type recordingReferralBillingRepository struct {
	repository.BillingRepository
	recorder *referralLockRecorder
	txBound  bool
}

func (r *recordingReferralBillingRepository) FindReferralIssue(ctx context.Context, inviteeUserID, programID string) (*model.BillingReferralIssue, error) {
	location := "root"
	if r.txBound {
		location = "tx"
	}
	r.recorder.events = append(r.recorder.events, "referral-find:"+location)
	return r.BillingRepository.FindReferralIssue(ctx, inviteeUserID, programID)
}

func (r *recordingReferralBillingRepository) LockAccount(ctx context.Context, userID string) (*model.BillingWalletAccount, error) {
	r.recorder.events = append(r.recorder.events, "wallet-lock:"+userID)
	return r.BillingRepository.LockAccount(ctx, userID)
}

func newRecordingBillingReferralFixture(t *testing.T) (*billingReferralFixture, *referralLockRecorder) {
	t.Helper()
	f := newBillingReferralFixture(t, 10)
	f.createUser(t, billingReferralInviterID, "")
	f.createUser(t, billingReferralInviteeID, billingReferralInviterID)
	recorder := &referralLockRecorder{}
	repo := &recordingReferralRepository{Repository: f.repo, recorder: recorder}
	f.wallet = NewBillingWalletService(repo, &f.bundle, BillingWalletOptions{Now: func() time.Time { return f.now }})
	f.referrals = NewBillingReferralService(repo, f.wallet, &f.bundle, BillingReferralOptions{Now: func() time.Time { return f.now }})
	return f, recorder
}

func assertReferralUserLocks(t *testing.T, events, want []string) {
	t.Helper()
	var got []string
	for _, event := range events {
		if len(event) >= 10 && event[:10] == "user-lock:" {
			got = append(got, event)
		}
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("user locks = %v, want %v; all events=%v", got, want, events)
	}
}

func assertAllReferralUserLocksPrecedeWallet(t *testing.T, events []string) {
	t.Helper()
	lastUser, firstWallet := -1, len(events)
	for index, event := range events {
		if len(event) >= 10 && event[:10] == "user-lock:" {
			lastUser = index
		}
		if len(event) >= 7 && event[:7] == "wallet-" && firstWallet == len(events) {
			firstWallet = index
		}
	}
	if lastUser < 0 || firstWallet == len(events) || lastUser >= firstWallet {
		t.Fatalf("user locks do not precede wallet calls: %v", events)
	}
}

func assertNoReferralWalletCallForUser(t *testing.T, events []string, userID string) {
	t.Helper()
	for _, event := range events {
		if event == "wallet-lock:"+userID {
			t.Fatalf("unexpected wallet call for %s: %v", userID, events)
		}
	}
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
	bundle.Economics.CreditsPerCNY = 1_000
	bundle.Promotions = billing.PromotionCatalog{
		CatalogID: "promotion-test-v1",
		Programs: []billing.ReferralProgram{{
			ID: billing.ReferralFirstTopUpProgramID, Trigger: "invitee_first_paid_topup", MinimumTopUpCNY: billing.MicroCNY(10_000_000),
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
	} else if err := repo.Billing().CreateCatalogVersion(context.Background(), &model.BillingCatalogVersion{
		CatalogID: bundle.Products.CatalogID, Currency: "credits", Status: "published", PublishedAt: now,
		Snapshot: []byte(`{}`), CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (f *billingReferralFixture) createUser(t *testing.T, userID, invitedBy string) {
	t.Helper()
	user, err := f.repo.Users().FindByID(context.Background(), userID)
	if err == nil {
		user.InvitedBy = invitedBy
		if err := f.repo.Users().Update(context.Background(), user); err != nil {
			t.Fatal(err)
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal(err)
	} else {
		if err := f.repo.Users().Create(context.Background(), &model.User{
			ID: userID, Email: userID + "@example.test", Password: "fixture", InviteCode: "code-" + userID, InvitedBy: invitedBy,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.repo.Billing().FindAccount(context.Background(), userID); errors.Is(err, gorm.ErrRecordNotFound) {
		if err := f.repo.Billing().CreateAccount(context.Background(), &model.BillingWalletAccount{UserID: userID}); err != nil {
			t.Fatal(err)
		}
	} else if err != nil {
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
	if lot.ID != *lotID || lot.UserID != userID || lot.ProgramID != billing.ReferralFirstTopUpProgramID || lot.CatalogID != "promotion-test-v1" ||
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
