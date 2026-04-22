package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
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
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/router"
	"github.com/royalrick/anbanwriter/server/scheduler"
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

	// 10. Create WebSocket hub.
	wsHub := handler.NewWebSocketHub(jwtSvc)

	// 11. Create email service for verification codes.
	var emailSvc *service.EmailService
	if rdb != nil {
		emailSvc = service.NewEmailService(&cfg.Email, rdb, log)
		log.Info().
			Bool("smtp_configured", cfg.Email.SMTPHost != "").
			Msg("email service initialized")
	}

	// 11.1 Create auth handler.
	var authHandler *handler.AuthHandler
	if repo != nil {
		authHandler = handler.NewAuthHandler(jwtSvc, wechatSvc, repo, emailSvc, log, wsHub, cfg.Invitation.Enabled, cfg.Invitation.MaxPerUser)
	}

	// 11.1 Create API key service (needed before executor for per-user MCP keys).
	var apiKeySvc *service.APIKeyService
	if repo != nil {
		apiKeySvc = service.NewAPIKeyService(repo, log)

		// Ensure a system default API key exists on startup.
		if _, err := apiKeySvc.EnsureSystemKey(context.Background()); err != nil {
			log.Warn().Err(err).Msg("failed to ensure system API key")
		}
	}

	// 12. Create agent executor.
	var agentExecutor agent.TaskExecutor
	switch cfg.Claude.Executor {
	case "docker":
		dockerExec, err := agent.NewDockerExecutor(log, &cfg.ImageAPI, cfg.Claude.Env, cfg.Claude.Docker, cfg.Claude.Model, apiKeySvc, cfg.Claude.MaxTurns)
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
		agentExecutor = agent.NewLocalExecutor(log, &cfg.ImageAPI, cfg.Claude.Env, cfg.Claude.PluginDir, cfg.Claude.Sandbox, cfg.Claude.Model, apiKeySvc, cfg.Claude.MaxTurns, cfg.Claude.Docker.WorkspaceDir)
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
	var asynqClient *scheduler.AsynqClient
	workspaceSvc := service.NewWorkspaceService("", cfg.Claude.Docker.WorkspaceDir, log)

	if repo != nil {
		planSvc = service.NewPlanService(repo, log)
		channelSvc = service.NewChannelService(repo, log)
		creditSvc = service.NewCreditService(repo, &cfg.Credits, log)
		feedbackSvc = service.NewFeedbackService(repo, log)

		// Create Asynq client if Redis is available.
		if rdb != nil {
			asynqClient = scheduler.NewAsynqClient(
				cfg.Redis.Addr,
				cfg.Redis.Password,
				cfg.Redis.DB,
			)
			log.Info().Msg("Asynq client initialized")
		}

		taskSvc = service.NewTaskService(repo, agentExecutor, asynqClient, store, creditSvc, log, cfg.Claude.TaskLogDir, workspaceSvc, cfg.Claude.Docker.WorkspaceDir, service.NewRedisPubSub(rdb, log))
	}

	// 14. Create handlers.
	var planHandler *handler.PlanHandler
	var taskHandler *handler.TaskHandler
	var agentHandler *handler.AgentHandler
	var channelHandler *handler.ChannelHandler
	var timelineHandler *handler.TimelineHandler
	var creditHandler *handler.CreditHandler
	var apiKeyHandler *handler.APIKeyHandler
	var fileHandler *handler.FileHandler
	var feedbackHandler *handler.FeedbackHandler

	if repo != nil {
		planHandler = handler.NewPlanHandler(planSvc, log)
		// Pass local dataDir so ServeLocalFile can serve files from disk.
		taskHandler = handler.NewTaskHandler(taskSvc, log, cfg.Storage.LocalDataDir)
		channelHandler = handler.NewChannelHandler(channelSvc, log)
		timelineHandler = handler.NewTimelineHandler(repo, log)
		if creditSvc != nil {
			creditHandler = handler.NewCreditHandler(creditSvc, cfg.Credits.AdminAPIKey, log)
		}
		if apiKeySvc != nil {
			apiKeyHandler = handler.NewAPIKeyHandler(apiKeySvc, log)
		}
		agentHandler = handler.NewAgentHandler(taskSvc, apiKeySvc, store, cfg.MCP.APIKey, log)
		if store != nil {
			fileHandler = handler.NewFileHandler(store, log)
		}
		feedbackHandler = handler.NewFeedbackHandler(feedbackSvc, log)
	}

	// 14.1. Create MCP handler (using official MCP Go SDK).
	var mcpHandler http.Handler
	if channelSvc != nil && taskSvc != nil && creditSvc != nil && planSvc != nil {
		// Create AI operation services for MCP tools.
		var imageSvc *service.ImageService
		var writingSvc *service.WritingService
		var publishingSvc *service.PublishingService

		if store != nil {
			imageSvc = service.NewImageService(&cfg.ImageAPI, store, repo, creditSvc, cfg.Claude.Docker.MCPBaseURL, cfg.Claude.Docker.WorkspaceDir, log)
		}
		if repo != nil && creditSvc != nil {
			// Create LLM client for writing operations.
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
				llmClient := service.NewOpenAILLMClient(llmBaseURL, llmAPIKey, llmModel)
				writingSvc = service.NewWritingService(repo, creditSvc, llmClient, log)
			} else {
				log.Warn().Msg("LLM client not configured (missing writing config or ANTHROPIC_BASE_URL/AUTH_TOKEN/MODEL), writing tools unavailable")
			}
			publishingSvc = service.NewPublishingService(repo, creditSvc, log)
		}

		mcp.SetServices(&mcp.Services{
			ChannelSvc:    channelSvc,
			TaskSvc:       taskSvc,
			CreditSvc:     creditSvc,
			PlanSvc:       planSvc,
			ImageSvc:      imageSvc,
			WritingSvc:    writingSvc,
			PublishingSvc: publishingSvc,
			WorkspaceSvc:  workspaceSvc,
		})
		mcpHandler = mcp.NewMCPHandler(apiKeySvc, cfg.MCP.APIKey, log)
		log.Info().
			Bool("mcp_static_key_set", cfg.MCP.APIKey != "").
			Bool("image_tools", imageSvc != nil).
			Bool("writing_tools", writingSvc != nil).
			Bool("publishing_tools", publishingSvc != nil).
			Msg("MCP handler initialized with tools (official SDK)")
	} else {
		mcpHandler = mcp.NewMCPHandler(apiKeySvc, cfg.MCP.APIKey, log)
		log.Info().Msg("MCP handler initialized (no tools, services unavailable)")
	}

	// 15. Start Asynq worker if Redis is available.
	var asynqServer *scheduler.TaskProcessor
	if rdb != nil && taskSvc != nil {
		asynqServer = startAsynqServer(taskSvc, cfg, log)
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
		go startPeriodicCleanup(cleanupCtx, taskSvc, log)
	}

	// 16. Build Services struct.
	svcs := &router.Services{
		Config:          cfg,
		Logger:          log,
		DB:              mysqlDB,
		Redis:           rdb,
		Repo:            repo,
		JWTService:      jwtSvc,
		WechatSvc:       wechatSvc,
		WSHub:           wsHub,
		AuthHandler:     authHandler,
		Executor:        agentExecutor,
		PlanService:     planSvc,
		TaskService:     taskSvc,
		CreditService:   creditSvc,
		ChannelHandler:  channelHandler,
		PlanHandler:     planHandler,
		TaskHandler:     taskHandler,
		AgentHandler:    agentHandler,
		CreditHandler:   creditHandler,
		TimelineHandler: timelineHandler,
		APIKeyHandler:   apiKeyHandler,
		FileHandler:     fileHandler,
		FeedbackHandler: feedbackHandler,
		MCPHandler:      mcpHandler,
		StorageProvider: store,
	}

	// 17. Create router.
	app := router.NewRouter(svcs)

	// 18. Start HTTP server with graceful shutdown.
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)

	// Use signal.NotifyContext for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		log.Info().Msg("shutdown signal received, stopping server...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := app.ShutdownWithContext(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("server shutdown error")
		}

		// Stop Asynq server.
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
	}()

	log.Info().Str("addr", addr).Msg("server starting")
	if err := app.Listen(addr); err != nil {
		log.Error().Err(err).Msg("server listen error")
	}

	log.Info().Msg("server exited")
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
func startAsynqServer(taskSvc *service.TaskService, cfg *config.Config, log *zerolog.Logger) *scheduler.TaskProcessor {
	srv := scheduler.NewTaskProcessor(
		func(ctx context.Context, taskID, userID string) error {
			return taskSvc.HandleExecutionFromPayload(ctx, taskID, userID)
		},
		func(ctx context.Context, planID string) error {
			log.Info().Str("plan_id", planID).Msg("plan trigger: placeholder")
			return nil
		},
		func(ctx context.Context) error {
			return taskSvc.CleanupExpiredWorkspaces(ctx)
		},
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
func startPeriodicCleanup(ctx context.Context, taskSvc *service.TaskService, log *zerolog.Logger) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	// Run once at startup.
	if err := taskSvc.CleanupExpiredWorkspaces(ctx); err != nil {
		log.Error().Err(err).Msg("initial workspace cleanup failed")
	}

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("periodic cleanup stopped")
			return
		case <-ticker.C:
			if err := taskSvc.CleanupExpiredWorkspaces(ctx); err != nil {
				log.Error().Err(err).Msg("periodic workspace cleanup failed")
			}
		}
	}
}
