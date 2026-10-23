package middleware

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/redis/go-redis/v9"
)

// RateLimit creates a fixed-window rate limiting middleware using Redis.
// Limits to maxRequests per window per bucket, IP address, and request path.
// If redisClient is nil, rate limiting is skipped (pass-through).
func RateLimit(redisClient *redis.Client, bucket string, maxRequests int, window time.Duration) fiber.Handler {
	return func(c fiber.Ctx) error {
		if redisClient == nil {
			return c.Next()
		}

		ip := c.IP()
		if ip == "" {
			ip = "unknown"
		}

		key := fmt.Sprintf("ratelimit:%s:%s:%s", bucket, ip, c.Path())
		ctx := c.Context()

		// INCR + EXPIRE on the first hit implements a fixed window.
		count, err := redisClient.Incr(ctx, key).Result()
		if err != nil {
			// On Redis error, allow the request through (graceful degradation).
			return c.Next()
		}

		// Set expiry on first request in the window.
		if count == 1 {
			if err := redisClient.Expire(ctx, key, window).Err(); err != nil {
				// Non-fatal: the key will expire naturally if Redis restarts.
			}
		}

		// Set standard rate limit headers.
		c.Set("X-RateLimit-Limit", fmt.Sprintf("%d", maxRequests))
		c.Set("X-RateLimit-Remaining", fmt.Sprintf("%d", max(0, int64(maxRequests)-count)))
		c.Set("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(window).Unix()))

		if count > int64(maxRequests) {
			c.Set("Retry-After", fmt.Sprintf("%d", int(window.Seconds())))
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"code": fiber.StatusTooManyRequests * 100,
				"msg":  "too many requests, please try again later",
			})
		}

		return c.Next()
	}
}

// max is a helper since Go's built-in max requires Go 1.21+.
func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
