package handler

import (
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
)

type BillingAdminHandler struct {
	margin *service.MarginService
}

func NewBillingAdminHandler(margin *service.MarginService) *BillingAdminHandler {
	return &BillingAdminHandler{margin: margin}
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
