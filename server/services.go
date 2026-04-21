package main

import (
	"context"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/agent"
	"github.com/royalrick/anbanwriter/server/auth"
	"github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/handler"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/scheduler"
	"github.com/royalrick/anbanwriter/server/service"
	"github.com/royalrick/anbanwriter/server/storage"
)

// coreServices holds all service-layer instances created during startup.
type coreServices struct {
	jwtSvc        *auth.JWTService
	wechatSvc     *auth.WeChatService
	wsHub         *handler.WebSocketHub
	emailSvc      *service.EmailService
	apiKeySvc     *service.APIKeyService
	agentExecutor agent.TaskExecutor
	planSvc       *service.PlanService
	taskSvc       *service.TaskService
	channelSvc    *service.ChannelService
	creditSvc     *service.CreditService
	asynqClient   *scheduler.AsynqClient
	workspaceSvc  *service.WorkspaceService
	store         storage.Provider
}

// setupCoreServices creates all service-layer instances.
func setupCoreServices(
	cfg *config.Config,
	rdb *redis.Client,
	repo repository.Repository,
	store storage.Provider,
	log *zerolog.Logger,
) *coreServices {
	svcs := &coreServices{store: store}

	// 8. Create JWT service.
	jwtSvc, err := auth.NewJWTService(
		cfg.JWT.SecretKey,
		cfg.JWT.AccessExpiry,
		cfg.JWT.RefreshExpiry,
	)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create JWT service")
	}
	svcs.jwtSvc = jwtSvc

	// 9. Create WeChat service (nil if not configured).
	if cfg.WeChat.AppID != "" && cfg.WeChat.AppSecret != "" {
		svcs.wechatSvc = auth.NewWeChatService(cfg.WeChat.AppID, cfg.WeChat.AppSecret, log)
		log.Info().Msg("WeChat service initialized")
	}

	// 10. Create WebSocket hub.
	svcs.wsHub = handler.NewWebSocketHub(jwtSvc)

	// 11. Create email service for verification codes.
	if rdb != nil {
		svcs.emailSvc = service.NewEmailService(&cfg.Email, rdb, log)
		log.Info().
			Bool("smtp_configured", cfg.Email.SMTPHost != "").
			Msg("email service initialized")
	}

	// 11.1 Create API key service (needed before executor for per-user MCP keys).
	if repo != nil {
		svcs.apiKeySvc = service.NewAPIKeyService(repo, log)

		// Ensure a system default API key exists on startup.
		if _, err := svcs.apiKeySvc.EnsureSystemKey(context.Background()); err != nil {
			log.Warn().Err(err).Msg("failed to ensure system API key")
		}
	}

	// 12. Create agent executor.
	switch cfg.Claude.Executor {
	case "docker":
		dockerExec, err := agent.NewDockerExecutor(log, &cfg.ImageAPI, cfg.Claude.Env, cfg.Claude.Docker, cfg.Claude.Model, svcs.apiKeySvc, cfg.Claude.MaxTurns)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to create Docker executor")
		}
		svcs.agentExecutor = dockerExec
		dockerExec.CleanupOrphanedContainers()
		log.Info().
			Str("image", cfg.Claude.Docker.Image).
			Int64("cpu_cores", cfg.Claude.Docker.CPUCores).
			Int64("memory_mb", cfg.Claude.Docker.MemoryMB).
			Int("timeout_sec", cfg.Claude.Docker.TimeoutSec).
			Bool("per_user_mcp", svcs.apiKeySvc != nil).
			Msg("docker agent executor created")
	default:
		svcs.agentExecutor = agent.NewLocalExecutor(log, &cfg.ImageAPI, cfg.Claude.Env, cfg.Claude.PluginDir, cfg.Claude.Sandbox, cfg.Claude.Model, svcs.apiKeySvc, cfg.Claude.MaxTurns, cfg.Claude.Docker.WorkspaceDir)
		log.Info().
			Str("plugin_dir", cfg.Claude.PluginDir).
			Bool("sandbox", cfg.Claude.Sandbox).
			Bool("per_user_mcp", svcs.apiKeySvc != nil).
			Msg("local agent executor created")
	}

	// 13. Create services.
	svcs.workspaceSvc = service.NewWorkspaceService("", cfg.Claude.Docker.WorkspaceDir, log)

	if repo != nil {
		svcs.planSvc = service.NewPlanService(repo, log)
		svcs.channelSvc = service.NewChannelService(repo, log)
		svcs.creditSvc = service.NewCreditService(repo, &cfg.Credits, log)

		// Create Asynq client if Redis is available.
		if rdb != nil {
			svcs.asynqClient = scheduler.NewAsynqClient(
				cfg.Redis.Addr,
				cfg.Redis.Password,
				cfg.Redis.DB,
			)
			log.Info().Msg("Asynq client initialized")
		}

		// Set up task progress notifier. Use Redis pub/sub if available,
		// otherwise fall back to database polling.
		var notifier service.TaskProgressNotifier
		if rdb != nil {
			notifier = service.NewRedisNotifier(rdb, log)
			log.Info().Msg("task progress notifier: redis pub/sub")
		} else {
			notifier = service.NewPollingNotifier(repo.Tasks(), log)
			log.Info().Msg("task progress notifier: database polling (redis unavailable)")
		}
		svcs.taskSvc = service.NewTaskService(repo, svcs.agentExecutor, svcs.asynqClient, store, svcs.creditSvc, log, cfg.Claude.TaskLogDir, svcs.workspaceSvc, cfg.Claude.Docker.WorkspaceDir, service.WithNotifier(notifier))
	}

	return svcs
}
