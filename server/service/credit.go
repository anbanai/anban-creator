package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"

	"gorm.io/datatypes"
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

func creditTaskLabel(taskType string) string {
	switch taskType {
	case model.ScopeSeednote:
		return "种草笔记"
	case model.ScopeArticle:
		return "公众号文章"
	case model.ScopeEcommerce:
		return "电商出图"
	case model.ScopeVideo:
		return "视频生成"
	case model.CreditTypeViralAnalysis:
		return "爆文拆解"
	default:
		return taskType
	}
}

func creditOperationLabel(opType string) string {
	switch opType {
	case model.CreditTypeImageGen:
		return "AI 生图"
	case model.CreditTypeImageUnderstanding:
		return "图片理解"
	case model.CreditTypeArticleWrite:
		return "文章写作"
	case model.CreditTypeConvert:
		return "格式转换"
	case model.CreditTypeHumanize:
		return "文章润色"
	case model.CreditTypeTopicResearch:
		return "选题研究"
	case model.CreditTypeSEO:
		return "SEO 优化"
	case model.CreditTypeOutline:
		return "大纲生成"
	case model.CreditTypeVideoGen:
		return "视频生成"
	case model.CreditTypeVideoUnderstanding:
		return "视频理解"
	case model.CreditTypePosterGeneration:
		return "海报生成"
	case model.CreditTypeViralAnalysis:
		return "爆文拆解"
	default:
		return opType
	}
}

func creditTaskDeductDescription(taskType string, amount int, multiplier int) string {
	label := creditTaskLabel(taskType)
	if multiplier > 1 {
		return fmt.Sprintf("生成%s（强目标 x%d）扣除积分%d", label, multiplier, amount)
	}
	return fmt.Sprintf("生成%s扣除积分%d", label, amount)
}

func creditOperationDeductDescription(opType string, amount int) string {
	return fmt.Sprintf("%s扣除积分%d", creditOperationLabel(opType), amount)
}

// GetBalance returns the current credit balance for a user.
func (s *CreditService) GetBalance(ctx context.Context, userID string) (int, error) {
	user, err := s.repo.Users().FindByID(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("find user: %w", err)
	}
	return user.CreditsBalance, nil
}

func (s *CreditService) GetUserTier(ctx context.Context, userID string) (model.Tier, error) {
	user, err := s.repo.Users().FindByID(ctx, userID)
	if err != nil {
		return model.TierFree, fmt.Errorf("find user: %w", err)
	}
	return model.NormalizeTier(string(user.Tier)), nil
}

func (s *CreditService) GetUserBillingMultiplier(ctx context.Context, userID string) (float64, error) {
	user, err := s.repo.Users().FindByID(ctx, userID)
	if err != nil {
		return 1, fmt.Errorf("find user: %w", err)
	}
	if user.BillingMultiplier == nil || *user.BillingMultiplier <= 0 {
		return 1, nil
	}
	return *user.BillingMultiplier, nil
}

func (s *CreditService) ValidateTaskOwnership(ctx context.Context, userID, taskID string) error {
	if taskID == "" {
		return nil
	}
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("validate billing task: %w", err)
	}
	if task.UserID != userID {
		return fmt.Errorf("validate billing task: task does not belong to user")
	}
	return nil
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

// GrantBonus adds credits to a user's account as a bonus (registration, invite, etc.).
func (s *CreditService) GrantBonus(ctx context.Context, userID string, amount int, bonusType, description string) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}

	return s.repo.WithTx(ctx, func(txRepo repository.Repository) error {
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
			Type:         bonusType,
			Amount:       amount,
			BalanceAfter: newBalance,
			Description:  description,
		}
		if err := txRepo.Credits().CreateTransaction(ctx, tx); err != nil {
			return fmt.Errorf("create bonus transaction: %w", err)
		}
		s.logger.Info().Str("user_id", userID).Str("type", bonusType).Int("amount", amount).Int("balance", newBalance).Msg("bonus credits granted")
		return nil
	})
}

