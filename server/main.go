package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"github.com/royalrick/anbanwriter/server/agent"
	"github.com/royalrick/anbanwriter/server/auth"
	"github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/handler"
	"github.com/royalrick/anbanwriter/server/mcp"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/platform"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/router"
	"github.com/royalrick/anbanwriter/server/scheduler"
	"github.com/royalrick/anbanwriter/server/seednote"
	"github.com/royalrick/anbanwriter/server/service"
	"github.com/royalrick/anbanwriter/server/storage"
)

// defaultConfigPaths lists config file locations to try when -config is not set.
var defaultConfigPaths = []string{"./config.yaml", "./server/config.yaml"}

func main() {
	// 1. Parse flags.
	configPath := flag.String("config", "", "path to server config file (default: ./config.yaml or ./server/config.yaml)")
	port := flag.Int("port", 0, "server listen port (overrides config and ANBAN_SERVER_PORT)")
	flag.Parse()

	// Resolve config path: explicit flag > env > default search.
	cfgFile := resolveConfigPath(*configPath)

	// 2. Load config.
	cfg, err := config.NewConfig(cfgFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Override port via ANBAN_SERVER_PORT env or -port flag.
	if *port > 0 {
		cfg.Server.Port = *port
	} else if v := os.Getenv("ANBAN_SERVER_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			cfg.Server.Port = p
		}
	}

	// 3. Init logger (JSON to stderr, level from config, LOG_LEVEL env, or "info").
	logLevelStr := cfg.Logging.Level
	if logLevelStr == "" {
		logLevelStr = os.Getenv("LOG_LEVEL")
	}
	logLevel := parseLogLevel(logLevelStr)
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	logger := zerolog.New(os.Stderr).With().Timestamp().Logger().Level(logLevel)
	log := &logger

	log.Info().
		Int("port", cfg.Server.Port).
		Str("host", cfg.Server.Host).
		Int("asynq_concurrency", cfg.Asynq.Concurrency).
		Msg("config loaded")

	// 3.5. Validate image_presets config (key length, uniqueness, reserved words).
	// Fail fast so a malformed config surfaces immediately instead of as 500s on first task create.
	if err := config.ValidateImagePresets(cfg.ImagePresets); err != nil {
		log.Fatal().Err(err).Msg("invalid image_presets configuration")
	}

	// 4. Connect MySQL.
	mysqlDB := connectMySQL(context.Background(), cfg, log)

	// 5. Connect Redis.
	rdb := connectRedis(context.Background(), cfg, log)

	// 6. Auto-migrate models.
	if mysqlDB != nil {
		if err := model.AutoMigrate(mysqlDB); err != nil {
			log.Error().Err(err).Msg("failed to auto-migrate models")
		} else {
			log.Info().Msg("database migration completed")
		}

	}

	// 7. Create repository.
	var repo repository.Repository
	if mysqlDB != nil {
		repo = repository.New(mysqlDB)
	}

	// 7.1 Create storage provider.
	store, err := storage.NewProvider(cfg.Storage, log)
	if err != nil {
		if cfg.Storage.Provider == "oss" {
			log.Fatal().Err(err).Msg("failed to create OSS storage provider (check credentials and configuration)")
		}
		log.Warn().Err(err).Msg("failed to create storage provider, file uploads will be unavailable")
		store = nil
	} else {
		log.Info().Str("provider", store.Name()).Msg("storage provider initialized")
	}

	// 8. Create JWT service.
	jwtSvc, err := auth.NewJWTService(
		cfg.JWT.SecretKey,
		cfg.JWT.AccessExpiry,
		cfg.JWT.RefreshExpiry,
	)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create JWT service")
	}

	// 9. Create WeChat service (nil if not configured).
	var wechatSvc *auth.WeChatService
	if cfg.WeChat.AppID != "" && cfg.WeChat.AppSecret != "" {
		wechatSvc = auth.NewWeChatService(cfg.WeChat.AppID, cfg.WeChat.AppSecret, log)
		log.Info().Msg("WeChat service initialized")
	}

	// 9.1 Create Seednote (种草笔记) sidecar client.
	var seednoteClient *seednote.Client
	{
		seednoteClient = seednote.NewClient(cfg.Seednote.BaseURL, time.Duration(cfg.Seednote.Timeout)*time.Second)
		hcCtx, hcCancel := context.WithTimeout(context.Background(), 3*time.Second)
		if err := seednoteClient.HealthCheck(hcCtx); err != nil {
			hcCancel()
			log.Error().Err(err).Msg("Seednote sidecar 不可用，种草笔记功能将不可用")
			seednoteClient = nil
		} else {
			hcCancel()
			log.Info().Str("base_url", cfg.Seednote.BaseURL).Msg("Seednote sidecar client initialized")
		}
	}

	// 10. Create WebSocket hub.
	wsHub := handler.NewWebSocketHub(jwtSvc)

	// 11. Create email service for verification codes.
	service.InitGeoCheck(log)
	var emailSvc *service.EmailService
	if rdb != nil {
		emailSvc = service.NewEmailService(&cfg.Email, rdb, log)
		log.Info().
			Bool("smtp_configured", cfg.Email.SMTPHost != "").
			Msg("email service initialized")
	}

	// 11.1 Create API key service (needed before executor for per-user MCP keys).
	var apiKeySvc *service.APIKeyService
	var sysKey string
	if repo != nil {
		apiKeySvc = service.NewAPIKeyService(repo, log)
		log.Info().
			Bool("api_key_svc_initialized", apiKeySvc != nil).
			Msg("API key service created")

		// Ensure a system default API key exists on startup.
		var err error
		sysKey, err = apiKeySvc.EnsureSystemKey(context.Background())
		if err != nil {
			log.Error().Err(err).Msg("CRITICAL: failed to ensure system API key — all MCP tool calls will fail with 401")
		} else {
			log.Info().Msg("system API key verified")
		}
	} else {
		log.Error().Msg("CRITICAL: repository is nil — API key service not created, all MCP authentication will fail. Check MySQL connectivity.")
	}

	// 12. Create agent executor.
	var agentExecutor agent.TaskExecutor
	switch cfg.Claude.Executor {
	case "docker":
		dockerExec, err := agent.NewDockerExecutor(log, &cfg.ImageAPI, cfg.Claude.Env, cfg.Claude.Docker, cfg.AgentServerURL(), cfg.Claude.Model, apiKeySvc, cfg.Claude.MaxTurns, store)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to create Docker executor")
		}
		agentExecutor = dockerExec
		dockerExec.CleanupOrphanedContainers()
		log.Info().
			Str("image", cfg.Claude.Docker.Image).
			Int64("cpu_cores", cfg.Claude.Docker.CPUCores).
			Int64("memory_mb", cfg.Claude.Docker.MemoryMB).
			Int("timeout_sec", cfg.Claude.Docker.TimeoutSec).
			Bool("per_user_mcp", apiKeySvc != nil).
			Msg("docker agent executor created")
	default:
		agentExecutor = agent.NewLocalExecutor(log, &cfg.ImageAPI, cfg.Claude.Env, cfg.Claude.PluginDir, cfg.Claude.Sandbox, cfg.Claude.Model, apiKeySvc, cfg.Claude.MaxTurns, cfg.Claude.Docker.WorkspaceDir, cfg.AgentServerURL(), store)
		log.Info().
			Str("plugin_dir", cfg.Claude.PluginDir).
			Bool("sandbox", cfg.Claude.Sandbox).
			Bool("per_user_mcp", apiKeySvc != nil).
			Msg("local agent executor created")
	}

	// 13. Create services.
	var planSvc *service.PlanService
	var taskSvc *service.TaskService
	var channelSvc *service.ChannelService
	var creditSvc *service.CreditService
	var feedbackSvc *service.FeedbackService
	var publishingSvc *service.PublishingService
	var seednoteTrackingSvc *service.SeednoteTrackingService
	var templateSvc *service.TemplateService
	var viralAnalysisSvc *service.ViralAnalysisService
	var posterSvc *service.PosterService
	var asynqClient *scheduler.AsynqClient
	workspaceSvc := service.NewWorkspaceService("", cfg.Claude.Docker.WorkspaceDir)

	if repo != nil {
		planSvc = service.NewPlanService(repo, log)
		channelSvc = service.NewChannelService(repo, log)
		creditSvc = service.NewCreditService(repo, &cfg.Credits, log)
		feedbackSvc = service.NewFeedbackService(repo, log)
		publishingSvc = service.NewPublishingService(repo, log)
		templateSvc = service.NewTemplateService(repo, log)
		posterSvc = service.NewPosterService(repo, log)

		// Create Asynq client if Redis is available.
		if rdb != nil {
			asynqClient = scheduler.NewAsynqClient(
				cfg.Redis.Addr,
				cfg.Redis.Password,
				cfg.Redis.DB,
			)
			log.Info().Msg("Asynq client initialized")
		}

		taskSvc = service.NewTaskService(repo, agentExecutor, asynqClient, store, creditSvc, log, cfg.Claude.TaskLogDir, workspaceSvc, cfg.Claude.Docker.WorkspaceDir, service.NewRedisPubSub(rdb, log), publishingSvc)
		if count, err := taskSvc.ClearArtifactTitles(context.Background()); err != nil {
			log.Warn().Err(err).Msg("failed to clear artifact task titles")
		} else if count > 0 {
			log.Info().Int64("count", count).Msg("cleared artifact task titles")
		}

		// Wire goal-mode evaluator: reuse the writing LLM client when available.
		// If writingLLMClient is nil at this point, we wire it later (see below).
	}

	// 12.1 Create per-user model config service.
	var modelConfigSvc *service.ModelConfigService
	if repo != nil {
		modelConfigSvc = service.NewModelConfigService(repo, cfg, log)
		log.Info().Msg("model config service initialized")
	}

	var writingLLMClient service.LLMClient
	if repo != nil {
		// Prefer config.yaml writing section; fall back to env vars.
		llmBaseURL := cfg.Writing.BaseURL
		llmAPIKey := cfg.Writing.Key
		llmModel := cfg.Writing.Model
		if llmBaseURL == "" {
			llmBaseURL = os.Getenv("ANTHROPIC_BASE_URL")
		}
		if llmAPIKey == "" {
			llmAPIKey = os.Getenv("ANTHROPIC_AUTH_TOKEN")
		}
		if llmModel == "" {
			llmModel = cfg.Claude.Model
			if llmModel == "" {
				llmModel = os.Getenv("ANTHROPIC_MODEL")
			}
		}
		if llmBaseURL != "" && llmAPIKey != "" && llmModel != "" {
			writingLLMClient = service.NewOpenAILLMClient(llmBaseURL, llmAPIKey, llmModel, cfg.Writing.Timeout)
			if strings.Contains(llmBaseURL, "/anthropic") {
				log.Warn().
					Str("base_url", llmBaseURL).
					Msg("writing.base_url contains '/anthropic' — the writing service uses the OpenAI SDK; ensure the endpoint supports /v1/chat/completions")
			}
			log.Info().Str("endpoint", llmBaseURL).Str("model", llmModel).Msg("writing LLM client initialized")
		}
	}

	// Initialize vision LLM client (falls back to writing config if not configured).
	var visionLLMClient service.LLMClient
	{
		vBaseURL := cfg.Vision.BaseURL
		vAPIKey := cfg.Vision.Key
		vModel := cfg.Vision.Model
		vTimeout := cfg.Vision.Timeout
		if vTimeout == 0 {
			vTimeout = 60 * time.Second
		}
		if vBaseURL != "" && vAPIKey != "" && vModel != "" {
			visionLLMClient = service.NewOpenAILLMClient(vBaseURL, vAPIKey, vModel, vTimeout)
			log.Info().Str("endpoint", vBaseURL).Str("model", vModel).Msg("vision LLM client initialized")
		}
	}

	if repo != nil && seednoteClient != nil {
		seednoteTrackingSvc = service.NewSeednoteTrackingService(repo, platform.NewSeednoteProvider(seednoteClient), writingLLMClient, asynqClient, log)
		log.Info().Bool("llm_configured", writingLLMClient != nil).Msg("SeedNote tracking service initialized")
		if taskSvc != nil {
			viralAnalysisSvc = service.NewViralAnalysisService(repo, platform.NewSeednoteProvider(seednoteClient), writingLLMClient, asynqClient, log)
			viralAnalysisSvc.SetCreditService(creditSvc)
			taskSvc.SetSeednoteTrackingService(seednoteTrackingSvc)
			log.Info().Bool("llm_configured", writingLLMClient != nil).Msg("Viral analysis service initialized")
		}
	}

	// Goal-mode configuration is purely a credit multiplier now — the actual
	// evaluation loop runs inside Claude Code's built-in /goal mechanism.
	if taskSvc != nil {
		log.Info().
			Int("multiplier", cfg.Credits.EffectiveGoalModeMultiplier()).
			Msg("goal mode configured (uses Claude Code native /goal)")
	}
	// 13.1 Create auth handler (after creditSvc so we can grant registration bonus).
	authHandler := handler.NewAuthHandler(jwtSvc, wechatSvc, &cfg.WeChat, repo, emailSvc, log, wsHub, cfg.Invitation.Enabled, cfg.Invitation.MaxPerUser, creditSvc, &cfg.Credits)

	// 14. Create handlers.
	var planHandler *handler.PlanHandler
	var taskHandler *handler.TaskHandler
	var seednoteAnalyticsHandler *handler.SeednoteAnalyticsHandler
	var agentHandler *handler.AgentHandler
	var channelHandler *handler.ChannelHandler
	var timelineHandler *handler.TimelineHandler
	var creditHandler *handler.CreditHandler
	var apiKeyHandler *handler.APIKeyHandler
	var fileHandler *handler.FileHandler
	var feedbackHandler *handler.FeedbackHandler
	var modelConfigHandler *handler.ModelConfigHandler
	var imageModelHandler *handler.ImageModelHandler
	var templateHandler *handler.TemplateHandler
	var viralAnalysisHandler *handler.ViralAnalysisHandler
	var posterHandler *handler.PosterHandler
	var resourceHandler *handler.ResourceHandler
	var topicPoolHandler *handler.TopicPoolHandler
	var topicPoolSvc *service.TopicPoolService
	var agentFeedbackSvc *service.AgentFeedbackService
	var designerSvc *service.DesignerService
	var designerHandler *handler.DesignerHandler

	if repo != nil {
		planHandler = handler.NewPlanHandler(planSvc, log)
		// Pass local dataDir so ServeLocalFile can serve files from disk.
		taskHandler = handler.NewTaskHandler(taskSvc, log, cfg.Storage.LocalDataDir)
		if seednoteTrackingSvc != nil {
			seednoteAnalyticsHandler = handler.NewSeednoteAnalyticsHandler(seednoteTrackingSvc, log)
		}
		channelHandler = handler.NewChannelHandler(channelSvc, log)
		if modelConfigSvc != nil {
			channelHandler.SetModelConfigService(modelConfigSvc)
		}
		if writingLLMClient != nil {
			channelHandler.SetLLMClient(writingLLMClient, cfg.Writing.Timeout)
		}
		if visionLLMClient != nil {
			channelHandler.SetVisionClient(visionLLMClient)
		}
		if templateSvc != nil {
			channelHandler.SetTemplateService(templateSvc)
		}
		if store != nil {
			channelHandler.SetStore(store)
		}
		if seednoteClient != nil {
			channelHandler.SetSeednoteClient(seednoteClient)
		}
		timelineHandler = handler.NewTimelineHandler(repo, log)
		if creditSvc != nil {
			creditHandler = handler.NewCreditHandler(creditSvc, &cfg.Credits, &cfg.ImageAPI, cfg.Credits.AdminAPIKey, log)
		}
		if apiKeySvc != nil {
			apiKeyHandler = handler.NewAPIKeyHandler(apiKeySvc, log)
		}
		agentHandler = handler.NewAgentHandler(taskSvc, apiKeySvc, store, cfg.MCP.APIKey, log)
		if store != nil {
			fileHandler = handler.NewFileHandler(store, log)
		}
		feedbackHandler = handler.NewFeedbackHandler(feedbackSvc, log)
		templateHandler = handler.NewTemplateHandler(templateSvc, log)
		if viralAnalysisSvc != nil {
			viralAnalysisHandler = handler.NewViralAnalysisHandler(viralAnalysisSvc, log)
		}
		posterHandler = handler.NewPosterHandler(posterSvc, log)
		topicPoolSvc = service.NewTopicPoolService(repo, log)
		topicPoolHandler = handler.NewTopicPoolHandler(topicPoolSvc, log)
		if taskSvc != nil {
			taskSvc.SetTopicPoolService(topicPoolSvc)
		}
		agentFeedbackSvc = service.NewAgentFeedbackService(repo, log)
	}
	resourceHandler = handler.NewResourceHandler(log)
	if modelConfigSvc != nil {
		modelConfigHandler = handler.NewModelConfigHandler(modelConfigSvc, log)
	}
	// Image model options handler (tier-gated listing). Always available so the
	// frontend can render the create-task/plan dropdown even without presets.
	imageModelHandler = handler.NewImageModelHandler(cfg.ImagePresets, repo, log)
	// Wire image presets + repo into task/plan handlers for tier-gated validation.
	if taskHandler != nil {
		taskHandler.SetImagePresets(cfg.ImagePresets)
		if repo != nil {
			taskHandler.SetRepository(repo)
		}
	}
	if planHandler != nil {
		planHandler.SetImagePresets(cfg.ImagePresets)
		if repo != nil {
			planHandler.SetRepository(repo)
		}
	}

	// 14.1. Create MCP handler (using official MCP Go SDK).
	var mcpHandler http.Handler
	if channelSvc != nil && taskSvc != nil && creditSvc != nil && planSvc != nil {
		// Create AI operation services for MCP tools.
		var imageSvc *service.ImageService
		var writingSvc *service.WritingService
		var liveSliceSvc *service.LiveSliceService

		if store != nil {
			imageSvc = service.NewImageService(&cfg.ImageAPI, store, repo, log)
			if modelConfigSvc != nil {
				imageSvc.SetModelConfigService(modelConfigSvc)
			}
		}
		if mysqlDB != nil && imageSvc != nil {
			designerSvc = service.NewDesignerService(mysqlDB, imageSvc, creditSvc, &cfg.ImageAPI, store, log)
			designerHandler = handler.NewDesignerHandler(designerSvc, log)
		}
		if repo != nil {
			if writingLLMClient != nil {
				writersDir := ""
				if cfg.Claude.PluginDir != "" {
					writersDir = filepath.Join(cfg.Claude.PluginDir, "writers")
				}
				writingSvc = service.NewWritingService(repo, writingLLMClient, writersDir, cfg.Writing.Timeout, cfg.Writing.ConvertTimeout, log)
				if visionLLMClient != nil {
					writingSvc.SetVisionClient(visionLLMClient)
				}
				if modelConfigSvc != nil {
					writingSvc.SetModelConfigService(modelConfigSvc)
				}
			} else {
				log.Warn().Msg("LLM client not configured (missing writing config or ANTHROPIC_BASE_URL/AUTH_TOKEN/MODEL), writing tools unavailable")
			}
		}
		if writingLLMClient != nil || cfg.TingWu.Complete() || store != nil {
			var err error
			liveSliceSvc, err = service.NewLiveSliceService(cfg.TingWu, writingLLMClient, store, log)
			if err != nil {
				log.Warn().Err(err).Msg("live-slice service unavailable")
			} else {
				log.Info().
					Bool("llm_configured", writingLLMClient != nil).
					Bool("tingwu_configured", cfg.TingWu.Complete()).
					Bool("storage_configured", store != nil).
					Msg("live-slice service initialized")
			}
		}

		mcp.SetServices(&mcp.Services{
			ChannelSvc:       channelSvc,
			TaskSvc:          taskSvc,
			CreditSvc:        creditSvc,
			PlanSvc:          planSvc,
			ImageSvc:         imageSvc,
			WritingSvc:       writingSvc,
			PublishingSvc:    publishingSvc,
			WorkspaceSvc:     workspaceSvc,
			TemplateSvc:      templateSvc,
			LiveSliceSvc:     liveSliceSvc,
			SeednoteClient:   seednoteClient,
			TopicPoolSvc:     topicPoolSvc,
			AgentFeedbackSvc: agentFeedbackSvc,
		})
		mcp.SetBillingServices(creditSvc, modelConfigSvc, cfg)
		mcp.SetLogger(log)
		mcpHandler = mcp.NewMCPHandler(apiKeySvc, cfg.MCP.APIKey, log)
		log.Info().
			Bool("mcp_static_key_set", cfg.MCP.APIKey != "").
			Bool("image_tools", imageSvc != nil).
			Bool("writing_tools", writingSvc != nil).
			Bool("live_slice_tools", liveSliceSvc != nil).
			Bool("publishing_tools", publishingSvc != nil).
			Msg("MCP handler initialized with tools (official SDK)")
	} else {
		mcpHandler = mcp.NewMCPHandler(apiKeySvc, cfg.MCP.APIKey, log)
		log.Info().Msg("MCP handler initialized (no tools, services unavailable)")
	}

	// 15. Start Asynq worker if Redis is available.
	var asynqServer *scheduler.TaskProcessor
	if rdb != nil && taskSvc != nil {
		asynqServer = startAsynqServer(repo, taskSvc, seednoteTrackingSvc, viralAnalysisSvc, cfg, log)
	}

	// 15.1 Start plan checker if repository and task service are available.
	if repo != nil && taskSvc != nil {
		schedulerCtx, schedulerCancel := context.WithCancel(context.Background())
		defer schedulerCancel()
		go scheduler.StartPlanChecker(schedulerCtx, repo, taskSvc, log, rdb)
	}

	// 15.2 Start periodic workspace cleanup (every hour).
	if repo != nil && taskSvc != nil {
		cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
		defer cleanupCancel()
		go startPeriodicCleanup(cleanupCtx, taskSvc, viralAnalysisSvc, posterSvc, log)
	}

	// 16. Build Services struct.
	svcs := &router.Services{
		Config:                   cfg,
		Logger:                   log,
		DB:                       mysqlDB,
		Redis:                    rdb,
		Repo:                     repo,
		JWTService:               jwtSvc,
		WechatSvc:                wechatSvc,
		WSHub:                    wsHub,
		AuthHandler:              authHandler,
		Executor:                 agentExecutor,
		PlanService:              planSvc,
		TaskService:              taskSvc,
		CreditService:            creditSvc,
		ChannelHandler:           channelHandler,
		PlanHandler:              planHandler,
		TaskHandler:              taskHandler,
		SeednoteAnalyticsHandler: seednoteAnalyticsHandler,
		AgentHandler:             agentHandler,
		CreditHandler:            creditHandler,
		TimelineHandler:          timelineHandler,
		APIKeyHandler:            apiKeyHandler,
		FileHandler:              fileHandler,
		FeedbackHandler:          feedbackHandler,
		ModelConfigHandler:       modelConfigHandler,
		ImageModelHandler:        imageModelHandler,
		TemplateHandler:          templateHandler,
		ViralAnalysisHandler:     viralAnalysisHandler,
		PosterHandler:            posterHandler,
		ResourceHandler:          resourceHandler,
		TopicPoolHandler:         topicPoolHandler,
		DesignerHandler:          designerHandler,
		MCPHandler:               mcpHandler,
		StorageProvider:          store,
	}

	// 17. Create router.
	app := router.NewRouter(svcs)

	// 17.1 Async MCP health check — verifies MCP endpoint is accessible with system key.
	if sysKey != "" {
		healthCtx, healthCancel := context.WithTimeout(context.Background(), 10*time.Second)
		go func() {
			defer healthCancel()
			mcp.CheckHealth(healthCtx, cfg.AgentServerURL(), sysKey, log)
		}()
	}

	// 18. Start HTTP server with graceful shutdown.
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)

	// Use signal.NotifyContext for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownDone := make(chan struct{})
	go func() {
		<-ctx.Done()
		log.Info().Msg("shutdown signal received, stopping server...")

		// 1. Cancel all running tasks so executors can wind down.
		if taskSvc != nil {
			cancelled := taskSvc.CancelAllRunning()
			if cancelled > 0 {
				log.Info().Int("cancelled_tasks", cancelled).Msg("cancelled running tasks")
			}
		}

		// 2. Shutdown HTTP server.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := app.ShutdownWithContext(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("server shutdown error")
		}

		// 3. Shutdown Asynq server.
		// Task contexts are already cancelled, so workers should exit promptly.
		if asynqServer != nil {
			asynqServer.Shutdown()
			log.Info().Msg("Asynq server stopped")
		}

		// Close Docker executor client if applicable.
		if closer, ok := agentExecutor.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				log.Error().Err(err).Msg("failed to close agent executor")
			}
		}

		// Close Asynq client.
		if asynqClient != nil {
			if err := asynqClient.Close(); err != nil {
				log.Error().Err(err).Msg("failed to close Asynq client")
			}
		}

		// Stop Redis pub/sub subscriber.
		if taskSvc != nil {
			taskSvc.Close()
		}

		if repo != nil {
			if err := repo.Close(); err != nil {
				log.Error().Err(err).Msg("failed to close repository")
			}
		}
		if rdb != nil {
			if err := rdb.Close(); err != nil {
				log.Error().Err(err).Msg("failed to close Redis")
			}
		}

		close(shutdownDone)
	}()

	log.Info().Str("addr", addr).Msg("server starting")
	if err := app.Listen(addr); err != nil {
		log.Error().Err(err).Msg("server listen error")
	}

	// Wait for graceful shutdown to complete, with a timeout guard.
	select {
	case <-shutdownDone:
		log.Info().Msg("graceful shutdown completed")
	case <-time.After(60 * time.Second):
		log.Warn().Msg("graceful shutdown timed out after 60s, forcing exit")
		os.Exit(1)
	}
}

