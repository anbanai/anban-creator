package handler

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
)

func setupTopicRadarHandler(t *testing.T) (*fiber.App, repository.Repository) {
	t.Helper()
	repo := repository.New(setupTaskHandlerTestDB(t))
	ctx := context.Background()
	if err := repo.Projects().Create(ctx, &model.Project{ID: "project", UserID: "owner", Name: "Radar", Platform: "seednote", Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	svc := service.NewTopicRadarService(repo, nil, nil)
	sub, err := svc.CreateSubscription(ctx, service.CreateTopicSubscriptionParams{UserID: "owner", ProjectID: "project", Name: "Daily", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	sub.ID = "subscription"
	if err := repo.TopicSubscriptions().Create(ctx, sub); err != nil {
		t.Fatal(err)
	}
	if err := repo.TopicDigests().Create(ctx, &model.TopicDigest{ID: "digest", SubscriptionID: sub.ID, UserID: "owner", ProjectID: "project", Status: model.TopicDigestStatusSucceeded, OccurrenceKey: "handler", InputSnapshot: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TopicNotificationChannels().Create(ctx, &model.TopicNotificationChannel{ID: "channel", ProjectID: "project", UserID: "owner", Provider: "feishu", Name: "Team", WebhookURL: "https://1.1.1.1/hook?token=secret-fixture", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	log := zerolog.New(io.Discard)
	h := NewTopicRadarHandler(svc, repo, &log)
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error { c.Locals("user_id", c.Get("X-User-ID")); return c.Next() })
	app.Get("/projects/:project_id/subscriptions", h.ListSubscriptions)
	app.Patch("/subscriptions/:subscription_id", h.UpdateSubscription)
	app.Delete("/subscriptions/:subscription_id", h.DeleteSubscription)
	app.Get("/digests/:digest_id", h.GetDigest)
	app.Get("/projects/:project_id/channels", h.ListNotificationChannels)
	app.Patch("/channels/:channel_id", h.UpdateNotificationChannel)
	app.Delete("/channels/:channel_id", h.DeleteNotificationChannel)
	return app, repo
}

func TestTopicRadarHandlerDeletesSubscriptionWithAuthorizationAndIdempotency(t *testing.T) {
	app, repo := setupTopicRadarHandler(t)
	for _, tc := range []struct {
		user   string
		status int
	}{
		{user: "stranger", status: fiber.StatusForbidden},
		{user: "owner", status: fiber.StatusOK},
		{user: "owner", status: fiber.StatusOK},
	} {
		req := httptest.NewRequest("DELETE", "/subscriptions/subscription", nil)
		req.Header.Set("X-User-ID", tc.user)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != tc.status {
			t.Fatalf("delete as %s: %d %s, want %d", tc.user, resp.StatusCode, body, tc.status)
		}
	}
	stored, err := repo.TopicSubscriptions().FindByID(context.Background(), "subscription")
	if err != nil {
		t.Fatal(err)
	}
	if stored.DeletedAt == nil || stored.Enabled || stored.NextRunAt != nil {
		t.Fatalf("deleted subscription state = %+v", stored)
	}
}

func TestTopicRadarHandlerAuthorizationAndReadModels(t *testing.T) {
	app, _ := setupTopicRadarHandler(t)
	for _, tc := range []struct {
		path, user string
		status     int
	}{
		{"/digests/digest", "", 401}, {"/digests/digest", "stranger", 403}, {"/digests/missing", "owner", 404},
		{"/digests/digest", "owner", 200}, {"/projects/project/channels", "stranger", 403},
		{"/projects/project/subscriptions", "stranger", 403}, {"/projects/project/channels", "owner", 200},
	} {
		req := httptest.NewRequest("GET", tc.path, nil)
		req.Header.Set("X-User-ID", tc.user)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != tc.status {
			t.Fatalf("%s %s: %d %s", tc.path, tc.user, resp.StatusCode, body)
		}
		if strings.Contains(string(body), "secret-fixture") || strings.Contains(string(body), "webhook_url") {
			t.Fatalf("secret exposed: %s", body)
		}
		if tc.path == "/digests/digest" && tc.status == 200 && (!strings.Contains(string(body), `"adoptions":[]`) || !strings.Contains(string(body), `"notifications":[]`)) {
			t.Fatalf("missing durable status lists: %s", body)
		}
	}
}

func TestTopicRadarHandlerPauseAndSecretPreservingChannelUpdate(t *testing.T) {
	app, repo := setupTopicRadarHandler(t)
	for _, tc := range []struct{ path, body string }{
		{"/subscriptions/subscription", `{"enabled":false}`},
		{"/channels/channel", `{"name":"Renamed","enabled":false}`},
	} {
		req := httptest.NewRequest("PATCH", tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-User-ID", "owner")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("%s: %d %s", tc.path, resp.StatusCode, body)
		}
	}
	sub, _ := repo.TopicSubscriptions().FindByID(context.Background(), "subscription")
	if sub.Enabled || sub.NextRunAt != nil {
		t.Fatalf("pause did not clear due schedule: %+v", sub)
	}
	channel, _ := repo.TopicNotificationChannels().FindByID(context.Background(), "channel")
	if channel.Name != "Renamed" || channel.Enabled || channel.WebhookURL != "https://1.1.1.1/hook?token=secret-fixture" {
		t.Fatalf("channel patch lost state")
	}
}

type failedTopicRadarQueue struct{}

func (failedTopicRadarQueue) EnqueueUnique(string, []byte, string) (bool, error) {
	return false, errors.New("private queue transport detail")
}

func TestTopicRadarHandlerReturnsDurableWorkAfterQueueFailure(t *testing.T) {
	app, repo := setupTopicRadarHandler(t)
	svc := service.NewTopicRadarService(repo, failedTopicRadarQueue{}, nil)
	log := zerolog.New(io.Discard)
	h := NewTopicRadarHandler(svc, repo, &log)
	app.Post("/subscriptions/:subscription_id/run", h.RunDigest)
	app.Post("/content-assets/:id/scripts", h.RequestScript)
	ctx := context.Background()
	asset := model.NewContentAsset("owner", "Scoped topic")
	asset.ID = "asset"
	if err := repo.ContentAssets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}
	if err := repo.TopicSuggestions().CreateBatch(ctx, []*model.TopicSuggestion{{ID: "suggestion", DigestID: "digest", ProjectID: "project", Title: "Scoped topic", SourceRefs: []byte(`[]`), Scores: []byte(`{}`), VideoFit: "strong"}}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ path, body string }{
		{"/subscriptions/subscription/run", `{"request_key":"durable"}`},
		{"/content-assets/asset/scripts", `{"project_id":"project","target_platform":"douyin"}`},
	} {
		req := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-User-ID", "owner")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || !strings.Contains(string(body), `"status":"queued"`) || strings.Contains(string(body), "private queue") {
			t.Fatalf("%s: %d %s", tc.path, resp.StatusCode, body)
		}
	}
	digests, err := svc.ListDigests(ctx, "owner", "subscription")
	if err != nil {
		t.Fatal(err)
	}
	for _, digest := range digests {
		if strings.Contains(digest.Error, "private queue") {
			t.Fatal("private queue error persisted to public read model")
		}
	}
}
