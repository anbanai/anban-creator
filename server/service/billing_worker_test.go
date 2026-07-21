package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type billingMaintenanceResult struct {
	count   int
	claimed int
	err     error
}

type fakeBillingMaintenanceWallet struct {
	mu               sync.Mutex
	outbox           []billingMaintenanceResult
	expiry           []billingMaintenanceResult
	outboxCalls      int
	expiryCalls      int
	active           int
	maxActive        int
	delay            time.Duration
	blockUntilCancel bool
	called           chan struct{}
}

func (f *fakeBillingMaintenanceWallet) ProcessSettlementOutbox(ctx context.Context, _ int) (int, error) {
	processed, _, err := f.processSettlementOutboxBatch(ctx, 0)
	return processed, err
}

func (f *fakeBillingMaintenanceWallet) processSettlementOutboxBatch(ctx context.Context, _ int) (int, int, error) {
	result := f.call(ctx, true)
	claimed := result.claimed
	if claimed == 0 && result.count > 0 {
		claimed = result.count
	}
	return result.count, claimed, result.err
}

func (f *fakeBillingMaintenanceWallet) ExpirePromotionalCredits(ctx context.Context, _ time.Time, _ int) (int, error) {
	result := f.call(ctx, false)
	return result.count, result.err
}

func (f *fakeBillingMaintenanceWallet) call(ctx context.Context, outbox bool) billingMaintenanceResult {
	f.mu.Lock()
	f.active++
	if f.active > f.maxActive {
		f.maxActive = f.active
	}
	if outbox {
		f.outboxCalls++
	} else {
		f.expiryCalls++
	}
	if f.called != nil {
		select {
		case f.called <- struct{}{}:
		default:
		}
	}
	var result billingMaintenanceResult
	if outbox && len(f.outbox) > 0 {
		result, f.outbox = f.outbox[0], f.outbox[1:]
	}
	if !outbox && len(f.expiry) > 0 {
		result, f.expiry = f.expiry[0], f.expiry[1:]
	}
	delay, block := f.delay, f.blockUntilCancel
	f.mu.Unlock()

	if block {
		<-ctx.Done()
		result.err = ctx.Err()
	} else if delay > 0 {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			result.err = ctx.Err()
		case <-timer.C:
		}
	}

	f.mu.Lock()
	f.active--
	f.mu.Unlock()
	return result
}

func (f *fakeBillingMaintenanceWallet) counts() (outbox, expiry, maxActive int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.outboxCalls, f.expiryCalls, f.maxActive
}

func TestBillingMaintenanceWorkerRunsImmediatelyAndOnTicksWithoutOverlap(t *testing.T) {
	wallet := &fakeBillingMaintenanceWallet{delay: 5 * time.Millisecond}
	worker := NewBillingMaintenanceWorker(wallet, BillingMaintenanceWorkerOptions{
		OutboxInterval: 2 * time.Millisecond, ExpiryInterval: 3 * time.Millisecond,
		OutboxBatchSize: 10, ExpiryBatchSize: 10, MaxBatchesPerRun: 1,
	}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); worker.Run(ctx) }()
	waitForBillingWorker(t, func() bool {
		outbox, expiry, _ := wallet.counts()
		return outbox >= 2 && expiry >= 2
	})
	cancel()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("worker did not stop promptly after cancellation")
	}
	if _, _, maxActive := wallet.counts(); maxActive != 1 {
		t.Fatalf("worker max concurrent maintenance calls = %d, want 1", maxActive)
	}
}

func TestBillingMaintenanceWorkerDrainsFullBatchesWithBound(t *testing.T) {
	wallet := &fakeBillingMaintenanceWallet{
		outbox: []billingMaintenanceResult{{count: 2}, {count: 2}, {count: 1}},
		expiry: []billingMaintenanceResult{{count: 3}, {count: 3}, {count: 3}, {count: 3}},
	}
	worker := NewBillingMaintenanceWorker(wallet, BillingMaintenanceWorkerOptions{
		OutboxInterval: time.Hour, ExpiryInterval: time.Hour,
		OutboxBatchSize: 2, ExpiryBatchSize: 3, MaxBatchesPerRun: 3,
	}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); worker.Run(ctx) }()
	waitForBillingWorker(t, func() bool {
		outbox, expiry, _ := wallet.counts()
		return outbox == 3 && expiry == 3
	})
	cancel()
	<-done
	if outbox, expiry, _ := wallet.counts(); outbox != 3 || expiry != 3 {
		t.Fatalf("bounded drain calls = outbox %d expiry %d, want 3 and 3", outbox, expiry)
	}
}

func TestBillingMaintenanceWorkerDrainsAfterFullRetryBatch(t *testing.T) {
	wallet := &fakeBillingMaintenanceWallet{outbox: []billingMaintenanceResult{
		{count: 0, claimed: 2},
		{count: 0, claimed: 1},
	}}
	worker := NewBillingMaintenanceWorker(wallet, BillingMaintenanceWorkerOptions{
		OutboxInterval: time.Hour, ExpiryInterval: time.Hour,
		OutboxBatchSize: 2, ExpiryBatchSize: 10, MaxBatchesPerRun: 3,
	}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); worker.Run(ctx) }()
	waitForBillingWorker(t, func() bool {
		outbox, _, _ := wallet.counts()
		return outbox >= 2
	})
	cancel()
	<-done
}

func TestBillingMaintenanceWorkerContinuesAfterTransientError(t *testing.T) {
	wallet := &fakeBillingMaintenanceWallet{outbox: []billingMaintenanceResult{{err: errors.New("temporary")}, {count: 0}}}
	worker := NewBillingMaintenanceWorker(wallet, BillingMaintenanceWorkerOptions{
		OutboxInterval: 5 * time.Millisecond, ExpiryInterval: time.Hour,
		OutboxBatchSize: 10, ExpiryBatchSize: 10, MaxBatchesPerRun: 1,
	}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); worker.Run(ctx) }()
	waitForBillingWorker(t, func() bool {
		outbox, _, _ := wallet.counts()
		return outbox >= 2
	})
	cancel()
	<-done
}

func TestBillingMaintenanceWorkerCancellationInterruptsActiveCall(t *testing.T) {
	wallet := &fakeBillingMaintenanceWallet{blockUntilCancel: true, called: make(chan struct{}, 1)}
	worker := NewBillingMaintenanceWorker(wallet, BillingMaintenanceWorkerOptions{
		OutboxInterval: time.Hour, ExpiryInterval: time.Hour,
		OutboxBatchSize: 10, ExpiryBatchSize: 10, MaxBatchesPerRun: 1,
	}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); worker.Run(ctx) }()
	select {
	case <-wallet.called:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("worker did not run immediately")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("worker did not interrupt active maintenance call")
	}
}

func waitForBillingWorker(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for billing maintenance worker")
}
