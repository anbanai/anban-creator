package handler

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	serverbilling "github.com/anbanai/anban-creator/server/billing"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	BillingCodeInvalid                   = 40001
	BillingCodeDebtOutstanding           = 40201
	BillingCodeInsufficientForTask       = 40202
	BillingCodeInsufficientForStandalone = 40203
	BillingCodeUnauthorized              = 40101
	BillingCodeResourceNotFound          = 40400
	BillingCodeSKUNotFound               = 40401
	BillingCodeCatalogNotFound           = 40402
	BillingCodeUserNotFound              = 40403
	BillingCodeChargeConflict            = 40901
	BillingCodeQuoteConsumed             = 40902
	BillingCodeQuoteMismatch             = 40903
	BillingCodeReversalNotAllowed        = 40904
	BillingCodeQuoteExpired              = 41001
	BillingCodeInternal                  = 50000
	BillingCodeLedgerInvalid             = 50001
)

type BillingHandlerOptions struct {
	AdminAPIKey   string
	InviteBaseURL string
}

type BillingHandler struct {
	repo          repository.Repository
	catalog       *service.BillingCatalogService
	referrals     *service.BillingReferralService
	bundle        serverbilling.Bundle
	adminAPIKey   string
	inviteBaseURL string
	logger        *zerolog.Logger
}

type BillingWalletResponse struct {
	Paid        int64 `json:"paid"`
	Promotional int64 `json:"promotional"`
	Debt        int64 `json:"debt"`
	Balance     int64 `json:"balance"`
}

type BillingTransactionResponse struct {
	ID               string                       `json:"id"`
	EventKind        model.BillingWalletEventKind `json:"event_kind"`
	PaidDelta        int64                        `json:"paid_delta"`
	PromotionalDelta int64                        `json:"promotional_delta"`
	DebtDelta        int64                        `json:"debt_delta"`
	CatalogID        string                       `json:"catalog_id,omitempty"`
	ChargeID         *string                      `json:"charge_id,omitempty"`
	LotID            *string                      `json:"lot_id,omitempty"`
	ResourceType     string                       `json:"resource_type,omitempty"`
	ResourceID       string                       `json:"resource_id,omitempty"`
	SourceType       *string                      `json:"source_type,omitempty"`
	SourceID         *string                      `json:"source_id,omitempty"`
	CreatedAt        time.Time                    `json:"created_at"`
}

type BillingQuoteResponse struct {
	ID           string         `json:"id"`
	CatalogID    string         `json:"catalog_id"`
	SKUID        string         `json:"sku_id"`
	PriceCredits int64          `json:"price_credits"`
	SKUSnapshot  datatypes.JSON `json:"sku_snapshot"`
	ExpiresAt    time.Time      `json:"expires_at"`
}

type BillingCatalogResponse struct {
	CatalogID string                    `json:"catalog_id"`
	Currency  string                    `json:"currency"`
	SKUs      []serverbilling.SKUConfig `json:"skus"`
}

func NewBillingHandler(repo repository.Repository, catalog *service.BillingCatalogService, referrals *service.BillingReferralService, bundle *serverbilling.Bundle, opts BillingHandlerOptions, logger *zerolog.Logger) *BillingHandler {
	var snapshot serverbilling.Bundle
	if bundle != nil {
		snapshot = *bundle
		snapshot.Promotions.Programs = append([]serverbilling.ReferralProgram(nil), bundle.Promotions.Programs...)
	}
	return &BillingHandler{
		repo: repo, catalog: catalog, referrals: referrals, bundle: snapshot,
		adminAPIKey: strings.TrimSpace(opts.AdminAPIKey), inviteBaseURL: strings.TrimSpace(opts.InviteBaseURL), logger: logger,
	}
}

func (h *BillingHandler) Wallet(c fiber.Ctx) error {
	userID, ok := billingUserID(c)
	if !ok {
		return billingErrorResponse(c, fiber.StatusUnauthorized, BillingCodeUnauthorized, "billing_unauthorized")
	}
	account, err := h.repo.Billing().FindAccount(c.Context(), userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Success(c, BillingWalletResponse{})
	}
	if err != nil {
		h.logError(err, userID, "read billing wallet")
		return writeBillingServiceError(c, err)
	}
	balance, err := account.DisplayBalance()
	if err != nil {
		h.logError(err, userID, "validate billing wallet projection")
		return writeBillingServiceError(c, service.ErrBillingLedgerInvalid)
	}
	return Success(c, BillingWalletResponse{
		Paid: account.PaidCredits, Promotional: account.PromotionalCredits, Debt: account.DebtCredits, Balance: balance,
	})
}

