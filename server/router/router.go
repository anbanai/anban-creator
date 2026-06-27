package router

import (
	"context"
	"net/http"
	"slices"
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
	Config                   *config.Config
	Logger                   *zerolog.Logger
	DB                       *gorm.DB
	Redis                    *redis.Client
	Repo                     repository.Repository
	JWTService               *auth.JWTService
	WechatSvc                *auth.WeChatService
	WSHub                    *handler.WebSocketHub
	AuthHandler              *handler.AuthHandler
	Executor                 agent.TaskExecutor
	PlanService              *service.PlanService
	TaskService              *service.TaskService
	CreditService            *service.CreditService
	PlanHandler              *handler.PlanHandler
	TaskHandler              *handler.TaskHandler
	SeednoteAnalyticsHandler *handler.SeednoteAnalyticsHandler
	AgentHandler             *handler.AgentHandler
	CreditHandler            *handler.CreditHandler
	ProjectHandler           *handler.ProjectHandler
	TimelineHandler          *handler.TimelineHandler
	APIKeyHandler            *handler.APIKeyHandler
	FileHandler              *handler.FileHandler
	FeedbackHandler          *handler.FeedbackHandler
	ModelConfigHandler       *handler.ModelConfigHandler
	ImageModelHandler        *handler.ImageModelHandler
	TemplateHandler          *handler.TemplateHandler
	ViralAnalysisHandler     *handler.ViralAnalysisHandler
	PosterHandler            *handler.PosterHandler
	ResourceHandler          *handler.ResourceHandler
	TopicPoolHandler         *handler.TopicPoolHandler
	DesignerHandler          *handler.DesignerHandler
	MCPHandler               http.Handler
	StorageProvider          storage.Provider
}

