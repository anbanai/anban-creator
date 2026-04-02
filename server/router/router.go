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

	"github.com/royalrick/anbanwriter/server/agent"
	"github.com/royalrick/anbanwriter/server/auth"
	"github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/handler"
	appmiddleware "github.com/royalrick/anbanwriter/server/middleware"
	"github.com/royalrick/anbanwriter/server/mcp"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/service"
)

// Services aggregates all service dependencies required by the router.
type Services struct {
	Config       *config.Config
	Logger       *zerolog.Logger
	DB           *gorm.DB
	Redis        *redis.Client
	Repo         repository.Repository
	JWTService   *auth.JWTService
	WechatSvc    *auth.WeChatService
	WSHub        *handler.WebSocketHub
	AuthHandler  *handler.AuthHandler
	Executor     *agent.Executor
	PlanService  *service.PlanService
	TaskService  *service.TaskService
	PlanHandler  *handler.PlanHandler
	TaskHandler  *handler.TaskHandler
	ConfigHandler *handler.ConfigHandler
	TimelineHandler *handler.TimelineHandler
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

	// Request logging: log method, path, status, duration for all requests.
	if svc.Logger != nil {
		app.Use(appmiddleware.RequestLogger(svc.Logger))
	}

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
	rateLimiter := appmiddleware.RateLimit(svc.Redis, 100, 1*time.Minute)

	apiV1 := app.Group("/api/v1", authMiddleware, rateLimiter)

	if svc.AuthHandler != nil {
		apiV1.Get("/auth/me", svc.AuthHandler.Me)
	}

	// ---------------------------------------------------------------------------
	// User Config endpoints
	// ---------------------------------------------------------------------------

	if svc.ConfigHandler != nil {
		apiV1.Get("/configs", svc.ConfigHandler.List)
		apiV1.Get("/configs/:scope", svc.ConfigHandler.GetByScope)
		apiV1.Put("/configs/:scope", svc.ConfigHandler.Upsert)
	}

	// ---------------------------------------------------------------------------
	// Plan endpoints
	// ---------------------------------------------------------------------------

	if svc.PlanHandler != nil {
		apiV1.Post("/plans", svc.PlanHandler.Create)
		apiV1.Get("/plans", svc.PlanHandler.List)
		apiV1.Get("/plans/:id", svc.PlanHandler.GetByID)
		apiV1.Put("/plans/:id", svc.PlanHandler.Update)
		apiV1.Delete("/plans/:id", svc.PlanHandler.Delete)
		apiV1.Post("/plans/:id/pause", svc.PlanHandler.Pause)
		apiV1.Post("/plans/:id/resume", svc.PlanHandler.Resume)
	}

	// ---------------------------------------------------------------------------
	// Task endpoints
	// ---------------------------------------------------------------------------

	if svc.TaskHandler != nil {
		apiV1.Post("/tasks", svc.TaskHandler.Create)
		apiV1.Get("/tasks", svc.TaskHandler.List)
		apiV1.Get("/tasks/:id", svc.TaskHandler.GetByID)
		apiV1.Post("/tasks/:id/cancel", svc.TaskHandler.Cancel)
		apiV1.Get("/tasks/:id/files", svc.TaskHandler.GetFiles)
		apiV1.Get("/tasks/:id/stream", svc.TaskHandler.Stream)
	}

	// ---------------------------------------------------------------------------
	// Timeline endpoint
	// ---------------------------------------------------------------------------

	if svc.TimelineHandler != nil {
		apiV1.Get("/timeline", svc.TimelineHandler.GetTimeline)
	}

	// MCP tools endpoint with API key or JWT authentication.
	if svc.Repo != nil && svc.Logger != nil {
		mcpAuth := appmiddleware.MCPAuthMiddleware(
			svc.JWTService, svc.Repo, svc.Config.MCP.APIKey, svc.Logger,
		)
		mcpGroup := app.Group("/api/v1/mcp", mcpAuth)
		mcpGroup.Get("/tools", mcp.NewMCPHttpHandler(svc.Repo, svc.Logger))
	}

	return app
}
