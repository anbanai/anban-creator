package router

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/helmet"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/royalrick/anbanwriter/server/auth"
	"github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/handler"
	appmiddleware "github.com/royalrick/anbanwriter/server/middleware"
	"github.com/royalrick/anbanwriter/server/repository"
)

// Services aggregates all service dependencies required by the router.
type Services struct {
	Config      *config.Config
	Logger      *zerolog.Logger
	DB          *gorm.DB
	Redis       *redis.Client
	Repo        repository.Repository
	JWTService  *auth.JWTService
	WechatSvc   *auth.WeChatService
	WSHub       *handler.WebSocketHub
	AuthHandler *handler.AuthHandler
}

// NewRouter creates a new Fiber app with middleware and route groups.
func NewRouter(svc *Services) *fiber.App {
	app := fiber.New(fiber.Config{
		BodyLimit: 50 * 1024 * 1024, // 50 MB
	})

	// ---------------------------------------------------------------------------
	// Global middleware (in order)
	// ---------------------------------------------------------------------------

	// Request ID: inject X-Request-ID header into context.
	app.Use(requestid.New())

	// CORS: defaults allow all origins with standard methods.
	app.Use(cors.New())

	// Security headers.
	app.Use(helmet.New())

	// ---------------------------------------------------------------------------
	// Health check
	// ---------------------------------------------------------------------------

	app.Get("/health", func(c fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		status := fiber.Map{"status": "ok"}

		// Check MySQL.
		if svc.DB != nil {
			sqlDB, err := svc.DB.DB()
			if err == nil {
				if err := sqlDB.PingContext(ctx); err != nil {
					status["status"] = "degraded"
					status["mysql"] = "down"
				} else {
					status["mysql"] = "up"
				}
			} else {
				status["status"] = "degraded"
				status["mysql"] = "down"
			}
		}

		// Check Redis.
		if svc.Redis != nil {
			if err := svc.Redis.Ping(ctx).Err(); err != nil {
				status["status"] = "degraded"
				status["redis"] = "down"
			} else {
				status["redis"] = "up"
			}
		}

		code := fiber.StatusOK
		if status["status"] == "degraded" {
			code = fiber.StatusServiceUnavailable
		}
		return c.Status(code).JSON(status)
	})

	// ---------------------------------------------------------------------------
	// WebSocket
	// ---------------------------------------------------------------------------

	if svc.WSHub != nil {
		app.Get("/ws", svc.WSHub.HandleWebSocket())
	}

	// ---------------------------------------------------------------------------
	// Public API group — /api/v1/auth
	// ---------------------------------------------------------------------------

	authPublic := app.Group("/api/v1/auth")
	if svc.AuthHandler != nil {
		authPublic.Post("/register", svc.AuthHandler.Register)
		authPublic.Post("/login", svc.AuthHandler.Login)
		authPublic.Post("/refresh", svc.AuthHandler.Refresh)
		authPublic.Post("/logout", svc.AuthHandler.Logout)
		authPublic.Post("/wx-login", svc.AuthHandler.WXLogin)
	}

	// ---------------------------------------------------------------------------
	// Protected API group — /api/v1 (requires authentication)
	// ---------------------------------------------------------------------------

	authMiddleware := appmiddleware.AuthMiddleware(svc.JWTService, svc.Repo, svc.Logger)

	apiV1 := app.Group("/api/v1", authMiddleware)

	if svc.AuthHandler != nil {
		apiV1.Get("/auth/me", svc.AuthHandler.Me)
	}

	// Placeholder groups for future modules.
	_ = apiV1.Group("/configs")  // user config CRUD
	_ = apiV1.Group("/plans")    // plan CRUD
	_ = apiV1.Group("/tasks")    // task CRUD
	_ = apiV1.Group("/timeline") // task timeline

	return app
}