func (h *BillingHandler) Catalog(c fiber.Ctx) error {
	skus := append([]serverbilling.SKUConfig(nil), h.bundle.Products.SKUs...)
	return Success(c, BillingCatalogResponse{
		CatalogID: h.bundle.Products.CatalogID,
		Currency:  h.bundle.Products.Currency,
		SKUs:      skus,
	})
}

func (h *BillingHandler) Transactions(c fiber.Ctx) error {
	userID, ok := billingUserID(c)
	if !ok {
		return billingErrorResponse(c, fiber.StatusUnauthorized, BillingCodeUnauthorized, "billing_unauthorized")
	}
	offset, offsetErr := strconv.Atoi(c.Query("offset", "0"))
	limit, limitErr := strconv.Atoi(c.Query("limit", "20"))
	if offsetErr != nil || limitErr != nil || offset < 0 || limit < 1 || limit > 100 {
		return billingErrorResponse(c, fiber.StatusBadRequest, BillingCodeInvalid, "billing_invalid")
	}
	entries, err := h.repo.Billing().ListEntriesByUser(c.Context(), userID, offset, limit)
	if err != nil {
		h.logError(err, userID, "list billing transactions")
		return writeBillingServiceError(c, err)
	}
	total, err := h.repo.Billing().CountEntriesByUser(c.Context(), userID)
	if err != nil {
		h.logError(err, userID, "count billing transactions")
		return writeBillingServiceError(c, err)
	}
	items := make([]BillingTransactionResponse, 0, len(entries))
	for _, entry := range entries {
		items = append(items, BillingTransactionResponse{
			ID: entry.ID, EventKind: entry.EventKind, PaidDelta: entry.PaidDelta,
			PromotionalDelta: entry.PromotionalDelta, DebtDelta: entry.DebtDelta, CatalogID: entry.CatalogID,
			ChargeID: entry.ChargeID, LotID: entry.LotID, ResourceType: entry.ResourceType, ResourceID: entry.ResourceID,
			SourceType: entry.SourceType, SourceID: entry.SourceID, CreatedAt: entry.CreatedAt,
		})
	}
	return Success(c, fiber.Map{"items": items, "total": total, "offset": offset, "limit": limit})
}

