package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"gorm.io/gorm"
)

type providerRequestRejectedError struct{ cause error }

func (e *providerRequestRejectedError) Error() string { return e.cause.Error() }
func (e *providerRequestRejectedError) Unwrap() error { return e.cause }

func (s *BillingWalletService) reuseStandaloneOperationIdentity(ctx context.Context, userID, taskID, scope, fingerprint string) (*model.Task, string, error) {
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fingerprint, nil
	}
	if err != nil {
		return nil, "", err
	}
	if task.ReuseRunID == "" {
		return nil, fingerprint, nil
	}
	if task.UserID != userID {
		return nil, "", ErrBillingQuoteMismatch
	}
	run, err := s.repo.ContentAssets().FindReuseRun(ctx, userID, task.ReuseRunID)
	if err != nil {
		return nil, "", err
	}
	if run.Status != model.ContentReuseRunStatusRunning || run.TargetProjectID != task.ProjectID {
		return nil, "", repository.ErrContentReuseRunConflict
	}
	key := fingerprint
	for attempts := 0; attempts < 100; attempts++ {
		charge, err := s.repo.Billing().FindChargeByKey(ctx, scope, key)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return task, key, nil
		}
		if err != nil {
			return nil, "", err
		}
		if _, err := s.repo.Billing().FindReversal(ctx, charge.ID); errors.Is(err, gorm.ErrRecordNotFound) {
			return task, key, nil
		} else if err != nil {
			return nil, "", err
		}
		key = billingFingerprint(fingerprint, charge.ID)
	}
	return nil, "", fmt.Errorf("operation retry limit exceeded")
}

func (s *BillingWalletService) failReuseStandaloneOperation(ctx context.Context, task *model.Task, charge *model.BillingCharge, providerErr error) error {
	if task == nil || charge == nil {
		return nil
	}
	var rejection *providerRequestRejectedError
	if errors.As(providerErr, &rejection) {
		_, err := s.ReverseStandaloneOperation(context.WithoutCancel(ctx), charge.ID, "provider_error", "reuse-provider-failure:"+charge.ID)
		return err
	}
	return s.repo.ContentAssets().MarkReuseOperationAmbiguous(context.WithoutCancel(ctx), task.ReuseRunID, "")
}

func (s *BillingWalletService) stopStaleReuseOperation(ctx context.Context, userID, taskID string) (bool, error) {
	if s == nil {
		return false, nil
	}
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if task.UserID != userID || task.ReuseRunID == "" {
		return false, nil
	}
	return true, s.repo.ContentAssets().MarkReuseOperationAmbiguous(ctx, task.ReuseRunID, "")
}

func (s *BillingWalletService) reconcileReuseOperationHold(ctx context.Context, userID, taskID string) error {
	if s == nil {
		return nil
	}
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if task.UserID != userID || task.ReuseRunID == "" {
		return nil
	}
	_, err = s.repo.ContentAssets().ReconcileReuseOperationHold(context.WithoutCancel(ctx), userID, task.ReuseRunID)
	return err
}