// createTaskLogDir ensures the task log directory exists if configured.
func createTaskLogDir(dir string, log *zerolog.Logger) {
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Warn().Err(err).Str("dir", dir).Msg("failed to create task log directory")
	}
}

// resolveConfigPath returns the config file path. If explicit is non-empty, it is
// used directly. Otherwise the default paths are tried in order.
func resolveConfigPath(explicit string) string {
	if explicit != "" {
		return explicit
	}
	for _, p := range defaultConfigPaths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Return the first default even if it doesn't exist so NewConfig gives a clear error.
	return defaultConfigPaths[0]
}

// connectMySQL attempts to connect to MySQL. Returns nil on failure (logs error).
func connectMySQL(ctx context.Context, cfg *config.Config, log *zerolog.Logger) *gorm.DB {
	db, err := gorm.Open(mysql.Open(cfg.Database.DSN), &gorm.Config{})
	if err != nil {
		log.Error().Err(err).Msg("failed to connect to MySQL")
		return nil
	}

	sqlDB, err := db.DB()
	if err != nil {
		log.Error().Err(err).Msg("failed to get underlying sql.DB")
		return db
	}

	sqlDB.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(time.Duration(cfg.Database.ConnMaxLifetime) * time.Second)

	log.Info().Msg("MySQL connection established")
	return db
}

