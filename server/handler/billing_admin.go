package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type BillingAdminHandler struct {
	margin    *service.MarginService
	repo      repository.Repository
	referrals *service.BillingReferralService
	catalogID string
}

type BillingAdminHandlerOptions struct {
	Repo      repository.Repository
	Referrals *service.BillingReferralService
	CatalogID string
}

func NewBillingAdminHandler(margin *service.MarginService, options ...BillingAdminHandlerOptions) *BillingAdminHandler {
	h := &BillingAdminHandler{margin: margin}
	if len(options) > 0 {
		h.repo = options[0].Repo
		h.referrals = options[0].Referrals
		h.catalogID = strings.TrimSpace(options[0].CatalogID)
	}
	return h
}

func (h *BillingAdminHandler) Costs(c fiber.Ctx) error {
	from, to, err := billingReportWindow(c)
	if err != nil {
		return billingErrorResponse(c, fiber.StatusBadRequest, BillingCodeInvalid, "billing_invalid")
	}
	report, err := h.margin.Report(c.Context(), from, to, c.Query("group_by", "provider"), true)
	if err != nil {
		return writeBillingServiceError(c, err)
	}
	return Success(c, report)
}

func (h *BillingAdminHandler) Margins(c fiber.Ctx) error {
	from, to, err := billingReportWindow(c)
	if err != nil {
		return billingErrorResponse(c, fiber.StatusBadRequest, BillingCodeInvalid, "billing_invalid")
	}
	report, err := h.margin.Report(c.Context(), from, to, c.Query("group_by", "day"), false)
	if err != nil {
		return writeBillingServiceError(c, err)
	}
	return Success(c, report)
}

func (h *BillingAdminHandler) Reconciliation(c fiber.Ctx) error {
	report, err := h.margin.Reconcile(c.Context(), time.Now().UTC())
	if err != nil {
		return writeBillingServiceError(c, err)
	}
	return Success(c, report)
}

type adminBillingUserResponse struct {
	ID       string                `json:"id"`
	Email    string                `json:"email"`
	Nickname string                `json:"nickname"`
	Tier     model.Tier            `json:"tier"`
	IsAdmin  bool                  `json:"is_admin"`
	Wallet   BillingWalletResponse `json:"wallet"`
}

type adminTopUpOperationResponse struct {
	ID                 string                            `json:"id"`
	UserID             string                            `json:"user_id"`
	AdminUserID        string                            `json:"admin_user_id"`
	Credits            int64                             `json:"credits"`
	ExternalSourceType string                            `json:"external_source_type"`
	ExternalSourceID   string                            `json:"external_source_id"`
	Status             model.BillingTopUpOperationStatus `json:"status"`
	OriginalStatus     model.BillingTopUpOperationStatus `json:"original_status,omitempty"`
	ReplayOfID         string                            `json:"replay_of_id,omitempty"`
	ErrorCode          string                            `json:"error_code,omitempty"`
	ErrorMessage       string                            `json:"error_message,omitempty"`
	EntryID            string                            `json:"entry_id,omitempty"`
	PaidCreditsAdded   int64                             `json:"paid_credits_added"`
	DebtRepaidCredits  int64                             `json:"debt_repaid_credits"`
	CreatedAt          time.Time                         `json:"created_at"`
	UpdatedAt          time.Time                         `json:"updated_at"`
}

type adminTopUpRequest struct {
	UserID           string `json:"user_id"`
	Credits          int64  `json:"credits"`
	ExternalSourceID string `json:"external_source_id"`
}

type adminWalletEntryResponse struct {
	ID               string                       `json:"id"`
	UserID           string                       `json:"user_id"`
	EventKind        model.BillingWalletEventKind `json:"event_kind"`
	PaidDelta        int64                        `json:"paid_delta"`
	PromotionalDelta int64                        `json:"promotional_delta"`
	DebtDelta        int64                        `json:"debt_delta"`
	CatalogID        string                       `json:"catalog_id,omitempty"`
	SourceType       *string                      `json:"source_type,omitempty"`
	SourceID         *string                      `json:"source_id,omitempty"`
	ResourceType     string                       `json:"resource_type,omitempty"`
	ResourceID       string                       `json:"resource_id,omitempty"`
	CreatedAt        time.Time                    `json:"created_at"`
}

