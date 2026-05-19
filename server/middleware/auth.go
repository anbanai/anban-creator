package middleware

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/royalrick/anbanwriter/server/auth"
	"github.com/royalrick/anbanwriter/server/handler"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// AuthMiddleware validates the JWT token in the Authorization header and stores
// the user ID, user object, and role in Fiber locals.
func AuthMiddleware(jwtSvc *auth.JWTService, repo repository.Repository, logger *zerolog.Logger) fiber.Handler {
	return func(c fiber.Ctx) error {
		// Skip JWT validation for agent communication endpoints (use API key auth instead).
		if strings.HasPrefix(c.Path(), "/api/v1/agent/") {
			return c.Next()
		}

		// Skip admin endpoints — they use their own API key authentication.
		if strings.HasPrefix(c.Path(), "/api/v1/admin/") {
			return c.Next()
		}

		// Skip public auth endpoints — they handle their own authentication.
		// This is necessary because Fiber v3 registers group middleware as USE routes
		// that match all paths under the prefix, including routes from other groups.
		switch c.Path() {
		case "/api/v1/auth/register", "/api/v1/auth/login",
			"/api/v1/auth/refresh", "/api/v1/auth/logout", "/api/v1/auth/wx-login",
			"/api/v1/auth/send-code":
			return c.Next()
		}

		if repo == nil {
			logger.Error().Str("path", c.Path()).Msg("auth middleware called but repository is nil")
			return handler.Error(c, fiber.StatusServiceUnavailable, "service is not available")
		}

		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return handler.Error(c, fiber.StatusUnauthorized, "missing Authorization header")
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			return handler.Error(c, fiber.StatusUnauthorized, "invalid Authorization format")
		}

		token := parts[1]

		claims, err := jwtSvc.ValidateToken(token)
		if err != nil {
			logger.Warn().Err(err).Msg("token validation failed")
			return handler.Error(c, fiber.StatusUnauthorized, "invalid or expired token")
		}

		if claims.UserID == "" {
			logger.Error().Str("path", c.Path()).Msg("valid token but empty user_id")
			return handler.Error(c, fiber.StatusUnauthorized, "invalid token")
		}

		// Look up user from DB.
		user, err := repo.Users().FindByID(c.Context(), claims.UserID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return handler.Error(c, fiber.StatusUnauthorized, "user not found")
			}
			logger.Error().Err(err).Str("user_id", claims.UserID).Msg("failed to query user")
			return handler.Error(c, fiber.StatusInternalServerError, "internal error")
		}

		// Store in locals.
		c.Locals("user_id", claims.UserID)
		c.Locals("user", user)
		c.Locals("role", claims.Role)

		return c.Next()
	}
}

// OptionalAuthMiddleware validates the JWT token if present but does not reject
// unauthenticated requests. If a valid token is found the user info is stored
// in Fiber locals.
func OptionalAuthMiddleware(jwtSvc *auth.JWTService, repo repository.Repository, logger *zerolog.Logger) fiber.Handler {
	return func(c fiber.Ctx) error {
		if repo == nil {
			return c.Next()
		}

		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return c.Next()
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			return c.Next()
		}

		claims, err := jwtSvc.ValidateToken(parts[1])
		if err != nil {
			return c.Next()
		}

		if claims.UserID == "" {
			return c.Next()
		}

		user, err := repo.Users().FindByID(c.Context(), claims.UserID)
		if err == nil {
			c.Locals("user_id", claims.UserID)
			c.Locals("user", user)
			c.Locals("role", claims.Role)
		}

		return c.Next()
	}
}

// GetUserID extracts the authenticated user ID from Fiber locals.
// This is a convenience re-export of handler.GetUserID.
func GetUserID(c fiber.Ctx) string {
	return handler.GetUserID(c)
}
