package middleware

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
)

func TestRequestLoggerRedactsShareTokensOnly(t *testing.T) {
	for _, route := range []struct{ path, want string }{
		{"/api/v1/public/shares/secret-token", "/api/v1/public/shares/[redacted]"},
		{"/api/v1/public/shares/secret-token/images/image-id", "/api/v1/public/shares/[redacted]/images/image-id"},
		{"/API/v1/Public/Shares/secret-token", "/API/v1/Public/Shares/[redacted]"},
		{"/share/secret-token", "/share/[redacted]"},
		{"/api/v1/tasks/task-id/share", "/api/v1/tasks/task-id/share"},
	} {
		var logs bytes.Buffer
		logger := zerolog.New(&logs)
		app := fiber.New()
		app.Use(RequestLogger(&logger))
		app.Get("/*", func(c fiber.Ctx) error { return c.SendStatus(200) })
		resp, err := app.Test(httptest.NewRequest("GET", route.path, nil))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if strings.Contains(logs.String(), "secret-token") || !strings.Contains(logs.String(), route.want) {
			t.Fatalf("%s: %s", route.path, logs.String())
		}
	}
}