func (h *BillingAdminHandler) requireAdmin(c fiber.Ctx) (string, bool, error) {
	if h.repo == nil {
		return "", false, Error(c, fiber.StatusInternalServerError, "billing admin is not configured")
	}
	userID := strings.TrimSpace(GetUserID(c))
	if userID == "" {
		return "", false, billingErrorResponse(c, fiber.StatusUnauthorized, BillingCodeUnauthorized, "billing_unauthorized")
	}
	user, err := h.repo.Users().FindByID(c.Context(), userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, billingErrorResponse(c, fiber.StatusUnauthorized, BillingCodeUnauthorized, "billing_unauthorized")
	}
	if err != nil {
		return "", false, writeBillingServiceError(c, err)
	}
	if user == nil {
		return "", false, billingErrorResponse(c, fiber.StatusUnauthorized, BillingCodeUnauthorized, "billing_unauthorized")
	}
	if !user.IsAdmin {
		return "", false, Forbidden(c, "administrator access required")
	}
	return userID, true, nil
}

func (h *BillingAdminHandler) Users(c fiber.Ctx) error {
	if _, ok, err := h.requireAdmin(c); !ok {
		return err
	}
	users, err := h.repo.Users().Search(c.Context(), c.Query("q"), parseLimit(c.Query("limit"), 20))
	if err != nil {
		return writeBillingServiceError(c, err)
	}
	items := make([]adminBillingUserResponse, 0, len(users))
	for _, user := range users {
		wallet := BillingWalletResponse{}
		if account, accountErr := h.repo.Billing().FindAccount(c.Context(), user.ID); accountErr == nil {
			balance, balanceErr := account.DisplayBalance()
			if balanceErr != nil {
				return writeBillingServiceError(c, fmt.Errorf("%w: %v", service.ErrBillingLedgerInvalid, balanceErr))
			}
			wallet = BillingWalletResponse{Paid: account.PaidCredits, Promotional: account.PromotionalCredits, Debt: account.DebtCredits, Balance: balance}
		} else if !errors.Is(accountErr, gorm.ErrRecordNotFound) {
			return writeBillingServiceError(c, accountErr)
		}
		items = append(items, adminBillingUserResponse{ID: user.ID, Email: user.Email, Nickname: user.Nickname, Tier: user.Tier, IsAdmin: user.IsAdmin, Wallet: wallet})
	}
	return Success(c, fiber.Map{"items": items})
}

func (h *BillingAdminHandler) TopUpOperations(c fiber.Ctx) error {
	if _, ok, err := h.requireAdmin(c); !ok {
		return err
	}
	offset, limit, err := billingPagination(c)
	if err != nil {
		return err
	}
	filter := repository.BillingTopUpOperationFilter{UserID: strings.TrimSpace(c.Query("user_id")), Status: strings.TrimSpace(c.Query("status")), ExternalSourceID: strings.TrimSpace(c.Query("external_source_id"))}
	from, to, windowErr := billingReportWindow(c)
	if windowErr != nil {
		return billingErrorResponse(c, fiber.StatusBadRequest, BillingCodeInvalid, "billing_invalid")
	}
	filter.From, filter.To = from, to
	items, err := h.repo.Billing().ListTopUpOperations(c.Context(), filter, offset, limit)
	if err != nil {
		return writeBillingServiceError(c, err)
	}
	total, err := h.repo.Billing().CountTopUpOperations(c.Context(), filter)
	if err != nil {
		return writeBillingServiceError(c, err)
	}
	result := make([]adminTopUpOperationResponse, 0, len(items))
	for _, item := range items {
		result = append(result, topUpOperationResponse(item))
	}
	return Success(c, fiber.Map{"items": result, "total": total, "offset": offset, "limit": limit})
}

func (h *BillingAdminHandler) Entries(c fiber.Ctx) error {
	if _, ok, err := h.requireAdmin(c); !ok {
		return err
	}
	offset, limit, err := billingPagination(c)
	if err != nil {
		return err
	}
	userID, eventKind := strings.TrimSpace(c.Query("user_id")), strings.TrimSpace(c.Query("event_kind"))
	items, err := h.repo.Billing().ListEntries(c.Context(), userID, eventKind, offset, limit)
	if err != nil {
		return writeBillingServiceError(c, err)
	}
	total, err := h.repo.Billing().CountEntries(c.Context(), userID, eventKind)
	if err != nil {
		return writeBillingServiceError(c, err)
	}
	result := make([]adminWalletEntryResponse, 0, len(items))
	for _, item := range items {
		result = append(result, walletEntryResponse(item))
	}
	return Success(c, fiber.Map{"items": result, "total": total, "offset": offset, "limit": limit})
}

