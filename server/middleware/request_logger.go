package middleware

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
)

// RequestLogger returns a middleware that logs all API requests with
// method, path, status code, duration, and request ID.
func RequestLogger(logger *zerolog.Logger) fiber.Handler {
	return func(c fiber.Ctx) error {
		start := time.Now()

		// Process the request.
		err := c.Next()

		// Calculate duration.
		duration := time.Since(start)

		// Extract request ID if available (set by requestid middleware).
		requestID := c.Get("X-Request-ID")

		// Build log event.
		event := logger.Info()
		if c.Response().StatusCode() >= 500 {
			event = logger.Error()
		} else if c.Response().StatusCode() >= 400 {
			event = logger.Warn()
		}

		event.
			Str("method", c.Method()).
			Str("path", c.Path()).
			Int("status", c.Response().StatusCode()).
			Dur("duration", duration).
			Str("ip", c.IP())

		if requestID != "" {
			event.Str("request_id", requestID)
		}

		// Log errors from the handler chain.
		if err != nil {
			event.Err(err)
		}

		event.Msg("request")

		return err
	}
}
