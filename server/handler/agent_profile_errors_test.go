package handler

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
)

func TestAgentProfileErrorResponseContract(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   int
		msg    string
	}{
		{name: "not found", err: service.ErrAgentProfileNotFound, status: fiber.StatusBadRequest, code: 46001, msg: "agent_profile_not_found"},
		{name: "unavailable", err: service.ErrAgentProfileUnavailable, status: fiber.StatusUnprocessableEntity, code: 46002, msg: "agent_profile_unavailable"},
		{name: "access denied", err: service.ErrAgentProfileAccessDenied, status: fiber.StatusForbidden, code: 46003, msg: "agent_profile_access_denied"},
		{name: "snapshot invalid", err: service.ErrAgentProfileSnapshotInvalid, status: fiber.StatusBadRequest, code: 46004, msg: "agent_profile_snapshot_invalid"},
		{name: "snapshot conflict", err: service.ErrAgentProfileSnapshotConflict, status: fiber.StatusConflict, code: 46005, msg: "agent_profile_snapshot_conflict"},
		{name: "provider unavailable", err: service.ErrAgentProviderUnavailable, status: fiber.StatusUnprocessableEntity, code: 46006, msg: "agent_provider_unavailable"},
		{name: "cost unmapped", err: service.ErrAgentModelCostUnmapped, status: fiber.StatusUnprocessableEntity, code: 46007, msg: "agent_model_cost_unmapped"},
		{name: "profile SKU missing", err: service.ErrBillingProfileSKUNotFound, status: fiber.StatusUnprocessableEntity, code: 46008, msg: "billing_profile_sku_not_found"},
	}
	seenCodes := make(map[int]string, len(tests))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := fiber.New()
			app.Get("/", func(c fiber.Ctx) error {
				handled, response := respondAgentProfileError(c, errors.Join(errors.New("context"), tt.err))
				if !handled {
					t.Fatalf("error was not handled")
				}
				return response
			})
			response, err := app.Test(httptest.NewRequest("GET", "/", nil))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			var body Response
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != tt.status || body.Code != tt.code || body.Msg != tt.msg || body.Data == nil {
				t.Fatalf("status=%d body=%#v", response.StatusCode, body)
			}
			if previous, exists := seenCodes[body.Code]; exists {
				t.Fatalf("numeric code %d is shared by %q and %q", body.Code, previous, tt.name)
			}
			seenCodes[body.Code] = tt.name
		})
	}
}