func (h *BillingAdminHandler) TopUp(c fiber.Ctx) error {
	adminID, ok, err := h.requireAdmin(c)
	if !ok {
		return err
	}
	var req adminTopUpRequest
	if err := c.Bind().Body(&req); err != nil {
		return billingErrorResponse(c, fiber.StatusBadRequest, BillingCodeInvalid, "billing_invalid")
	}
	req.UserID, req.ExternalSourceID = strings.TrimSpace(req.UserID), strings.TrimSpace(req.ExternalSourceID)
	const sourceType, scope = "manual_api", "admin-topup"
	fingerprintBytes := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s:%s", req.UserID, req.Credits, sourceType, req.ExternalSourceID)))
	fingerprint := hex.EncodeToString(fingerprintBytes[:])
	key := req.ExternalSourceID
	if key == "" {
		// Invalid requests still need an auditable idempotency identity, while
		// keeping repeated invalid submissions independent of one another.
		key = "invalid-" + uuid.NewString()
	} else if len(key) > 128 {
		key = "invalid-" + fingerprint[:32]
	}
	now := time.Now().UTC()
	operation := &model.BillingTopUpOperation{ID: uuid.NewString(), UserID: truncateBillingText(req.UserID, 36), AdminUserID: adminID, Credits: req.Credits, ExternalSourceType: sourceType, ExternalSourceID: truncateBillingText(req.ExternalSourceID, 128), CatalogID: h.catalogID, RequestFingerprint: fingerprint, IdempotencyScope: scope, IdempotencyKey: key, Status: model.BillingTopUpOperationStatusPending, CreatedAt: now, UpdatedAt: now}
	if req.ExternalSourceID != "" {
		if existing, findErr := h.repo.Billing().FindTopUpOperationByKey(c.Context(), scope, key); findErr == nil {
			if existing.RequestFingerprint != fingerprint {
				return h.recordTopUpConflict(c, *existing, adminID, req, fingerprint, sourceType)
			}
			if existing.Status == model.BillingTopUpOperationStatusPending {
				if err := h.completePendingTopUp(c, existing, req, adminID, fingerprint, sourceType, scope, key); err != nil {
					return writeBillingServiceError(c, err)
				}
				return h.recordTopUpReplay(c, *existing, adminID, fingerprint)
			}
			return h.recordTopUpReplay(c, *existing, adminID, fingerprint)
		} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return writeBillingServiceError(c, findErr)
		}
	}
	if req.UserID == "" || req.Credits <= 0 || req.ExternalSourceID == "" || len(req.ExternalSourceID) > 128 {
		operation.Status = model.BillingTopUpOperationStatusFailed
		operation.ErrorCode = "billing_invalid"
		operation.ErrorMessage = invalidTopUpMessage(req.UserID, req.Credits, req.ExternalSourceID)
		if len(req.ExternalSourceID) > 128 {
			operation.ErrorMessage = "external_source_id must be at most 128 bytes"
		}
		if createErr := h.repo.Billing().CreateTopUpOperation(c.Context(), operation); createErr != nil {
			return writeBillingServiceError(c, createErr)
		}
		return billingErrorResponse(c, fiber.StatusBadRequest, BillingCodeInvalid, "billing_invalid")
	}
	if err := h.repo.Billing().CreateTopUpOperation(c.Context(), operation); err != nil {
		if repository.IsDuplicateKeyError(err) {
			if existing, findErr := h.repo.Billing().FindTopUpOperationByKey(c.Context(), scope, key); findErr == nil {
				if existing.RequestFingerprint != fingerprint {
					return h.recordTopUpConflict(c, *existing, adminID, req, fingerprint, sourceType)
				}
				return h.recordTopUpReplay(c, *existing, adminID, fingerprint)
			}
		}
		return writeBillingServiceError(c, err)
	}
	if h.referrals == nil || h.catalogID == "" {
		operation.Status = model.BillingTopUpOperationStatusFailed
		operation.ErrorCode = "billing_internal"
		operation.ErrorMessage = "billing admin is not configured"
		operation.UpdatedAt = time.Now().UTC()
		if updateErr := h.repo.Billing().UpdateTopUpOperation(c.Context(), operation); updateErr != nil {
			return writeBillingServiceError(c, updateErr)
		}
		return Error(c, fiber.StatusInternalServerError, "billing admin is not configured")
	}
	if err := h.completePendingTopUp(c, operation, req, adminID, fingerprint, sourceType, scope, key); err != nil {
		return writeBillingServiceError(c, err)
	}
	return Success(c, fiber.Map{"status": "succeeded", "operation": topUpOperationResponse(*operation), "entry_id": operation.EntryID, "paid_credits_added": operation.PaidCreditsAdded, "debt_repaid_credits": operation.DebtRepaidCredits})
}