func (h *BillingHandler) CreateQuote(c fiber.Ctx) error {
	userID, ok := billingUserID(c)
	if !ok {
		return billingErrorResponse(c, fiber.StatusUnauthorized, BillingCodeUnauthorized, "billing_unauthorized")
	}
	var req struct {
		Operation          string `json:"operation"`
		Route              string `json:"route"`
		CatalogID          string `json:"catalog_id"`
		RequestFingerprint string `json:"request_fingerprint"`
		IdempotencyScope   string `json:"idempotency_scope"`
		IdempotencyKey     string `json:"idempotency_key"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return billingErrorResponse(c, fiber.StatusBadRequest, BillingCodeInvalid, "billing_invalid")
	}
	quote, err := h.catalog.CreateQuote(c.Context(), service.QuoteRequest{
		UserID: userID, CatalogID: req.CatalogID, Operation: req.Operation, Route: req.Route,
		RequestFingerprint: req.RequestFingerprint, IdempotencyScope: req.IdempotencyScope, IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		return writeBillingServiceError(c, err)
	}
	return Success(c, BillingQuoteResponse{
		ID: quote.ID, CatalogID: quote.CatalogID, SKUID: quote.SKUID, PriceCredits: quote.PriceCredits,
		SKUSnapshot: append(datatypes.JSON(nil), quote.SKUSnapshot...), ExpiresAt: quote.ExpiresAt,
	})
}

func (h *BillingHandler) Referral(c fiber.Ctx) error {
	userID, ok := billingUserID(c)
	if !ok {
		return billingErrorResponse(c, fiber.StatusUnauthorized, BillingCodeUnauthorized, "billing_unauthorized")
	}
	user, err := h.repo.Users().FindByID(c.Context(), userID)
	if err != nil {
		return writeBillingServiceError(c, err)
	}
	response := fiber.Map{
		"invite_code": user.InviteCode,
		"invite_link": h.inviteBaseURL + url.QueryEscape(user.InviteCode),
		"status":      "not_issued",
	}
	program, ok := h.referralProgram()
	if !ok {
		response["program"] = nil
		return Success(c, response)
	}
	response["program"] = fiber.Map{
		"id": program.ID, "catalog_id": h.bundle.Promotions.CatalogID,
		"minimum_topup_credits": minimumReferralCredits(program.MinimumTopUpCNY, h.bundle.Policy.CreditsPerCNY),
		"inviter_credits":       program.InviterCredits, "invitee_credits": program.InviteeCredits,
		"expires_after_seconds": int64(program.ExpiresAfter / time.Second), "max_inviter_rewards": program.MaxInviterRewards,
	}
	issue, err := h.repo.Billing().FindReferralIssue(c.Context(), userID, program.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Success(c, response)
	}
	if err != nil {
		return writeBillingServiceError(c, err)
	}
	response["issue_id"] = issue.ID
	response["status"] = issue.Status
	response["issued_at"] = issue.IssuedAt
	return Success(c, response)
}

func (h *BillingHandler) AdminAuth(c fiber.Ctx) error {
	adminKey := strings.TrimSpace(c.Get("X-Admin-API-Key"))
	authorization := strings.TrimSpace(c.Get("Authorization"))
	provided := ""
	if adminKey != "" && authorization == "" {
		provided = adminKey
	} else if adminKey == "" && len(authorization) > 7 && strings.EqualFold(authorization[:7], "Bearer ") {
		candidate := authorization[7:]
		if strings.TrimSpace(candidate) == candidate && !strings.ContainsAny(candidate, " \t\r\n") {
			provided = candidate
		}
	}
	expectedDigest := sha256.Sum256([]byte(h.adminAPIKey))
	providedDigest := sha256.Sum256([]byte(provided))
	if h.adminAPIKey == "" || provided == "" || subtle.ConstantTimeCompare(providedDigest[:], expectedDigest[:]) != 1 {
		return billingErrorResponse(c, fiber.StatusUnauthorized, BillingCodeUnauthorized, "billing_unauthorized")
	}
	return c.Next()
}

func (h *BillingHandler) AdminTopUp(c fiber.Ctx) error {
	var req struct {
		UserID             string `json:"user_id"`
		Credits            int64  `json:"credits"`
		ExternalSourceType string `json:"external_source_type"`
		ExternalSourceID   string `json:"external_source_id"`
		CatalogID          string `json:"catalog_id"`
		RequestFingerprint string `json:"request_fingerprint"`
		IdempotencyScope   string `json:"idempotency_scope"`
		IdempotencyKey     string `json:"idempotency_key"`
		RequestID          string `json:"request_id"`
		CorrelationID      string `json:"correlation_id"`
	}
	if err := c.Bind().Body(&req); err != nil || strings.TrimSpace(req.UserID) == "" || req.Credits <= 0 ||
		strings.TrimSpace(req.ExternalSourceType) == "" || strings.TrimSpace(req.ExternalSourceID) == "" ||
		strings.TrimSpace(req.CatalogID) == "" || strings.TrimSpace(req.RequestFingerprint) == "" ||
		strings.TrimSpace(req.IdempotencyScope) == "" || strings.TrimSpace(req.IdempotencyKey) == "" {
		return billingErrorResponse(c, fiber.StatusBadRequest, BillingCodeInvalid, "billing_invalid")
	}
	topUpRequest, err := service.CanonicalTopUpRequest(service.TopUpRequest{
		UserID: req.UserID, Credits: req.Credits, ExternalSourceType: req.ExternalSourceType, ExternalSourceID: req.ExternalSourceID,
		CatalogID: req.CatalogID, RequestFingerprint: req.RequestFingerprint,
		IdempotencyScope: req.IdempotencyScope, IdempotencyKey: req.IdempotencyKey,
		ActorType: "admin", ActorID: "billing_api", SourceService: "billing-api",
		RequestID: req.RequestID, CorrelationID: req.CorrelationID,
	})
	if err != nil {
		return writeBillingServiceError(c, err)
	}
	result, err := h.referrals.TopUp(c.Context(), topUpRequest)
	if err != nil {
		return writeBillingServiceError(c, err)
	}
	response := fiber.Map{
		"entry_id": result.TopUp.EntryID, "lot_id": result.TopUp.LotID,
		"debt_repaid_credits": result.TopUp.DebtRepaid, "paid_credits_added": result.TopUp.PaidAdded,
	}
	if result.Referral != nil {
		response["referral_issue_id"] = result.Referral.ID
		response["referral_status"] = result.Referral.Status
	}
	return Success(c, response)
}

func (h *BillingHandler) referralProgram() (serverbilling.ReferralProgram, bool) {
	for _, program := range h.bundle.Promotions.Programs {
		if program.Trigger == "invitee_first_paid_topup" {
			return program, true
		}
	}
	return serverbilling.ReferralProgram{}, false
}

func (h *BillingHandler) logError(err error, userID, action string) {
	if h.logger != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg(action)
	}
}

func billingUserID(c fiber.Ctx) (string, bool) {
	userID := strings.TrimSpace(GetUserID(c))
	return userID, userID != ""
}

func writeBillingServiceError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, service.ErrBillingInvalid):
		return billingErrorResponse(c, fiber.StatusBadRequest, BillingCodeInvalid, "billing_invalid")
	case errors.Is(err, service.ErrBillingDebtOutstanding):
		return billingErrorResponse(c, fiber.StatusPaymentRequired, BillingCodeDebtOutstanding, "billing_debt_outstanding")
	case errors.Is(err, service.ErrBillingInsufficientForTask):
		return billingErrorResponse(c, fiber.StatusPaymentRequired, BillingCodeInsufficientForTask, "billing_insufficient_for_task")
	case errors.Is(err, service.ErrBillingInsufficientForStandaloneOperation):
		return billingErrorResponse(c, fiber.StatusPaymentRequired, BillingCodeInsufficientForStandalone, "billing_insufficient_for_standalone_operation")
	case errors.Is(err, service.ErrBillingSKUNotFound):
		return billingErrorResponse(c, fiber.StatusNotFound, BillingCodeSKUNotFound, "billing_sku_not_found")
	case errors.Is(err, service.ErrBillingCatalogNotFound):
		return billingErrorResponse(c, fiber.StatusNotFound, BillingCodeCatalogNotFound, "billing_catalog_not_found")
	case errors.Is(err, service.ErrBillingUserNotFound):
		return billingErrorResponse(c, fiber.StatusNotFound, BillingCodeUserNotFound, "billing_user_not_found")
	case errors.Is(err, service.ErrBillingConflict):
		return billingErrorResponse(c, fiber.StatusConflict, BillingCodeChargeConflict, "billing_charge_conflict")
	case errors.Is(err, service.ErrBillingQuoteConsumed):
		return billingErrorResponse(c, fiber.StatusConflict, BillingCodeQuoteConsumed, "billing_quote_consumed")
	case errors.Is(err, service.ErrBillingQuoteMismatch):
		return billingErrorResponse(c, fiber.StatusConflict, BillingCodeQuoteMismatch, "billing_quote_mismatch")
	case errors.Is(err, service.ErrBillingReversalNotAllowed):
		return billingErrorResponse(c, fiber.StatusConflict, BillingCodeReversalNotAllowed, "billing_reversal_not_allowed")
	case errors.Is(err, service.ErrBillingQuoteExpired):
		return billingErrorResponse(c, fiber.StatusGone, BillingCodeQuoteExpired, "billing_quote_expired")
	case errors.Is(err, gorm.ErrRecordNotFound):
		return billingErrorResponse(c, fiber.StatusNotFound, BillingCodeResourceNotFound, "billing_resource_not_found")
	case errors.Is(err, service.ErrBillingLedgerInvalid):
		return billingErrorResponse(c, fiber.StatusInternalServerError, BillingCodeLedgerInvalid, "billing_ledger_invalid")
	default:
		return billingErrorResponse(c, fiber.StatusInternalServerError, BillingCodeInternal, "billing_internal")
	}
}

func billingErrorResponse(c fiber.Ctx, status, code int, message string) error {
	return c.Status(status).JSON(Response{Code: code, Msg: message})
}

func minimumReferralCredits(minimum serverbilling.MicroCNY, creditsPerCNY int64) int64 {
	if minimum <= 0 || creditsPerCNY <= 0 {
		return 0
	}
	const unit = int64(1_000_000)
	whole, remainder := int64(minimum)/unit, int64(minimum)%unit
	if whole > (1<<63-1)/creditsPerCNY || remainder > (1<<63-1)/creditsPerCNY {
		return 0
	}
	credits := whole * creditsPerCNY
	product := remainder * creditsPerCNY
	fraction := product / unit
	if product%unit != 0 {
		fraction++
	}
	if credits > (1<<63-1)-fraction {
		return 0
	}
	return credits + fraction
}