// connectRedis attempts to connect to Redis. Returns nil on failure (logs error).
func connectRedis(ctx context.Context, cfg *config.Config, log *zerolog.Logger) *redis.Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})

	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Error().Err(err).Msg("failed to connect to Redis")
		return nil
	}

	log.Info().Str("addr", cfg.Redis.Addr).Msg("Redis connection established")
	return rdb
}

// startAsynqServer starts the Asynq task processor in a background goroutine.
func startAsynqServer(repo repository.Repository, taskSvc *service.TaskService, seednoteTrackingSvc *service.SeednoteTrackingService, viralAnalysisSvc *service.ViralAnalysisService, cfg *config.Config, log *zerolog.Logger) *scheduler.TaskProcessor {
	var seednoteDiscoverHandler scheduler.SeednoteTrackingHandler
	var seednoteCaptureHandler scheduler.SeednoteTrackingHandler
	if seednoteTrackingSvc != nil {
		seednoteDiscoverHandler = func(ctx context.Context, trackingID string) error {
			return seednoteTrackingSvc.DiscoverPublishedNote(ctx, trackingID)
		}
		seednoteCaptureHandler = func(ctx context.Context, trackingID string) error {
			return seednoteTrackingSvc.CaptureMetrics(ctx, trackingID)
		}
	}

	var viralAnalysisHandler scheduler.ViralAnalysisHandler
	if viralAnalysisSvc != nil {
		viralAnalysisHandler = func(ctx context.Context, analysisID string) error {
			return viralAnalysisSvc.ExecuteAnalysis(ctx, analysisID)
		}
	}

	srv := scheduler.NewTaskProcessor(
		func(ctx context.Context, taskID, userID string) error {
			return taskSvc.HandleExecutionFromPayload(ctx, taskID, userID)
		},
		func(ctx context.Context, planID string) error {
			return scheduler.TriggerPlanNow(ctx, repo, taskSvc, planID, log)
		},
		func(ctx context.Context) error {
			return taskSvc.CleanupExpiredWorkspaces(ctx)
		},
		seednoteDiscoverHandler,
		seednoteCaptureHandler,
		viralAnalysisHandler,
		cfg.Redis.Addr,
		cfg.Redis.Password,
		cfg.Redis.DB,
		cfg.Asynq.Concurrency,
		log,
	)

	go func() {
		log.Info().Int("concurrency", cfg.Asynq.Concurrency).Msg("starting Asynq task processor")
		if err := srv.Start(); err != nil {
			log.Error().Err(err).Msg("Asynq server start error")
		}
	}()

	return srv
}