// NewRouter creates a new Fiber app with middleware and route groups.
func NewRouter(svc *Services) *fiber.App {
	app := fiber.New(fiber.Config{
		BodyLimit:    50 * 1024 * 1024, // 50 MB
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 16 * time.Minute, // > plugin .mcp.json timeout (15min) so long LLM calls survive
		IdleTimeout:  120 * time.Second,
	})

	// ---------------------------------------------------------------------------
	// Global middleware (in order)
	// ---------------------------------------------------------------------------

	// Request ID: inject X-Request-ID header into context.
	app.Use(requestid.New())

	// CORS: configurable allowed origins. Always includes localhost for local development.
	corsConfig := cors.Config{
		AllowOriginsFunc: func(origin string) bool {
			if strings.HasPrefix(origin, "http://localhost") ||
				strings.HasPrefix(origin, "http://127.0.0.1") {
				return true
			}
			return slices.Contains(svc.Config.CORS.AllowedOrigins, origin)
		},
		AllowCredentials: true,
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
		if svc.AuthHandler != nil {
			app.Get("/ws/login", svc.WSHub.HandleLoginWebSocket(svc.AuthHandler))
		}
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
		authPublic.Post("/code-login", svc.AuthHandler.CodeLogin)
		authPublic.Post("/refresh", svc.AuthHandler.Refresh)
		authPublic.Post("/logout", svc.AuthHandler.Logout)
		authPublic.Post("/wx-login", svc.AuthHandler.WXLogin)
		authPublic.Post("/qrcode", svc.AuthHandler.GenerateQRCode)
		authPublic.Post("/scanned", svc.AuthHandler.NotifyScanned)
		authPublic.Post("/qr-callback", svc.AuthHandler.QRLoginCallback)
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
	// Public project config endpoint (no auth required — returns static data).
	// ---------------------------------------------------------------------------

	if svc.ProjectHandler != nil {
		app.Get("/api/v1/projects/platform-configs", svc.ProjectHandler.GetPlatformConfigs)
	}

	// Public resource catalog endpoint (no auth required).
	if svc.ResourceHandler != nil {
		// Theme preview (registered before the :category wildcards; distinct
		// segment count avoids any match conflict). Renders static theme styling.
		app.Get("/api/v1/resources/themes/:name/preview", svc.ResourceHandler.PreviewTheme)
		app.Get("/api/v1/resources/:category", svc.ResourceHandler.List)
		app.Get("/api/v1/resources/:category/:name", svc.ResourceHandler.Get)
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
		apiV1.Post("/auth/set-password", svc.AuthHandler.SetPassword)
	}

	// ---------------------------------------------------------------------------
	// Seednote login status
	// ---------------------------------------------------------------------------

	if svc.ProjectHandler != nil {
		apiV1.Get("/seednote/login-status", svc.ProjectHandler.SeednoteLoginStatus)
	}

	// ---------------------------------------------------------------------------
	// Project management
	// ---------------------------------------------------------------------------

	if svc.ProjectHandler != nil {
		apiV1.Get("/projects", svc.ProjectHandler.List)
		apiV1.Get("/projects/stats", svc.ProjectHandler.Stats)
		apiV1.Post("/projects", svc.ProjectHandler.Create)
		apiV1.Post("/projects/fetch-profile", svc.ProjectHandler.FetchProfile)
		apiV1.Post("/projects/analyze-image", svc.ProjectHandler.AnalyzeImage)
		apiV1.Get("/projects/:id", svc.ProjectHandler.Get)
		apiV1.Put("/projects/:id", svc.ProjectHandler.Update)
		apiV1.Patch("/projects/:id/archive", svc.ProjectHandler.Archive)
		apiV1.Patch("/projects/:id/restore", svc.ProjectHandler.Restore)
		apiV1.Delete("/projects/:id", svc.ProjectHandler.Delete)
	}

	// ---------------------------------------------------------------------------
	// Topic pool endpoints (nested under projects)
	// ---------------------------------------------------------------------------

	if svc.TopicPoolHandler != nil {
		apiV1.Get("/projects/:project_id/topics", svc.TopicPoolHandler.List)
		apiV1.Post("/projects/:project_id/topics", svc.TopicPoolHandler.Create)
		apiV1.Delete("/projects/:project_id/topics/:id", svc.TopicPoolHandler.Delete)
		apiV1.Patch("/projects/:project_id/topics/:id/reset", svc.TopicPoolHandler.Reset)
	}

	// ---------------------------------------------------------------------------
	// Designer endpoints (image generation studio)
	// ---------------------------------------------------------------------------

	if svc.DesignerHandler != nil {
		designer := apiV1.Group("/designer")
		designer.Get("/providers", svc.DesignerHandler.GetProviders)
		designer.Post("/generate", svc.DesignerHandler.Generate)
		designer.Post("/upload-reference", svc.DesignerHandler.UploadReference)
		designer.Post("/upload-reference-from-url", svc.DesignerHandler.UploadReferenceFromURL)
		designer.Get("/history", svc.DesignerHandler.GetHistory)
		designer.Get("/generations/:id", svc.DesignerHandler.GetGeneration)
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
		apiV1.Post("/tasks/files/zip", svc.TaskHandler.DownloadTasksZip)
		apiV1.Get("/tasks/:id", svc.TaskHandler.GetByID)
		if svc.SeednoteAnalyticsHandler != nil {
			apiV1.Get("/tasks/:id/seednote-analytics", svc.SeednoteAnalyticsHandler.GetTaskAnalytics)
		}
		apiV1.Delete("/tasks/:id", svc.TaskHandler.Delete)
		apiV1.Post("/tasks/:id/cancel", svc.TaskHandler.Cancel)
		apiV1.Post("/tasks/:id/retry", svc.TaskHandler.Retry)
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

	// File serving for non-local storage (e.g. OSS).
	if svc.FileHandler != nil && svc.StorageProvider != nil && svc.StorageProvider.Name() != "local" {
		apiV1.Get("/files/*", svc.FileHandler.ServeFile)
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
		credits.Get("/pricing", svc.CreditHandler.Pricing)
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

	// Image model options endpoint (tier-gated listing for create-task/plan dropdowns).
	if svc.ImageModelHandler != nil {
		apiV1.Get("/image-models", svc.ImageModelHandler.List)
	}

	// ---------------------------------------------------------------------------
	// Template endpoints
	// ---------------------------------------------------------------------------

	if svc.TemplateHandler != nil {
		templates := apiV1.Group("/templates")
		templates.Get("/", svc.TemplateHandler.List)
		templates.Post("/", svc.TemplateHandler.Create)
		templates.Get("/:id", svc.TemplateHandler.GetByID)
		templates.Put("/:id", svc.TemplateHandler.Update)
		templates.Delete("/:id", svc.TemplateHandler.Delete)
	}

	// ---------------------------------------------------------------------------
	// Viral analysis endpoints
	// ---------------------------------------------------------------------------

	if svc.ViralAnalysisHandler != nil {
		viralAnalyses := apiV1.Group("/viral-analyses")
		viralAnalyses.Post("/", svc.ViralAnalysisHandler.Create)
		viralAnalyses.Get("/", svc.ViralAnalysisHandler.List)
		viralAnalyses.Get("/:id", svc.ViralAnalysisHandler.GetByID)
	}

	// ---------------------------------------------------------------------------
	// Poster endpoints
	// ---------------------------------------------------------------------------

	if svc.PosterHandler != nil {
		posters := apiV1.Group("/posters")
		posters.Post("/", svc.PosterHandler.Create)
		posters.Get("/", svc.PosterHandler.List)
		posters.Get("/:id", svc.PosterHandler.GetByID)
	}

	// MCP endpoint (API key auth, no JWT required).
	if svc.MCPHandler != nil {
		mcpHandler := adaptor.HTTPHandler(svc.MCPHandler)
		app.All("/mcp", mcpHandler)
	}

	return app
}
