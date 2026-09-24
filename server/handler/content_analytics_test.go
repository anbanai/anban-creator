package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
)

type contentAnalyticsStub struct{ called bool }

func (s *contentAnalyticsStub) Candidates(_ context.Context, user, project, search string, offset, limit int) ([]service.AnalyticsCandidate, int, error) {
	s.called = true
	return []service.AnalyticsCandidate{{Target: service.AnalyticsTarget{Kind: "task", ID: "task"}, Title: search, ContentType: "article", Status: "pending"}}, 1, nil
}
func TestContentAnalyticsCandidatesHTTP(t *testing.T) {
	stub := &contentAnalyticsStub{}
	app := fiber.New()
	h := NewContentAnalyticsHandler(stub)
	app.Get("/projects/:id/content-analytics/candidates", func(c fiber.Ctx) error { c.Locals("user_id", "user"); return h.Candidates(c) })
	response, err := app.Test(httptest.NewRequest("GET", "/projects/00000000-0000-0000-0000-000000000001/content-analytics/candidates?search=title&offset=0&limit=25", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result struct {
		Data struct {
			Items []service.AnalyticsCandidate `json:"items"`
			Total int                          `json:"total"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || !stub.called || result.Data.Total != 1 || len(result.Data.Items) != 1 || result.Data.Items[0].Target.Kind != "task" {
		t.Fatalf("status=%d called=%v result=%+v", response.StatusCode, stub.called, result)
	}
}
