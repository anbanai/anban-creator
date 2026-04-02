package router

import (
	"github.com/gofiber/fiber/v3"
	"github.com/royalrick/anbanwriter/server/config"
	"github.com/rs/zerolog"
)

// NewRouter creates a new Fiber app with a health check endpoint.
func NewRouter(_ *config.Config, _ *zerolog.Logger) *fiber.App {
	app := fiber.New()

	app.Get("/health", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	return app
}
