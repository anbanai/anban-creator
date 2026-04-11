package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"

	"gorm.io/gorm"
)

var (
	ErrAlreadySignedIn     = errors.New("already signed in today")
	ErrInsufficientCredits = errors.New("insufficient credits")
	ErrInvalidAmount       = errors.New("invalid credit amount")
	ErrUnknownTaskType     = errors.New("unknown task type for credit costing")
)

// CreditService handles credits/points business logic.
type CreditService struct {
	repo   repository.Repository
	cfg    *config.CreditsConfig
	logger *zerolog.Logger
}

// NewCreditService creates a new CreditService.
func NewCreditService(repo repository.Repository, cfg *config.CreditsConfig, logger *zerolog.Logger) *CreditService {
	return &CreditService{repo: repo, cfg: cfg, logger: logger}
}

// GetBalance returns the current credit balance for a user.
func (s *CreditService) GetBalance(ctx context.Context, userID string) (int, error) {
	user, err := s.repo.Users().FindByID(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("find user: %w", err)
	}
	return user.CreditsBalance, nil
}

// GetSignInStatus returns whether the user has signed in today.
func (s *CreditService) GetSignInStatus(ctx context.Context, userID string) (bool, error) {
	_, err := s.repo.Credits().FindTodaySignIn(ctx, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("check sign-in status: %w", err)
	}
	return true, nil
}