func (h *BillingAdminHandler) completePendingTopUp(c fiber.Ctx, operation *model.BillingTopUpOperation, req adminTopUpRequest, adminID, fingerprint, sourceType, scope, key string) error {
	if h.referrals == nil || strings.TrimSpace(operation.CatalogID) == "" {
		operation.Status = model.BillingTopUpOperationStatusFailed
		operation.ErrorCode = "billing_internal"
		operation.ErrorMessage = "billing admin is not configured"
		operation.UpdatedAt = time.Now().UTC()
		if err := h.repo.Billing().UpdateTopUpOperation(c.Context(), operation); err != nil {
			return err
		}
		return service.ErrBillingInvalid
	}
	result, topUpErr := h.referrals.TopUp(c.Context(), service.TopUpRequest{UserID: req.UserID, Credits: req.Credits, ExternalSourceType: sourceType, ExternalSourceID: req.ExternalSourceID, CatalogID: operation.CatalogID, RequestFingerprint: fingerprint, IdempotencyScope: scope, IdempotencyKey: key, ActorType: "admin", ActorID: adminID, SourceService: "studio-admin"})
	if topUpErr != nil {
		operation.Status = model.BillingTopUpOperationStatusFailed
		operation.ErrorCode = billingOperationErrorCode(topUpErr)
		operation.ErrorMessage = truncateBillingError(topUpErr.Error())
		operation.UpdatedAt = time.Now().UTC()
		if updateErr := h.repo.Billing().UpdateTopUpOperation(c.Context(), operation); updateErr != nil {
			return updateErr
		}
		return topUpErr
	}
	operation.Status = model.BillingTopUpOperationStatusSucceeded
	operation.EntryID, operation.PaidCreditsAdded, operation.DebtRepaidCredits = result.TopUp.EntryID, result.TopUp.PaidAdded, result.TopUp.DebtRepaid
	operation.UpdatedAt = time.Now().UTC()
	return h.repo.Billing().UpdateTopUpOperation(c.Context(), operation)
}

func (h *BillingAdminHandler) recordTopUpReplay(c fiber.Ctx, original model.BillingTopUpOperation, adminID, fingerprint string) error {
	replay := original
	replay.ID = uuid.NewString()
	replay.AdminUserID = adminID
	replay.Status = model.BillingTopUpOperationStatusReplayed
	replay.ReplayOfID = original.ID
	replay.RequestFingerprint = fingerprint
	replay.IdempotencyKey = "replay-" + replay.ID
	replay.CreatedAt = time.Now().UTC()
	replay.UpdatedAt = replay.CreatedAt
	if err := h.repo.Billing().CreateTopUpOperation(c.Context(), &replay); err != nil {
		return writeBillingServiceError(c, err)
	}
	response := topUpOperationResponse(replay)
	response.OriginalStatus = original.Status
	return Success(c, fiber.Map{"status": "replayed", "operation": response, "entry_id": original.EntryID, "paid_credits_added": original.PaidCreditsAdded, "debt_repaid_credits": original.DebtRepaidCredits})
}