// DeductForTask deducts credits for a single task creation.
// The optional multiplier (defaults to 1 when omitted) scales the base task
// cost — used by goal-mode tasks which charge goal_mode_multiplier × base cost
// upfront to cover all retry attempts.
func (s *CreditService) DeductForTask(ctx context.Context, userID, taskType, taskID string, multiplier ...int) (int, error) {
	cost, ok := s.cfg.TaskCosts[taskType]
	if !ok {
		return 0, ErrUnknownTaskType
	}
	m := 1
	if len(multiplier) > 0 && multiplier[0] > 0 {
		m = multiplier[0]
	}
	totalCost := cost * m

	var newBalance int
	err := s.repo.WithTx(ctx, func(txRepo repository.Repository) error {
		var ok bool
		var err error
		newBalance, ok, err = txRepo.Users().DeductCredits(ctx, userID, totalCost)
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
			Amount:       -totalCost,
			BalanceAfter: newBalance,
			TaskID:       &taskIDCopy,
			Description:  creditTaskDeductDescription(taskType, totalCost, m),
		}
		if err := txRepo.Credits().CreateTransaction(ctx, tx); err != nil {
			return fmt.Errorf("create deduction transaction: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}

	s.logger.Info().Str("user_id", userID).Str("task_id", taskID).Int("cost", totalCost).Int("multiplier", m).Int("balance", newBalance).Msg("credits deducted for task")
	return newBalance, nil
}

// RefundForTask refunds credits for a failed or cancelled task.
// The reason parameter controls the transaction description ("failed" or "cancel").
func (s *CreditService) RefundForTask(ctx context.Context, taskID string, reason ...string) error {
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
		desc := "任务失败退还"
		if len(reason) > 0 && reason[0] == "cancel" {
			desc = "任务取消退还"
		}
		tx := &model.CreditTransaction{
			UserID:       deduction.UserID,
			Type:         model.CreditTypeTaskRefund,
			Amount:       refundAmount,
			BalanceAfter: newBalance,
			TaskID:       &taskIDCopy,
			Description:  fmt.Sprintf("%s +%d", desc, refundAmount),
		}
		if err := txRepo.Credits().CreateTransaction(ctx, tx); err != nil {
			return fmt.Errorf("create refund transaction: %w", err)
		}

		s.logger.Info().Str("task_id", taskID).Int("refund", refundAmount).Msg("credits refunded for task")
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
				Description:  creditTaskDeductDescription(taskType, costPerTask, 1),
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

// DeductBatchWithMultiplier is a typed alternative to DeductBatch that records
// a "强目标任务扣费" description with the multiplier. Callers must pre-multiply
// totalCost to reflect the multiplier (e.g. cost * quantity * multiplier).
func (s *CreditService) DeductBatchWithMultiplier(ctx context.Context, userID, taskType string, totalCost int, taskIDs []string, multiplier int) error {
	if multiplier <= 1 {
		return s.DeductBatch(ctx, userID, taskType, totalCost, taskIDs)
	}
	if totalCost <= 0 || len(taskIDs) == 0 {
		return ErrInvalidAmount
	}

	costPerTask := totalCost / len(taskIDs)

	return s.repo.WithTx(ctx, func(txRepo repository.Repository) error {
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
				Description:  creditTaskDeductDescription(taskType, costPerTask, multiplier),
			}
			if err := txRepo.Credits().CreateTransaction(ctx, tx); err != nil {
				return fmt.Errorf("create deduction transaction: %w", err)
			}
		}
		return nil
	})
}

// TaskCost returns the credit cost for a given task type.
func (s *CreditService) TaskCost(taskType string) (int, bool) {
	cost, ok := s.cfg.TaskCosts[taskType]
	return cost, ok
}

// TaskCosts returns the configured per-task-type costs.
func (s *CreditService) TaskCosts() map[string]int {
	return s.cfg.TaskCosts
}

// ModelCosts returns the configured per-model costs.
func (s *CreditService) ModelCosts() map[string]map[string]int {
	return s.cfg.ModelCosts
}

// DeductForOperation deducts credits for a single MCP tool operation.
// The amount parameter is the total credits to deduct (already calculated by the caller).
// Optional args are operationID, then taskID. operationID gives refund idempotency;
// taskID lets task detail pages show operation-level credit consumption.
func (s *CreditService) DeductForOperation(ctx context.Context, userID, opType string, amount int, operationID ...string) (int, error) {
	return s.deductForOperation(ctx, userID, opType, amount, nil, operationID...)
}

func (s *CreditService) DeductForOperationWithMetadata(ctx context.Context, userID, opType string, amount int, metadata model.CreditTransactionMetadata, operationID ...string) (int, error) {
	return s.deductForOperation(ctx, userID, opType, amount, &metadata, operationID...)
}

func (s *CreditService) deductForOperation(ctx context.Context, userID, opType string, amount int, metadata *model.CreditTransactionMetadata, operationID ...string) (int, error) {
	if amount <= 0 {
		return 0, ErrInvalidAmount
	}
	totalCost := amount
	var metadataJSON datatypes.JSON
	if metadata != nil {
		data, err := json.Marshal(metadata)
		if err != nil {
			return 0, fmt.Errorf("marshal operation metadata: %w", err)
		}
		metadataJSON = datatypes.JSON(data)
	}

	var newBalance int
	err := s.repo.WithTx(ctx, func(txRepo repository.Repository) error {
		var ok bool
		var err error
		newBalance, ok, err = txRepo.Users().DeductCredits(ctx, userID, totalCost)
		if err != nil {
			return fmt.Errorf("deduct credits: %w", err)
		}
		if !ok {
			return ErrInsufficientCredits
		}

		tx := &model.CreditTransaction{
			UserID:       userID,
			Type:         opType,
			Amount:       -totalCost,
			BalanceAfter: newBalance,
			Description:  creditOperationDeductDescription(opType, totalCost),
			Metadata:     metadataJSON,
		}
		if len(operationID) > 0 && operationID[0] != "" {
			opIDCopy := operationID[0]
			tx.OperationID = &opIDCopy
		}
		if len(operationID) > 1 && operationID[1] != "" {
			taskIDCopy := operationID[1]
			tx.TaskID = &taskIDCopy
		}
		if err := txRepo.Credits().CreateTransaction(ctx, tx); err != nil {
			return fmt.Errorf("create deduction transaction: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}

	s.logger.Info().Str("user_id", userID).Str("op_type", opType).Int("amount", totalCost).Int("balance", newBalance).Msg("credits deducted for operation")
	return newBalance, nil
}

// RefundForOperation refunds credits for a failed operation.
// When operationID is provided, the refund is idempotent — calling it multiple times
// with the same operationID will only refund once.
func (s *CreditService) RefundForOperation(ctx context.Context, userID, opType string, amount int, description string, operationID ...string) error {
	if amount <= 0 {
		return nil
	}

	return s.repo.WithTx(ctx, func(txRepo repository.Repository) error {
		// Idempotency check when operationID is provided.
		if len(operationID) > 0 && operationID[0] != "" {
			_, err := txRepo.Credits().FindRefundByOperationID(ctx, operationID[0])
			if err == nil {
				s.logger.Warn().Str("operation_id", operationID[0]).Msg("operation refund already exists, skipping")
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("check existing refund: %w", err)
			}
		}

		newBalance, err := txRepo.Users().AdjustBalance(ctx, userID, amount)
		if err != nil {
			return fmt.Errorf("adjust balance: %w", err)
		}

		tx := &model.CreditTransaction{
			UserID:       userID,
			Type:         opType,
			Amount:       amount,
			BalanceAfter: newBalance,
			Description:  description,
		}
		if len(operationID) > 0 && operationID[0] != "" {
			opIDCopy := operationID[0]
			tx.OperationID = &opIDCopy
		}
		if err := txRepo.Credits().CreateTransaction(ctx, tx); err != nil {
			return fmt.Errorf("create refund transaction: %w", err)
		}

		s.logger.Info().Str("user_id", userID).Str("op_type", opType).Int("refund", amount).Int("balance", newBalance).Msg("credits refunded for operation")
		return nil
	})
}

// RefundForOperationByID refunds credits for an operation identified by its operationID.
// It derives the user and amount from the original deduction transaction.
// Idempotent — calling it multiple times with the same operationID will only refund once.
func (s *CreditService) RefundForOperationByID(ctx context.Context, operationID string, description string) error {
	return s.repo.WithTx(ctx, func(txRepo repository.Repository) error {
		// Check for existing refund.
		_, err := txRepo.Credits().FindRefundByOperationID(ctx, operationID)
		if err == nil {
			s.logger.Warn().Str("operation_id", operationID).Msg("operation refund already exists, skipping")
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("check existing refund: %w", err)
		}

		// Find original deduction to derive user and amount.
		deduction, err := txRepo.Credits().FindDeductionByOperationID(ctx, operationID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				s.logger.Warn().Str("operation_id", operationID).Msg("no deduction found for operation, skipping refund")
				return nil
			}
			return fmt.Errorf("find deduction: %w", err)
		}

		refundAmount := -deduction.Amount
		newBalance, err := txRepo.Users().AdjustBalance(ctx, deduction.UserID, refundAmount)
		if err != nil {
			return fmt.Errorf("adjust balance: %w", err)
		}

		opIDCopy := operationID
		tx := &model.CreditTransaction{
			UserID:       deduction.UserID,
			Type:         deduction.Type,
			Amount:       refundAmount,
			BalanceAfter: newBalance,
			OperationID:  &opIDCopy,
			Description:  description,
		}
		if deduction.TaskID != nil && *deduction.TaskID != "" {
			taskIDCopy := *deduction.TaskID
			tx.TaskID = &taskIDCopy
		}
		if err := txRepo.Credits().CreateTransaction(ctx, tx); err != nil {
			return fmt.Errorf("create refund transaction: %w", err)
		}

		s.logger.Info().Str("operation_id", operationID).Int("refund", refundAmount).Int("balance", newBalance).Msg("credits refunded for operation by ID")
		return nil
	})
}

// DeductForTaskWithAmount deducts an explicit task_deduct amount for legacy or
// exceptional paths that cannot use the fixed TaskCosts table. Normal task
// creation, including e-commerce and video, should use DeductForTask/DeductBatch
// so creation billing remains a base service fee.
func (s *CreditService) DeductForTaskWithAmount(ctx context.Context, userID, taskType, taskID string, amount int) (int, error) {
	if amount <= 0 {
		return 0, ErrInvalidAmount
	}

	var newBalance int
	err := s.repo.WithTx(ctx, func(txRepo repository.Repository) error {
		var ok bool
		var err error
		newBalance, ok, err = txRepo.Users().DeductCredits(ctx, userID, amount)
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
			Amount:       -amount,
			BalanceAfter: newBalance,
			TaskID:       &taskIDCopy,
			Description:  creditTaskDeductDescription(taskType, amount, 1),
		}
		if err := txRepo.Credits().CreateTransaction(ctx, tx); err != nil {
			return fmt.Errorf("create deduction transaction: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}

	s.logger.Info().Str("user_id", userID).Str("task_id", taskID).Str("task_type", taskType).Int("cost", amount).Int("balance", newBalance).Msg("credits deducted for task (explicit amount)")
	return newBalance, nil
}

// EcommercePackageCost sums the per-module unit price × quantity over selected
// deliverable modules for delivery-scale estimates. It is not used as the
// creation-time charge; e-commerce creation deducts TaskCosts["ecommerce"].
func (s *CreditService) EcommercePackageCost(selected map[string]int) (int, bool) {
	total := 0
	known := true
	for module, count := range selected {
		if count <= 0 {
			continue
		}
		price, ok := s.cfg.EcommerceModulePrices[module]
		if !ok {
			known = false
			continue
		}
		total += price * count
	}
	return total, known
}

// EcommerceModulePrices returns configured per-module unit prices for delivery
// scale estimates in UI and planning surfaces.
func (s *CreditService) EcommerceModulePrices() map[string]int {
	return s.cfg.EcommerceModulePrices
}
