package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/anbanai/anban-creator/server/service"
)

const interactiveStartupAttempts = 3

// Run before opening the listener: retrying orphan cleanup must never touch an
// admitted session. A factory is exposed only after durable recovery succeeds.
func initializeInteractiveRuntime(ctx context.Context, svc *service.InteractiveService, factory service.RuntimeFactory, cleanup, recoverSessions func(context.Context) error, wait func(context.Context, time.Duration) error) (service.RuntimeFactory, error) {
	svc.SetStartupReady(false)
	if recoverSessions == nil {
		return nil, errors.New("interactive startup: durable session storage unavailable")
	}
	if wait == nil {
		wait = waitInteractiveStartup
	}
	var failure error
	for attempt := 1; attempt <= interactiveStartupAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		attemptCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		phase := "cleanup"
		var err error
		if cleanup != nil {
			err = cleanup(attemptCtx)
		}
		if err == nil {
			err = attemptCtx.Err()
		}
		if err == nil {
			phase = "recovery"
			err = recoverSessions(attemptCtx)
		}
		if err == nil {
			err = attemptCtx.Err()
		}
		attemptErr := attemptCtx.Err()
		cancel()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err == nil {
			svc.SetStartupReady(true)
			return factory, nil
		}
		// Runtime/database errors may include credentials. Retain only the phase
		// and timeout classification in the final startup diagnostic.
		reason := "failed"
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(attemptErr, context.DeadlineExceeded) {
			reason = "timed out"
		}
		failure = fmt.Errorf("interactive startup %s %s after %d attempts", phase, reason, attempt)
		if attempt < interactiveStartupAttempts {
			if err := wait(ctx, 5*time.Second); err != nil {
				return nil, err
			}
		}
	}
	return nil, failure
}

func waitInteractiveStartup(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