func (h *BillingAdminHandler) recordTopUpConflict(c fiber.Ctx, original model.BillingTopUpOperation, adminID string, req adminTopUpRequest, fingerprint, sourceType string) error {
	conflict := &model.BillingTopUpOperation{
		ID: uuid.NewString(), UserID: truncateBillingText(req.UserID, 36), AdminUserID: adminID,
		Credits: req.Credits, ExternalSourceType: sourceType, ExternalSourceID: truncateBillingText(req.ExternalSourceID, 128),
		CatalogID: h.catalogID, RequestFingerprint: fingerprint, IdempotencyScope: "admin-topup-conflict",
		IdempotencyKey: "conflict-" + uuid.NewString(), Status: model.BillingTopUpOperationStatusFailed,
		ReplayOfID: original.ID, ErrorCode: "billing_charge_conflict", ErrorMessage: "external_source_id is already associated with a different top-up request",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := h.repo.Billing().CreateTopUpOperation(c.Context(), conflict); err != nil {
		return writeBillingServiceError(c, err)
	}
	return billingErrorResponse(c, fiber.StatusConflict, BillingCodeChargeConflict, "billing_charge_conflict")
}

func invalidTopUpMessage(userID string, credits int64, sourceID string) string {
	switch {
	case strings.TrimSpace(userID) == "":
		return "user_id is required"
	case credits <= 0:
		return "credits must be greater than zero"
	case strings.TrimSpace(sourceID) == "":
		return "external_source_id is required"
	default:
		return "invalid top-up request"
	}
}

func billingPagination(c fiber.Ctx) (int, int, error) {
	offset, offsetErr := strconv.Atoi(c.Query("offset", "0"))
	limit, limitErr := strconv.Atoi(c.Query("limit", "20"))
	if offsetErr != nil || limitErr != nil || offset < 0 || limit < 1 || limit > 100 {
		return 0, 0, billingErrorResponse(c, fiber.StatusBadRequest, BillingCodeInvalid, "billing_invalid")
	}
	return offset, limit, nil
}

func parseLimit(value string, fallback int) int {
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > 100 {
		return fallback
	}
	return limit
}

func topUpOperationResponse(item model.BillingTopUpOperation) adminTopUpOperationResponse {
	return adminTopUpOperationResponse{ID: item.ID, UserID: item.UserID, AdminUserID: item.AdminUserID, Credits: item.Credits, ExternalSourceType: item.ExternalSourceType, ExternalSourceID: item.ExternalSourceID, Status: item.Status, ReplayOfID: item.ReplayOfID, ErrorCode: item.ErrorCode, ErrorMessage: item.ErrorMessage, EntryID: item.EntryID, PaidCreditsAdded: item.PaidCreditsAdded, DebtRepaidCredits: item.DebtRepaidCredits, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}
func walletEntryResponse(item model.BillingWalletEntry) adminWalletEntryResponse {
	return adminWalletEntryResponse{ID: item.ID, UserID: item.UserID, EventKind: item.EventKind, PaidDelta: item.PaidDelta, PromotionalDelta: item.PromotionalDelta, DebtDelta: item.DebtDelta, CatalogID: item.CatalogID, SourceType: item.SourceType, SourceID: item.SourceID, ResourceType: item.ResourceType, ResourceID: item.ResourceID, CreatedAt: item.CreatedAt}
}
func billingOperationErrorCode(err error) string {
	switch {
	case errors.Is(err, service.ErrBillingUserNotFound):
		return "billing_user_not_found"
	case errors.Is(err, service.ErrBillingConflict):
		return "billing_charge_conflict"
	case errors.Is(err, service.ErrBillingCatalogNotFound):
		return "billing_catalog_not_found"
	case errors.Is(err, service.ErrBillingLedgerInvalid):
		return "billing_ledger_invalid"
	case errors.Is(err, service.ErrBillingInvalid):
		return "billing_invalid"
	default:
		return "billing_internal"
	}
}
func truncateBillingError(message string) string {
	message = strings.TrimSpace(message)
	if len(message) > 255 {
		return truncateBillingText(message, 255)
	}
	return message
}

func truncateBillingText(value string, max int) string {
	if len(value) <= max {
		return value
	}
	for max > 0 && max < len(value) && (value[max]&0xc0) == 0x80 {
		max--
	}
	return value[:max]
}

func billingReportWindow(c fiber.Ctx) (time.Time, time.Time, error) {
	var from, to time.Time
	var err error
	if value := strings.TrimSpace(c.Query("from")); value != "" {
		from, err = time.Parse(time.RFC3339, value)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	if value := strings.TrimSpace(c.Query("to")); value != "" {
		to, err = time.Parse(time.RFC3339, value)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	if !from.IsZero() && !to.IsZero() && !from.Before(to) {
		return time.Time{}, time.Time{}, service.ErrBillingInvalid
	}
	return from.UTC(), to.UTC(), nil
}
