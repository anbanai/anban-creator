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

// MCPAuthMiddleware authenticates requests to the MCP endpoint using either:
// 1. X-API-Key header matched against a server-level MCP API key
// 2. Authorization: Bearer <jwt_token> (standard user auth)
//
// When the API key is used, user_id is set to "mcp-api-key" and the request
// is allowed through without a database user lookup. This is intended for
// Claude Code integration where a persistent API key is more convenient.
//
// When a JWT token is provided, full user authentication is performed as in
// the standard AuthMiddleware.
func MCPAuthMiddleware(jwtSvc *auth.JWTService, repo repository.Repository, mcpAPIKey string, logger *zerolog.Logger) fiber.Handler {
	return func(c fiber.Ctx) error {
		// 1. Check X-API-Key header first.
		apiKey := c.Get("X-API-Key")
		if apiKey != "" && mcpAPIKey != "" {
			if apiKey == mcpAPIKey {
				c.Locals("user_id", "mcp-api-key")
				c.Locals("auth_method", "api_key")
				return c.Next()
			}
			logger.Warn().Str("ip", c.IP()).Msg("invalid MCP API key provided")
			return handler.Error(c, fiber.StatusUnauthorized, "invalid API key")
		}

		// 2. Fall back to JWT Bearer token authentication.
		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return handler.Error(c, fiber.StatusUnauthorized, "missing Authorization header or X-API-Key")
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			return handler.Error(c, fiber.StatusUnauthorized, "invalid Authorization format")
		}

		claims, err := jwtSvc.ValidateToken(parts[1])
		if err != nil {
			logger.Warn().Err(err).Msg("MCP endpoint token validation failed")
			return handler.Error(c, fiber.StatusUnauthorized, "invalid or expired token")
		}

		if claims.UserID == "" {
			return handler.Error(c, fiber.StatusUnauthorized, "invalid token")
		}

		user, err := repo.Users().FindByID(c.Context(), claims.UserID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return handler.Error(c, fiber.StatusUnauthorized, "user not found")
			}
			logger.Error().Err(err).Str("user_id", claims.UserID).Msg("failed to query user for MCP auth")
			return handler.Error(c, fiber.StatusInternalServerError, "internal error")
		}

		c.Locals("user_id", claims.UserID)
		c.Locals("user", user)
		c.Locals("role", claims.Role)
		c.Locals("auth_method", "jwt")

		return c.Next()
	}
}