// SignIn awards daily sign-in credits.
func (s *CreditService) SignIn(ctx context.Context, userID string) (int, error) {
	var newBalance int
	err := s.repo.WithTx(ctx, func(txRepo repository.Repository) error {
		// Check sign-in status INSIDE the transaction to prevent race.
		_, err := txRepo.Credits().FindTodaySignIn(ctx, userID)
		if err == nil {
			// Found existing sign-in today.
			return ErrAlreadySignedIn
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("check sign-in: %w", err)
		}

		newBalance, err = txRepo.Users().AdjustBalance(ctx, userID, s.cfg.DailySignIn)
		if err != nil {
			return fmt.Errorf("adjust balance: %w", err)
		}

		tx := &model.CreditTransaction{
			UserID:       userID,
			Type:         model.CreditTypeSignIn,
			Amount:       s.cfg.DailySignIn,
			BalanceAfter: newBalance,
			Description:  fmt.Sprintf("每日签到 +%d", s.cfg.DailySignIn),
		}
		if err := txRepo.Credits().CreateTransaction(ctx, tx); err != nil {
			return fmt.Errorf("create sign-in transaction: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}

	s.logger.Info().Str("user_id", userID).Int("balance", newBalance).Msg("user signed in")
	return newBalance, nil
}

// ListTransactions returns paginated credit transactions for a user.
func (s *CreditService) ListTransactions(ctx context.Context, userID string, offset, limit int) ([]*model.CreditTransaction, int64, error) {
	transactions, err := s.repo.Credits().FindByUserID(ctx, userID, offset, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list transactions: %w", err)
	}
	total, err := s.repo.Credits().CountByUserID(ctx, userID)
	if err != nil {
		return nil, 0, fmt.Errorf("count transactions: %w", err)
	}
	return transactions, total, nil
}

// AdminGrant adds credits to a user's account (admin operation).
func (s *CreditService) AdminGrant(ctx context.Context, userID string, amount int, description string) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}

	return s.repo.WithTx(ctx, func(txRepo repository.Repository) error {
		// Verify user exists.
		_, err := txRepo.Users().FindByID(ctx, userID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("user not found: %s", userID)
			}
			return fmt.Errorf("find user: %w", err)
		}

		newBalance, err := txRepo.Users().AdjustBalance(ctx, userID, amount)
		if err != nil {
			return fmt.Errorf("adjust balance: %w", err)
		}

		tx := &model.CreditTransaction{
			UserID:       userID,
			Type:         model.CreditTypeAdminGrant,
			Amount:       amount,
			BalanceAfter: newBalance,
			Description:  description,
		}
		if err := txRepo.Credits().CreateTransaction(ctx, tx); err != nil {
			return fmt.Errorf("create grant transaction: %w", err)
		}
		s.logger.Info().Str("user_id", userID).Int("amount", amount).Int("balance", newBalance).Msg("admin granted credits")
		return nil
	})
}

// DeductForTask deducts credits for a single task creation.
func (s *CreditService) DeductForTask(ctx context.Context, userID, taskType, taskID string) (int, error) {
	cost, ok := s.cfg.TaskCosts[taskType]
	if !ok {
		return 0, ErrUnknownTaskType
	}

	var newBalance int
	err := s.repo.WithTx(ctx, func(txRepo repository.Repository) error {
		var ok bool
		var err error
		newBalance, ok, err = txRepo.Users().DeductCredits(ctx, userID, cost)
		if err != nil {
			return fmt.Errorf("deduct credits: %w", err)
		}
		if !ok {
			return ErrInsufficientCredits
		}

		taskIDCopy := taskID
		tx := &model.CreditTransaction{
			UserID:       userID,
			Type:         model.CreditTypeTaskDeduct,
			Amount:       -cost,
			BalanceAfter: newBalance,
			TaskID:       &taskIDCopy,
			Description:  fmt.Sprintf("任务扣费 (%s) -%d", taskType, cost),
		}
		if err := txRepo.Credits().CreateTransaction(ctx, tx); err != nil {
			return fmt.Errorf("create deduction transaction: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}

	s.logger.Info().Str("user_id", userID).Str("task_id", taskID).Int("cost", cost).Int("balance", newBalance).Msg("credits deducted for task")
	return newBalance, nil
}

// RefundForTask refunds credits for a failed task.
func (s *CreditService) RefundForTask(ctx context.Context, taskID string) error {
	return s.repo.WithTx(ctx, func(txRepo repository.Repository) error {
		// Check for existing refund to prevent double-refund.
		_, err := txRepo.Credits().FindRefundByTaskID(ctx, taskID)
		if err == nil {
			s.logger.Warn().Str("task_id", taskID).Msg("refund already exists, skipping")
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("check existing refund: %w", err)
		}

		deduction, err := txRepo.Credits().FindDeductionByTaskID(ctx, taskID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				s.logger.Warn().Str("task_id", taskID).Msg("no deduction found for task, skipping refund")
				return nil
			}
			return fmt.Errorf("find deduction: %w", err)
		}

		refundAmount := -deduction.Amount

		newBalance, err := txRepo.Users().AdjustBalance(ctx, deduction.UserID, refundAmount)
		if err != nil {
			return fmt.Errorf("adjust balance: %w", err)
		}

		taskIDCopy := taskID
		tx := &model.CreditTransaction{
			UserID:       deduction.UserID,
			Type:         model.CreditTypeTaskRefund,
			Amount:       refundAmount,
			BalanceAfter: newBalance,
			TaskID:       &taskIDCopy,
			Description:  fmt.Sprintf("任务失败退还 +%d", refundAmount),
		}
		if err := txRepo.Credits().CreateTransaction(ctx, tx); err != nil {
			return fmt.Errorf("create refund transaction: %w", err)
		}

		s.logger.Info().Str("task_id", taskID).Int("refund", refundAmount).Msg("credits refunded for failed task")
		return nil
	})
}

// DeductBatch deducts the total cost for multiple tasks in a single atomic transaction.
// It creates individual transaction records for each taskID.
// If repo is nil, it uses the service's default repository (creating its own transaction).
// If repo is provided, it operates within that repository's transaction context.
func (s *CreditService) DeductBatch(ctx context.Context, userID, taskType string, totalCost int, taskIDs []string, repo ...repository.Repository) error {
	if totalCost <= 0 || len(taskIDs) == 0 {
		return ErrInvalidAmount
	}

	costPerTask := totalCost / len(taskIDs)

	deduct := func(txRepo repository.Repository) error {
		newBalance, ok, err := txRepo.Users().DeductCredits(ctx, userID, totalCost)
		if err != nil {
			return fmt.Errorf("deduct credits: %w", err)
		}
		if !ok {
			return ErrInsufficientCredits
		}

		for _, taskID := range taskIDs {
			taskIDCopy := taskID
			tx := &model.CreditTransaction{
				UserID:       userID,
				Type:         model.CreditTypeTaskDeduct,
				Amount:       -costPerTask,
				BalanceAfter: newBalance,
				TaskID:       &taskIDCopy,
				Description:  fmt.Sprintf("任务扣费 (%s) -%d", taskType, costPerTask),
			}
			if err := txRepo.Credits().CreateTransaction(ctx, tx); err != nil {
				return fmt.Errorf("create deduction transaction: %w", err)
			}
		}
		return nil
	}

	// If a repo is provided, use it directly (caller manages the transaction).
	if len(repo) > 0 && repo[0] != nil {
		return deduct(repo[0])
	}
	// Otherwise, create our own transaction.
	return s.repo.WithTx(ctx, deduct)
}

// TaskCost returns the credit cost for a given task type.
func (s *CreditService) TaskCost(taskType string) (int, bool) {
	cost, ok := s.cfg.TaskCosts[taskType]
	return cost, ok
}