// parseLogLevel converts a LOG_LEVEL string to a zerolog level.
func parseLogLevel(level string) zerolog.Level {
	switch level {
	case "debug", "DEBUG":
		return zerolog.DebugLevel
	case "warn", "WARN", "warning", "WARNING":
		return zerolog.WarnLevel
	case "error", "ERROR":
		return zerolog.ErrorLevel
	case "trace", "TRACE":
		return zerolog.TraceLevel
	default:
		return zerolog.InfoLevel
	}
}

// startPeriodicCleanup runs CleanupExpiredWorkspaces on a ticker until ctx is cancelled.
func startPeriodicCleanup(ctx context.Context, taskSvc *service.TaskService, viralSvc *service.ViralAnalysisService, posterSvc *service.PosterService, log *zerolog.Logger) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	cleanup := func() {
		if err := taskSvc.CleanupExpiredWorkspaces(ctx); err != nil {
			log.Error().Err(err).Msg("periodic cleanup failed")
		}
		if viralSvc != nil {
			if err := viralSvc.CleanupOldCompleted(ctx); err != nil {
				log.Error().Err(err).Msg("viral analysis cleanup failed")
			}
		}
		if posterSvc != nil {
			if err := posterSvc.CleanupOldCompleted(ctx); err != nil {
				log.Error().Err(err).Msg("poster task cleanup failed")
			}
		}
	}

	// Run once at startup.
	cleanup()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("periodic cleanup stopped")
			return
		case <-ticker.C:
			cleanup()
		}
	}
}
