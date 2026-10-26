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

	"github.com/anbanai/anban-creator/server/auth"
	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/handler"
	appmiddleware "github.com/anbanai/anban-creator/server/middleware"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
)

// Services aggregates all service dependencies required by the router.
type Services struct {
	PortraitOnboardingHandler           *handler.PortraitOnboardingHandler
	AnalyticsHandler                    *handler.AnalyticsHandler
	ContentAnalyticsHandler             *handler.ContentAnalyticsHandler
	Config                              *config.Config
	Logger                              *zerolog.Logger
	DB                                  *gorm.DB
	Redis                               *redis.Client
	Repo                                repository.Repository
	JWTService                          *auth.JWTService
	WechatSvc                           *auth.WeChatService
	WSHub                               *handler.WebSocketHub
	AuthHandler                         *handler.AuthHandler
	PlanService                         *service.PlanService
	TaskService                         *service.TaskService
	PlanHandler                         *handler.PlanHandler
	TaskHandler                         *handler.TaskHandler
	SeednoteAnalyticsHandler            *handler.SeednoteAnalyticsHandler
	SeednoteImportHandler               *handler.SeednoteImportHandler
	WechatAnalyticsHandler              *handler.WechatAnalyticsHandler
	WechatAnalyticsImportHandler        *handler.WechatAnalyticsImportHandler
	WechatAccountAnalyticsImportHandler *handler.WechatAccountAnalyticsImportHandler
	WechatAccountAnalyticsHandler       *handler.WechatAccountAnalyticsHandler
	WechatPublicationHandler            *handler.WechatPublicationHandler
	ChannelsAnalyticsHandler            *handler.ChannelsAnalyticsHandler
	AgentHandler                        *handler.AgentHandler
	InteractiveHandler                  *handler.InteractiveHandler
	AssistantHandler                    *handler.AssistantHandler
	AgentProfileHandler                 *handler.AgentProfileHandler
	AgentPackHandler                    *handler.AgentPackHandler
	BillingHandler                      *handler.BillingHandler
	BillingAdminHandler                 *handler.BillingAdminHandler
	ProjectHandler                      *handler.ProjectHandler
	FeedbackDashboardHandler            *handler.FeedbackDashboardHandler
	TimelineHandler                     *handler.TimelineHandler
	APIKeyHandler                       *handler.APIKeyHandler
	FileHandler                         *handler.FileHandler
	UploadHandler                       *handler.UploadHandler
	UserMediaLibraryHandler             *handler.UserMediaLibraryHandler
	AIEntryHandler                      *handler.AIEntryHandler
	FeedbackHandler                     *handler.FeedbackHandler
	ImageCapabilityHandler              *handler.ImageCapabilityHandler
	HypitCapabilityHandler              *handler.HypitCapabilityHandler
	MontageCapabilityHandler            *handler.MontageCapabilityHandler
	LocalLifeCapabilityHandler          *handler.LocalLifeCapabilityHandler
	TemplateHandler                     *handler.TemplateHandler
	ImageAnalysisHandler                *handler.ImageAnalysisHandler
	ViralAnalysisHandler                *handler.ViralAnalysisHandler
	PosterHandler                       *handler.PosterHandler
	ResourceHandler                     *handler.ResourceHandler
	TopicPoolHandler                    *handler.TopicPoolHandler
	ContentAssetHandler                 *handler.ContentAssetHandler
	TrendsHandler                       *handler.TrendsHandler
	IlinkHandler                        *handler.IlinkHandler
	MCPHandler                          http.Handler
	StorageProvider                     storage.Provider
}

