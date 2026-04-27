package router

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
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
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/service"
	"github.com/royalrick/anbanwriter/server/storage"
)

// Services aggregates all service dependencies required by the router.
type Services struct {
	Config          *config.Config
	Logger          *zerolog.Logger
	DB              *gorm.DB
	Redis           *redis.Client
	Repo            repository.Repository
	JWTService      *auth.JWTService
	WechatSvc       *auth.WeChatService
	WSHub           *handler.WebSocketHub
	AuthHandler     *handler.AuthHandler
	Executor        agent.TaskExecutor
	PlanService     *service.PlanService
	TaskService     *service.TaskService
	CreditService   *service.CreditService
	PlanHandler     *handler.PlanHandler
	TaskHandler     *handler.TaskHandler
	AgentHandler    *handler.AgentHandler
	CreditHandler   *handler.CreditHandler
	ChannelHandler  *handler.ChannelHandler
	TimelineHandler *handler.TimelineHandler
	APIKeyHandler   *handler.APIKeyHandler
	FileHandler      *handler.FileHandler
	FeedbackHandler  *handler.FeedbackHandler
	ModelConfigHandler *handler.ModelConfigHandler
	MCPHandler       http.Handler
	StorageProvider storage.Provider
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

	// CORS: configurable allowed origins. Defaults to localhost-only when empty.
	var corsConfig cors.Config
	if len(svc.Config.CORS.AllowedOrigins) > 0 {
		corsConfig = cors.Config{
			AllowOrigins: svc.Config.CORS.AllowedOrigins,
		}
	} else {
		// Development default: dynamically allow any localhost origin.
		corsConfig = cors.Config{
			AllowOriginsFunc: func(origin string) bool {
				return strings.HasPrefix(origin, "http://localhost") ||
					strings.HasPrefix(origin, "http://127.0.0.1")
			},
		}
	}
	app.Use(cors.New(corsConfig))

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

	// Stricter rate limiter for auth endpoints (10 requests per minute per IP).
	authRateLimit := appmiddleware.RateLimit(svc.Redis, 10, 1*time.Minute)
	// Rate limit for send-code: 10 requests per 10 minutes per IP.
	sendCodeRateLimit := appmiddleware.RateLimit(svc.Redis, 10, 10*time.Minute)
	authPublic := app.Group("/api/v1/auth", authRateLimit)
	if svc.AuthHandler != nil {
		authPublic.Post("/send-code", sendCodeRateLimit, svc.AuthHandler.SendCode)
		authPublic.Post("/register", svc.AuthHandler.Register)
		authPublic.Post("/login", svc.AuthHandler.Login)
		authPublic.Post("/refresh", svc.AuthHandler.Refresh)
		authPublic.Post("/logout", svc.AuthHandler.Logout)
		authPublic.Post("/wx-login", svc.AuthHandler.WXLogin)
	}

	// ---------------------------------------------------------------------------
	// Agent communication endpoints (API key auth, no JWT required).
	// Registered before the JWT-protected group to avoid prefix-matching conflicts.
	// ---------------------------------------------------------------------------

	if svc.AgentHandler != nil {
		agentLimiter := appmiddleware.RateLimit(svc.Redis, 300, 1*time.Minute)
		agentAPI := app.Group("/api/v1/agent", agentLimiter, svc.AgentHandler.AuthMiddleware)
		agentAPI.Post("/upload", svc.AgentHandler.Upload)
		agentAPI.Post("/progress", svc.AgentHandler.Progress)
	}

	// ---------------------------------------------------------------------------
	// Public channel config endpoint (no auth required — returns static data).
	// ---------------------------------------------------------------------------

	if svc.ChannelHandler != nil {
		app.Get("/api/v1/channels/platform-configs", svc.ChannelHandler.GetPlatformConfigs)
	}

	// ---------------------------------------------------------------------------
	// Protected API group — /api/v1 (requires authentication)
	// ---------------------------------------------------------------------------

	authMiddleware := appmiddleware.AuthMiddleware(svc.JWTService, svc.Repo, svc.Logger)
	rateLimiter := appmiddleware.RateLimit(svc.Redis, 100, 1*time.Minute)

	apiV1 := app.Group("/api/v1", authMiddleware, rateLimiter)

	if svc.AuthHandler != nil {
		apiV1.Get("/auth/me", svc.AuthHandler.Me)
		apiV1.Put("/auth/password", svc.AuthHandler.ChangePassword)
	}

	// ---------------------------------------------------------------------------
	// Channel management
	// ---------------------------------------------------------------------------

	if svc.ChannelHandler != nil {
		apiV1.Get("/channels", svc.ChannelHandler.List)
		apiV1.Post("/channels", svc.ChannelHandler.Create)
		apiV1.Post("/channels/fetch-profile", svc.ChannelHandler.FetchProfile)
		apiV1.Get("/channels/:id", svc.ChannelHandler.Get)
		apiV1.Put("/channels/:id", svc.ChannelHandler.Update)
		apiV1.Patch("/channels/:id/archive", svc.ChannelHandler.Archive)
		apiV1.Patch("/channels/:id/restore", svc.ChannelHandler.Restore)
		apiV1.Delete("/channels/:id", svc.ChannelHandler.Delete)
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
		apiV1.Patch("/tasks/:id/published", svc.TaskHandler.MarkPublished)
		apiV1.Get("/tasks/:id/files", svc.TaskHandler.GetFiles)
		apiV1.Get("/tasks/:id/stream", svc.TaskHandler.Stream)
		apiV1.Get("/tasks/:id/preview", svc.TaskHandler.PreviewHTML)
		apiV1.Get("/tasks/:id/files/zip", svc.TaskHandler.DownloadZip)
		apiV1.Get("/tasks/:id/files/:fileId/download", svc.TaskHandler.DownloadFile)
		apiV1.Get("/usage/stats", svc.TaskHandler.UsageStats)
	}

	// Local file serving (only when using local storage provider).
	if svc.TaskHandler != nil && svc.StorageProvider != nil && svc.StorageProvider.Name() == "local" {
		apiV1.Get("/files/*", svc.TaskHandler.ServeLocalFile)
	}

	// File upload endpoint.
	if svc.FileHandler != nil {
		apiV1.Post("/files/upload", svc.FileHandler.Upload)
	}

	// ---------------------------------------------------------------------------
	// Timeline endpoint
	// ---------------------------------------------------------------------------

	if svc.TimelineHandler != nil {
		apiV1.Get("/timeline", svc.TimelineHandler.GetTimeline)
	}

	// ---------------------------------------------------------------------------
	// Credits endpoints
	// ---------------------------------------------------------------------------

	if svc.CreditHandler != nil {
		credits := apiV1.Group("/credits")
		credits.Get("/balance", svc.CreditHandler.Balance)
		credits.Get("/sign-in/status", svc.CreditHandler.SignInStatus)
		credits.Post("/sign-in", svc.CreditHandler.SignIn)
		credits.Get("/transactions", svc.CreditHandler.Transactions)
	}

	// Admin credits endpoint (outside JWT auth group, uses API key auth).
	if svc.CreditHandler != nil {
		adminLimiter := appmiddleware.RateLimit(svc.Redis, 10, 1*time.Minute)
		app.Post("/api/v1/admin/credits/grant", adminLimiter, svc.CreditHandler.AdminGrant)
	}

	// ---------------------------------------------------------------------------
	// API Key endpoints
	// ---------------------------------------------------------------------------

	if svc.APIKeyHandler != nil {
		apiV1.Post("/api-keys", svc.APIKeyHandler.Create)
		apiV1.Get("/api-keys", svc.APIKeyHandler.List)
		apiV1.Delete("/api-keys/:id", svc.APIKeyHandler.Revoke)
	}

	// ---------------------------------------------------------------------------
	// Feedback endpoint
	// ---------------------------------------------------------------------------

	if svc.FeedbackHandler != nil {
		apiV1.Post("/feedback", svc.FeedbackHandler.Create)
	}

	// Model config endpoints
	// ---------------------------------------------------------------------------

	if svc.ModelConfigHandler != nil {
		apiV1.Get("/model-config", svc.ModelConfigHandler.Get)
		apiV1.Put("/model-config", svc.ModelConfigHandler.Update)
		apiV1.Delete("/model-config", svc.ModelConfigHandler.Delete)
	}

	// MCP endpoint (API key auth, no JWT required).
	if svc.MCPHandler != nil {
		mcpHandler := adaptor.HTTPHandler(svc.MCPHandler)
		app.All("/mcp", mcpHandler)
	}

	return app
}