// NewRouter creates a new Fiber app with middleware and route groups.
func NewRouter(svc *Services) *fiber.App {
	app := fiber.New(fiber.Config{
		BodyLimit:         50 * 1024 * 1024, // 50 MB
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      16 * time.Minute, // > plugin .mcp.json timeout (15min) so long LLM calls survive
		IdleTimeout:       120 * time.Second,
		StreamRequestBody: true,
	})
	if svc.Repo != nil && svc.StorageProvider != nil {
		articleAssets := handler.NewArticleAssetHandler(svc.Repo, svc.StorageProvider, svc.Logger)
		app.Get("/api/v1/article-assets/:id", articleAssets.Get)
		app.Delete("/api/v1/article-assets/:id", appmiddleware.AuthMiddleware(svc.JWTService, svc.Repo, svc.Logger), articleAssets.Delete)
	}

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
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "Idempotency-Key", "Last-Event-ID"},
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

	// Auth and verification-code requests have separate, configurable limits.
	authRateLimit := appmiddleware.RateLimit(svc.Redis, "auth", svc.Config.RateLimit.Auth.Requests, svc.Config.RateLimit.Auth.Window)
	sendCodeRateLimit := appmiddleware.RateLimit(svc.Redis, "send_code", svc.Config.RateLimit.SendCode.Requests, svc.Config.RateLimit.SendCode.Window)
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
	// Agent communication endpoints use execution-scoped tokens for all
	// execution mutations.
	// Registered before the JWT-protected group to avoid prefix-matching conflicts.
	// ---------------------------------------------------------------------------

	if svc.AgentHandler != nil {
		agentLimiter := appmiddleware.RateLimit(svc.Redis, "agent", svc.Config.RateLimit.Agent.Requests, svc.Config.RateLimit.Agent.Window)
		app.Post("/api/v1/agent/bootstrap", agentLimiter, svc.AgentHandler.WorkloadAuthMiddleware, svc.AgentHandler.Bootstrap)
		// Register each Agent route explicitly. A prefix group here also applies its
		// execution-token middleware to user-facing GET routes under /agent, such
		// as the execution-profile capability endpoint below.
		app.Post("/api/v1/agent/artifacts/prepare", agentLimiter, svc.AgentHandler.ExecutionAuthMiddleware, svc.AgentHandler.PrepareArtifactUpload)
		app.Post("/api/v1/agent/artifacts/content", agentLimiter, svc.AgentHandler.ExecutionAuthMiddleware, svc.AgentHandler.StreamArtifactContent)
		app.Post("/api/v1/agent/artifacts/manifest", agentLimiter, svc.AgentHandler.ExecutionAuthMiddleware, svc.AgentHandler.ReportArtifactManifest)
		app.Post("/api/v1/agent/progress-plan", agentLimiter, svc.AgentHandler.ExecutionAuthMiddleware, svc.AgentHandler.ProgressPlan)
		app.Post("/api/v1/agent/progress", agentLimiter, svc.AgentHandler.ExecutionAuthMiddleware, svc.AgentHandler.Progress)
		app.Post("/api/v1/agent/complete", agentLimiter, svc.AgentHandler.ExecutionAuthMiddleware, svc.AgentHandler.Complete)
	}

	if svc.AgentPackHandler != nil {
		app.Get("/api/v1/agent-packs", svc.AgentPackHandler.List)
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
	rateLimiter := appmiddleware.RateLimit(svc.Redis, "api", svc.Config.RateLimit.API.Requests, svc.Config.RateLimit.API.Window)
	apiV1 := app.Group("/api/v1", authMiddleware, rateLimiter)
	if svc.InteractiveHandler != nil {
		interactive := apiV1.Group("/interactive")
		interactive.Get("/agents", svc.InteractiveHandler.Agents)
		interactive.Post("/sessions", svc.InteractiveHandler.Create)
		interactive.Get("/sessions/:id/events", svc.InteractiveHandler.Events)
		interactive.Post("/sessions/:id/messages", svc.InteractiveHandler.Message)
		interactive.Post("/sessions/:id/questions/:callId/answers", svc.InteractiveHandler.Answers)
		interactive.Post("/sessions/:id/commit", svc.InteractiveHandler.Commit)
		interactive.Delete("/sessions/:id", svc.InteractiveHandler.Delete)
	}
	if svc.AssistantHandler != nil {
		assistant := apiV1.Group("/assistant")
		assistant.Post("/interactions", svc.AssistantHandler.InteractionCreate)
		assistant.Get("/interactions/:id", svc.AssistantHandler.InteractionSnapshot)
		assistant.Post("/interactions/:id/messages", svc.AssistantHandler.InteractionMessage)
		assistant.Post("/interactions/:id/questions/:callId/answers", svc.AssistantHandler.InteractionAnswer)
		assistant.Get("/interactions/:id/events", svc.AssistantHandler.InteractionEvents)
		assistant.Post("/interactions/:id/commit", svc.AssistantHandler.InteractionCommit)
		assistant.Get("/bootstrap", svc.AssistantHandler.Bootstrap)
		assistant.Get("/projects/:projectId/works", svc.AssistantHandler.Works)
		assistant.Get("/checkpoint", svc.AssistantHandler.LatestCheckpoint)
		assistant.Post("/sessions", svc.AssistantHandler.Create)
		assistant.Get("/sessions/:id/events", svc.AssistantHandler.Events)
		assistant.Post("/sessions/:id/messages", svc.AssistantHandler.Message)
		assistant.Post("/sessions/:id/questions/:callId/answers", svc.AssistantHandler.Answers)
		assistant.Post("/sessions/:id/checkpoint", svc.AssistantHandler.Checkpoint)
		assistant.Post("/sessions/:id/commit", svc.AssistantHandler.Commit)
		assistant.Delete("/sessions/:id", svc.AssistantHandler.Delete)
		assistant.Post("/workspaces", svc.AssistantHandler.WorkspaceCreate)
		assistant.Post("/workspaces/:id/mode", svc.AssistantHandler.WorkspaceMode)
		assistant.Get("/workspaces/:id/events", svc.AssistantHandler.WorkspaceEvents)
		assistant.Post("/workspaces/:id/messages", svc.AssistantHandler.WorkspaceMessage)
		assistant.Post("/workspaces/:id/questions/:callId/answers", svc.AssistantHandler.WorkspaceAnswers)
		assistant.Post("/workspaces/:id/checkpoint", svc.AssistantHandler.WorkspaceCheckpoint)
		assistant.Post("/workspaces/:id/commit", svc.AssistantHandler.WorkspaceCommit)
		assistant.Delete("/workspaces/:id", svc.AssistantHandler.WorkspaceDelete)
	}
	if h := svc.PortraitOnboardingHandler; h != nil {
		apiV1.Get("/onboarding/capabilities", h.Capabilities)
		apiV1.Post("/onboarding/chat", h.Chat)
		apiV1.Post("/onboarding/transcribe", h.Transcribe)
	}
	if svc.BillingHandler != nil {
		billingAPI := apiV1.Group("/billing")
		billingAPI.Get("/wallet", svc.BillingHandler.Wallet)
		billingAPI.Get("/catalog", svc.BillingHandler.Catalog)
		billingAPI.Get("/transactions", svc.BillingHandler.Transactions)
		billingAPI.Post("/quotes", svc.BillingHandler.CreateTaskQuote)
		billingAPI.Get("/referral", svc.BillingHandler.Referral)

		adminBillingLimiter := appmiddleware.RateLimit(svc.Redis, "admin", svc.Config.RateLimit.Admin.Requests, svc.Config.RateLimit.Admin.Window)
		app.Post("/api/admin/billing/topups", adminBillingLimiter, svc.BillingHandler.AdminAuth, svc.BillingHandler.AdminTopUp)
		if svc.BillingAdminHandler != nil {
			app.Get("/api/admin/billing/costs", adminBillingLimiter, svc.BillingHandler.AdminAuth, svc.BillingAdminHandler.Costs)
			app.Get("/api/admin/billing/margins", adminBillingLimiter, svc.BillingHandler.AdminAuth, svc.BillingAdminHandler.Margins)
			app.Get("/api/admin/billing/reconciliation", adminBillingLimiter, svc.BillingHandler.AdminAuth, svc.BillingAdminHandler.Reconciliation)
			adminStudioBilling := apiV1.Group("/admin/billing", adminBillingLimiter)
			adminStudioBilling.Get("/users", svc.BillingAdminHandler.Users)
			adminStudioBilling.Post("/topups", svc.BillingAdminHandler.TopUp)
			adminStudioBilling.Get("/topups", svc.BillingAdminHandler.TopUpOperations)
			adminStudioBilling.Get("/entries", svc.BillingAdminHandler.Entries)
		}
	}

	if svc.AuthHandler != nil {
		apiV1.Get("/auth/me", svc.AuthHandler.Me)
		apiV1.Put("/auth/password", svc.AuthHandler.ChangePassword)
		apiV1.Post("/auth/set-password", svc.AuthHandler.SetPassword)
	}
	if svc.AgentProfileHandler != nil {
		apiV1.Get("/agent/execution-profiles", svc.AgentProfileHandler.List)
	}

	// ---------------------------------------------------------------------------
	// Project management
	// ---------------------------------------------------------------------------

	if svc.ProjectHandler != nil {
		apiV1.Get("/projects/platform-configs", svc.ProjectHandler.GetPlatformConfigs)
		apiV1.Get("/projects", svc.ProjectHandler.List)
		apiV1.Get("/projects/stats", svc.ProjectHandler.Stats)
		apiV1.Post("/projects", svc.ProjectHandler.Create)
		apiV1.Get("/projects/:id", svc.ProjectHandler.Get)
		apiV1.Get("/projects/:id/channel-configs", svc.ProjectHandler.ListChannelConfigs)
		apiV1.Get("/projects/:id/channel-configs/:channel", svc.ProjectHandler.GetChannelConfig)
		apiV1.Put("/projects/:id/channel-configs/:channel", svc.ProjectHandler.UpsertChannelConfig)
		apiV1.Delete("/projects/:id/channel-configs/:channel", svc.ProjectHandler.DeleteChannelConfig)
		apiV1.Get("/projects/:id/platform-configs/wechat", svc.ProjectHandler.GetWechatPlatformConfig)
		apiV1.Put("/projects/:id/platform-configs/wechat", svc.ProjectHandler.UpsertWechatPlatformConfig)
		if svc.FeedbackDashboardHandler != nil {
			apiV1.Get("/projects/:id/feedback", svc.FeedbackDashboardHandler.Get)
			apiV1.Post("/projects/:id/feedback/rerun", svc.FeedbackDashboardHandler.Rerun)
			apiV1.Post("/projects/:id/feedback/:state", svc.FeedbackDashboardHandler.SetPaused)
		}
		apiV1.Get("/projects/:id/profile", svc.ProjectHandler.GetProfile)
		apiV1.Get("/projects/:id/profile/revisions", svc.ProjectHandler.GetProfileRevisions)
		apiV1.Post("/projects/:id/profile/revisions/:revision/restore", svc.ProjectHandler.RestoreProfileRevision)
		apiV1.Post("/projects/:id/profile/analysis/quote", svc.ProjectHandler.ProfileAnalysisQuote)
		apiV1.Post("/projects/:id/profile/analysis", svc.ProjectHandler.StartProfileAnalysis)
		apiV1.Get("/projects/:id/profile/analysis", svc.ProjectHandler.GetProfileAnalysis)
		apiV1.Post("/projects/:id/profile/refresh", svc.ProjectHandler.RefreshProfile)
		apiV1.Post("/projects/:id/profile/retry", svc.ProjectHandler.RetryProfile)
		apiV1.Put("/projects/:id/profile", svc.ProjectHandler.UpdateProfile)
		apiV1.Patch("/projects/:id/profile/:dimension", svc.ProjectHandler.UpdateProfileDimension)
		apiV1.Patch("/projects/:id/profile/dimensions/:dimension", svc.ProjectHandler.UpdateProfileDimension)
		apiV1.Get("/projects/:id/memory", svc.ProjectHandler.Memory)
		apiV1.Put("/projects/:id/memory", svc.ProjectHandler.UpdateMemory)
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
	if svc.ContentAssetHandler != nil {
		apiV1.Get("/content-assets", svc.ContentAssetHandler.List)
		apiV1.Get("/content-assets/by-content", svc.ContentAssetHandler.GetLineageByContent)
		apiV1.Get("/content-assets/recommendations", svc.ContentAssetHandler.Recommendations)
		apiV1.Get("/content-reuse-runs", svc.ContentAssetHandler.ListReuseRuns)
		apiV1.Post("/content-assets", svc.ContentAssetHandler.Create)
		apiV1.Post("/content-assets/ingest", svc.ContentAssetHandler.Ingest)
		apiV1.Get("/content-assets/:id/lineage", svc.ContentAssetHandler.GetLineage)
		apiV1.Get("/content-assets/:id", svc.ContentAssetHandler.Get)
		apiV1.Post("/content-assets/:id/score", svc.ContentAssetHandler.Score)
		apiV1.Patch("/content-assets/:id", svc.ContentAssetHandler.Update)
		apiV1.Post("/content-assets/:id/links", svc.ContentAssetHandler.Link)
		apiV1.Post("/content-assets/:id/reuse-runs", svc.ContentAssetHandler.CreateReuseRun)
		apiV1.Get("/content-reuse-runs/:id", svc.ContentAssetHandler.GetReuseRun)
		apiV1.Post("/content-reuse-runs/:id/:status", svc.ContentAssetHandler.SetReuseRunStatus)
		apiV1.Post("/content-reuse-runs/:id/pause", svc.ContentAssetHandler.PauseReuseRun)
		apiV1.Post("/content-reuse-runs/:id/resume", svc.ContentAssetHandler.ResumeReuseRun)
		apiV1.Post("/content-reuse-runs/:id/cancel", svc.ContentAssetHandler.CancelReuseRun)
		apiV1.Get("/content-autopilot-policy", svc.ContentAssetHandler.GetPolicy)
		apiV1.Put("/content-autopilot-policy", svc.ContentAssetHandler.UpdatePolicy)
	}
	if svc.TrendsHandler != nil {
		apiV1.Get("/trends", svc.TrendsHandler.List)
		apiV1.Post("/trends/refresh", svc.TrendsHandler.Refresh)
	}

	// ---------------------------------------------------------------------------
	// Plan endpoints
	// ---------------------------------------------------------------------------

	if svc.PlanHandler != nil {
		apiV1.Post("/plans", svc.PlanHandler.Create)
		apiV1.Get("/plans", svc.PlanHandler.List)
		apiV1.Get("/plans/schedule-recommendation", svc.PlanHandler.ScheduleRecommendation)
		apiV1.Get("/plans/:id", svc.PlanHandler.GetByID)
		apiV1.Put("/plans/:id", svc.PlanHandler.Update)
		apiV1.Delete("/plans/:id", svc.PlanHandler.Delete)
		apiV1.Post("/plans/:id/pause", svc.PlanHandler.Pause)
		apiV1.Post("/plans/:id/resume", svc.PlanHandler.Resume)
		apiV1.Post("/plans/:id/execute", svc.PlanHandler.Execute)
	}

	// ---------------------------------------------------------------------------
	// Task endpoints
	// ---------------------------------------------------------------------------

	if svc.TaskHandler != nil {
		apiV1.Post("/tasks", svc.TaskHandler.Create)
		apiV1.Get("/tasks/before", svc.TaskHandler.ListBefore)
		apiV1.Get("/tasks", svc.TaskHandler.List)
		apiV1.Post("/tasks/files/zip", svc.TaskHandler.DownloadTasksZip)
		// Bulk operations (static segments, registered before /tasks/:id to win
		// over the param route; best-effort, ≤100 ids, per-task results).
		apiV1.Post("/tasks/bulk-cancel", svc.TaskHandler.BulkCancel)
		apiV1.Post("/tasks/bulk-clone", svc.TaskHandler.BulkClone)
		apiV1.Post("/tasks/bulk-delete", svc.TaskHandler.BulkDelete)
		apiV1.Get("/tasks/:id", svc.TaskHandler.GetByID)
		if svc.SeednoteAnalyticsHandler != nil {
			apiV1.Get("/tasks/:id/seednote-analytics", svc.SeednoteAnalyticsHandler.GetTaskAnalytics)
			apiV1.Post("/tasks/:id/seednote-analytics/bind", svc.SeednoteAnalyticsHandler.BindTask)
		}
		if svc.WechatAnalyticsHandler != nil {
			apiV1.Get("/tasks/:id/wechat-analytics", svc.WechatAnalyticsHandler.GetTaskAnalytics)
		}
		if svc.ChannelsAnalyticsHandler != nil {
			apiV1.Get("/tasks/:id/channels-analytics", svc.ChannelsAnalyticsHandler.GetTaskAnalytics)
			apiV1.Post("/tasks/:id/channels-analytics/bind", svc.ChannelsAnalyticsHandler.BindTask)
		}
		apiV1.Delete("/tasks/:id", svc.TaskHandler.Delete)
		apiV1.Post("/tasks/:id/cancel", svc.TaskHandler.Cancel)
		apiV1.Post("/tasks/:id/clone", svc.TaskHandler.Clone)
		apiV1.Post("/tasks/:id/resume", svc.TaskHandler.Resume)
		apiV1.Get("/tasks/:id/files", svc.TaskHandler.GetFiles)
		apiV1.Get("/tasks/:id/stream", svc.TaskHandler.Stream)
		apiV1.Get("/tasks/:id/preview", svc.TaskHandler.PreviewHTML)
		apiV1.Get("/tasks/:id/wechat-picture-copy", svc.TaskHandler.ManualWechatPictureHTML)
		apiV1.Get("/tasks/:id/files/zip", svc.TaskHandler.DownloadZip)
		apiV1.Get("/tasks/:id/files/retained/zip", svc.TaskHandler.DownloadRetainedZip)
		apiV1.Get("/tasks/:id/files/:fileId/preview", svc.TaskHandler.PreviewFile)
		apiV1.Get("/tasks/:id/files/:fileId/download", svc.TaskHandler.DownloadFile)
		apiV1.Get("/usage/stats", svc.TaskHandler.UsageStats)
	}
	if svc.FeedbackHandler != nil {
		apiV1.Get("/tasks/:id/feedback", svc.FeedbackHandler.GetTaskFeedback)
		apiV1.Put("/tasks/:id/feedback", svc.FeedbackHandler.UpsertTaskFeedback)
		apiV1.Post("/tasks/:id/attribution", svc.FeedbackHandler.CreateAttribution)
		apiV1.Post("/feedback/insights/:id/confirm", svc.FeedbackHandler.ConfirmInsight)
		apiV1.Post("/feedback/insights/:id/validate", svc.FeedbackHandler.ValidateInsight)
		apiV1.Post("/feedback/insights/:id/promote", svc.FeedbackHandler.PromoteInsight)
	}
	if svc.AnalyticsHandler != nil {
		apiV1.Get("/projects/:id/content-analytics/overview", svc.AnalyticsHandler.Overview)
		apiV1.Get("/projects/:id/content-analytics/contents", svc.AnalyticsHandler.Contents)
		apiV1.Get("/projects/:id/content-analytics/contents/:contentId", svc.AnalyticsHandler.Detail)
		apiV1.Get("/projects/:id/content-analytics/contents/:contentId/observations", svc.AnalyticsHandler.Observations)
		apiV1.Get("/projects/:id/content-analytics/dates", svc.AnalyticsHandler.Dates)
	}
	if svc.ContentAnalyticsHandler != nil {
		apiV1.Get("/projects/:id/content-analytics/candidates", svc.ContentAnalyticsHandler.Candidates)
	}
	if svc.SeednoteImportHandler != nil {
		apiV1.Post("/projects/:id/seednote-analytics/imports/preview", svc.SeednoteImportHandler.Preview)
		apiV1.Post("/projects/:id/seednote-analytics/imports", svc.SeednoteImportHandler.Import)
		apiV1.Get("/projects/:id/seednote-analytics/imports", svc.SeednoteImportHandler.List)
		apiV1.Get("/projects/:id/seednote-analytics/imports/:batchId", svc.SeednoteImportHandler.Detail)
		apiV1.Get("/projects/:id/seednote-analytics/imports/:batchId/file", svc.SeednoteImportHandler.File)
		apiV1.Post("/projects/:id/seednote-analytics/imports/:batchId/revoke", svc.SeednoteImportHandler.Revoke)
	}
	if svc.WechatPublicationHandler != nil {
		apiV1.Get("/projects/:id/wechat/capabilities", svc.WechatPublicationHandler.Capabilities)
		apiV1.Get("/tasks/:id/wechat-publication", svc.WechatPublicationHandler.Get)
		apiV1.Post("/tasks/:id/wechat-publication/publish", svc.WechatPublicationHandler.Publish)
		apiV1.Post("/tasks/:id/wechat-publication/retry-publish", svc.WechatPublicationHandler.RetryPublish)
		apiV1.Post("/tasks/:id/wechat-publication/reconcile", svc.WechatPublicationHandler.Reconcile)
		apiV1.Post("/tasks/:id/wechat-publication/recover", svc.WechatPublicationHandler.Recover)
		apiV1.Post("/tasks/:id/wechat-publication/select", svc.WechatPublicationHandler.Select)
		apiV1.Post("/tasks/:id/wechat-publication/manual-bind", svc.WechatPublicationHandler.ManualBind)
		apiV1.Post("/tasks/:id/wechat-publication/skip", svc.WechatPublicationHandler.Skip)
		apiV1.Post("/tasks/:id/wechat-publication/restore", svc.WechatPublicationHandler.RestoreSkipped)
	}
	if svc.WechatAnalyticsImportHandler != nil {
		apiV1.Get("/wechat-analytics/imports/:batchId", svc.WechatAnalyticsImportHandler.GlobalDetail)

		apiV1.Post("/projects/:id/wechat-analytics/imports/preview", svc.WechatAnalyticsImportHandler.Preview)
		apiV1.Post("/projects/:id/wechat-analytics/imports", svc.WechatAnalyticsImportHandler.Import)
		apiV1.Get("/projects/:id/wechat-analytics/imports", svc.WechatAnalyticsImportHandler.List)
		apiV1.Get("/projects/:id/wechat-analytics/imports/:batchId", svc.WechatAnalyticsImportHandler.Detail)

		apiV1.Post("/projects/:id/wechat-analytics/imports/:batchId/revoke", svc.WechatAnalyticsImportHandler.Revoke)
	}
	if svc.WechatAccountAnalyticsHandler != nil {
		apiV1.Get("/projects/:id/wechat-analytics/overview", svc.WechatAccountAnalyticsHandler.Overview)
		apiV1.Get("/projects/:id/wechat-analytics/traffic-sources", svc.WechatAccountAnalyticsHandler.TrafficSources)
	}
	if svc.WechatAccountAnalyticsImportHandler != nil {
		apiV1.Post("/projects/:id/wechat-analytics/account-imports/preview", svc.WechatAccountAnalyticsImportHandler.Preview)
		apiV1.Post("/projects/:id/wechat-analytics/account-imports", svc.WechatAccountAnalyticsImportHandler.Import)
		apiV1.Get("/projects/:id/wechat-analytics/account-imports", svc.WechatAccountAnalyticsImportHandler.List)
		apiV1.Get("/projects/:id/wechat-analytics/account-imports/:batchId", svc.WechatAccountAnalyticsImportHandler.Detail)
		apiV1.Post("/projects/:id/wechat-analytics/account-imports/:batchId/revoke", svc.WechatAccountAnalyticsImportHandler.Revoke)
	}

	// Local file serving (only when using local storage provider).
	if svc.TaskHandler != nil && svc.StorageProvider != nil && svc.StorageProvider.Name() == "local" {
		apiV1.Get("/files/*", svc.TaskHandler.ServeLocalFile)
	}

	// File serving for non-local storage (e.g. OSS).
	if svc.FileHandler != nil && svc.StorageProvider != nil && svc.StorageProvider.Name() != "local" {
		apiV1.Get("/files/*", svc.FileHandler.ServeFile)
	}

	if svc.UploadHandler != nil {
		apiV1.Post("/uploads/prepare", svc.UploadHandler.Prepare)
		apiV1.Post("/uploads/resolve-asset-url", svc.UploadHandler.ResolveAssetDownloadURL)
		apiV1.Post("/uploads/resolve-download-url", svc.UploadHandler.ResolveDownloadURL)
	}
	if svc.UserMediaLibraryHandler != nil {
		mediaAssets := apiV1.Group("/media-assets")
		mediaAssets.Post("/", svc.UserMediaLibraryHandler.Create)
		mediaAssets.Get("/", svc.UserMediaLibraryHandler.List)
		mediaAssets.Get("/:id", svc.UserMediaLibraryHandler.Get)
		mediaAssets.Patch("/:id", svc.UserMediaLibraryHandler.Update)
		mediaAssets.Post("/:id/reanalyze", svc.UserMediaLibraryHandler.Reanalyze)
		mediaAssets.Delete("/:id", svc.UserMediaLibraryHandler.Remove)
	}
	if svc.AIEntryHandler != nil {
		apiV1.Post("/ai-entry/submit", svc.AIEntryHandler.Submit)
	}

	// ---------------------------------------------------------------------------
	// Timeline endpoint
	// ---------------------------------------------------------------------------

	if svc.TimelineHandler != nil {
		apiV1.Get("/timeline", svc.TimelineHandler.GetTimeline)
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

	// ---------------------------------------------------------------------------
	// ilink WeChat assistant endpoints — binding + task notifications + commands
	// ---------------------------------------------------------------------------

	if svc.IlinkHandler != nil {
		ilink := apiV1.Group("/ilink")
		ilink.Get("/status", svc.IlinkHandler.GetStatus)
		ilink.Post("/bind-code", svc.IlinkHandler.CreateBindCode)
		ilink.Post("/unbind", svc.IlinkHandler.Unbind)
		ilink.Put("/default-project", svc.IlinkHandler.SetDefaultProject)
	}

	if svc.ImageCapabilityHandler != nil {
		apiV1.Get("/image-capabilities", svc.ImageCapabilityHandler.List)
	}
	if svc.HypitCapabilityHandler != nil {
		apiV1.Get("/hypit-capabilities", svc.HypitCapabilityHandler.List)
	}
	if svc.MontageCapabilityHandler != nil {
		apiV1.Get("/montage-capabilities", svc.MontageCapabilityHandler.List)
	}
	if svc.LocalLifeCapabilityHandler != nil {
		apiV1.Get("/local-life-capabilities", svc.LocalLifeCapabilityHandler.List)
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
	if svc.ImageAnalysisHandler != nil {
		apiV1.Post("/image-analyses/:id/retry", svc.ImageAnalysisHandler.Retry)
		apiV1.Post("/image-analyses/:id/cancel", svc.ImageAnalysisHandler.Cancel)
	}

	// ---------------------------------------------------------------------------
	// Viral analysis endpoints
	// ---------------------------------------------------------------------------

	if svc.ViralAnalysisHandler != nil {
		viralAnalyses := apiV1.Group("/viral-analyses")
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
