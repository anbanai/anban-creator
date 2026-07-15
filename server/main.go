package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/auth"
	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/handler"
	"github.com/anbanai/anban-creator/server/mcp"
	projectmemory "github.com/anbanai/anban-creator/server/memory"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/platform"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/router"
	"github.com/anbanai/anban-creator/server/scheduler"
	"github.com/anbanai/anban-creator/server/seednote"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
	"github.com/anbanai/anban-creator/server/wcf"
)

// defaultConfigPaths lists config file locations to try when -config is not set.
var defaultConfigPaths = []string{"./config.yaml", "./server/config.yaml"}

func main() {
	// 1. Parse flags.
	configPath := flag.String("config", "", "path to server config file (default: ./config.yaml or ./server/config.yaml)")
	port := flag.Int("port", 0, "server listen port (overrides config.yaml server.port)")
	flag.Parse()

	// Resolve config path: explicit flag > env > default search.
	cfgFile := resolveConfigPath(*configPath)

	// 2. Load config.
	cfg, err := config.NewConfig(cfgFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	// -port flag overrides config.yaml server.port. (Per-environment port
	// control, if ever needed, is via ${ANBAN_PORT:-8080} written explicitly in
	// config.yaml — there is no hidden ANBAN_PORT override.)
	if *port > 0 {
		cfg.Server.Port = *port
	}

	// 3. Init logger (JSON to stderr, level from config.yaml logging.level).
	logLevelStr := cfg.Logging.Level
	if logLevelStr == "" {
		logLevelStr = "info"
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
		if err := migrateModels(mysqlDB, model.AutoMigrate); err != nil {
			log.Fatal().Err(err).Msg("failed to auto-migrate models")
		} else {
			log.Info().Msg("database migration completed")
		}

		// 6.1 One-time backfill: split the overloaded article Project.Style into
		// the new orthogonal dimensions (Style=visual / WritingStyle=writer). Old
		// article rows stored the writer key in Style; move it to WritingStyle and
		// clear Style so the writer key is no longer read as a visual-style anchor.
		if err := service.MigrateArticleStyleOverload(context.Background(), mysqlDB, log); err != nil {
			log.Error().Err(err).Msg("failed to backfill article project style overload")
		}

		// 6.2 One-time rename: the `channel` (WeChat account) concept became
		// `project`. AutoMigrate cannot rename tables/columns, so this reconciles
		// the legacy `channels` table + `channel_id` foreign keys into `projects`
		// / `project_id` while preserving all data. Idempotent; no-op on fresh or
		// already-migrated databases.
		if err := service.MigrateChannelsToProjects(context.Background(), mysqlDB, log); err != nil {
			log.Error().Err(err).Msg("failed to migrate channels to projects")
		}

		if err := service.MigrateProjectPositioningToInstructions(context.Background(), mysqlDB, log); err != nil {
			log.Error().Err(err).Msg("failed to migrate project positioning to instructions")
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

	// 9.1 Create Seednote (种草笔记) sidecar client. The client is wired
	// immediately; readiness is tracked asynchronously so optional sidecars never
	// block the core HTTP server from binding its port.
	seednoteClient := seednote.NewClient(cfg.Seednote.BaseURL, time.Duration(cfg.Seednote.Timeout)*time.Second)
	seednoteMonitor := service.NewSidecarMonitor(service.SidecarMonitorConfig{
		Name:        "seednote",
		HealthCheck: seednoteClient.HealthCheck,
	}, log)
	log.Info().Str("base_url", cfg.Seednote.BaseURL).Msg("Seednote sidecar client configured")

	// 9.2 Create ilink transport client. wcflink remains the underlying HTTP
	// sidecar, while ilink is the platform channel name.
	var wcfClient *wcf.Client
	var ilinkMonitor *service.SidecarMonitor
	if cfg.Ilink.Enabled {
		wcfClient = wcf.NewClient(cfg.Ilink.BaseURL, time.Duration(cfg.Ilink.Timeout)*time.Second)
		ilinkMonitor = service.NewSidecarMonitor(service.SidecarMonitorConfig{
			Name:        "ilink",
			HealthCheck: wcfClient.HealthCheck,
		}, log)
		log.Info().Str("base_url", cfg.Ilink.BaseURL).Msg("ilink transport client configured")
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

	var memoryMgr *projectmemory.ProjectMemoryManager
	if cfg.Claude.Executor != "kubernetes" && cfg.Memory.Enabled && store != nil {
		memoryMgr = projectmemory.NewProjectMemoryManager(store, cfg.Memory, service.NewRedisMemoryLocker(rdb, log), *log)
		log.Info().
			Str("provider", cfg.Memory.Provider).
			Str("oss_prefix", cfg.Memory.OSSPrefix).
			Str("runtime_dir", cfg.Memory.RuntimeDir).
			Msg("project memory manager initialized")
	}

	// 12. Create agent executor.
	var agentExecutor agent.TaskExecutor
	var kubeClient kubernetes.Interface
	var kubeDispatcher agent.KubernetesDispatcher
	var kubeVerifier *agent.KubernetesWorkloadVerifier
	var executionTokens *auth.ExecutionTokenService
	var bootstrapSvc *service.AgentBootstrapService
	var kubeReconciler *agent.KubernetesReconciler
	switch cfg.Claude.Executor {
	case "docker":
		dockerExec, err := agent.NewDockerExecutor(log, &cfg.ImageAPI, cfg.Claude.Env, cfg.Claude.Docker, cfg.AgentServerURL(), cfg.Claude.Model, apiKeySvc, cfg.Claude.MaxTurns, store, memoryMgr)
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
	case "kubernetes":
		restConfig, err := rest.InClusterConfig()
		if err != nil {
			log.Fatal().Err(err).Msg("failed to load Kubernetes in-cluster config")
		}
		kubeClient, err = kubernetes.NewForConfig(restConfig)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to create Kubernetes client")
		}
		log.Info().
			Str("namespace", cfg.Claude.Kubernetes.Namespace).
			Str("image", cfg.Claude.Kubernetes.AgentImage).
			Msg("Kubernetes Job runtime client created")
	default:
		agentExecutor = agent.NewLocalExecutor(log, &cfg.ImageAPI, cfg.Claude.Env, cfg.Claude.PluginDir, cfg.Claude.Sandbox, cfg.Claude.Model, apiKeySvc, cfg.Claude.MaxTurns, cfg.Claude.Docker.WorkspaceDir, cfg.AgentServerURL(), store, memoryMgr)
		log.Info().
			Str("plugin_dir", cfg.Claude.PluginDir).
			Bool("sandbox", cfg.Claude.Sandbox).
			Bool("per_user_mcp", apiKeySvc != nil).
			Msg("local agent executor created")
	}

	// 13. Create services.
	var planSvc *service.PlanService
	var taskSvc *service.TaskService
	var projectSvc *service.ProjectService
	var creditSvc *service.CreditService
	var feedbackSvc *service.FeedbackService
	var publishingSvc *service.PublishingService
	var seednoteTrackingSvc *service.SeednoteTrackingService
	var templateSvc *service.TemplateService
	var viralAnalysisSvc *service.ViralAnalysisService
	var posterSvc *service.PosterService
	var asynqClient *scheduler.AsynqClient
	workspaceSvc := service.NewWorkspaceService("", cfg.Claude.Docker.WorkspaceDir)
	videoCatalog := service.VideoModelCatalogFromConfig(cfg.VideoAPI.ModelCatalog)
	videoCreditMultiplier := cfg.Billing.CreditsPerCNY
	if videoCreditMultiplier <= 0 {
		videoCreditMultiplier = cfg.VideoAPI.CreditMultiplierOrDefault()
	}

	if repo != nil {
		planSvc = service.NewPlanService(repo, log)
		planSvc.SetVideoCatalogAndCreditMultiplier(videoCatalog, videoCreditMultiplier)
		planSvc.SetVideoBillingConfig(cfg.Billing)
		projectSvc = service.NewProjectService(repo, log)
		projectSvc.SetVideoCatalog(videoCatalog)
		creditSvc = service.NewCreditService(repo, &cfg.Credits, log)
		creditSvc.SetFullConfig(cfg)
		planSvc.SetCreditService(creditSvc)
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
				cfg.Asynq.ContentGenerateTimeout,
			)
			log.Info().Msg("Asynq client initialized")
		}

		taskSvc = service.NewTaskService(repo, agentExecutor, asynqClient, store, creditSvc, log, cfg.Claude.TaskLogDir, workspaceSvc, cfg.Claude.Docker.WorkspaceDir, service.NewRedisPubSub(rdb, log), publishingSvc)
		taskSvc.SetProjectMemoryManager(memoryMgr)
		taskSvc.SetVideoCatalogAndCreditMultiplier(videoCatalog, videoCreditMultiplier)
		taskSvc.SetVideoBillingConfig(cfg.Billing)
		taskSvc.SetMontageConfig(cfg.Montage)
		taskSvc.SetExecutionTimeouts(cfg.Asynq.ContentGenerateTimeout, cfg.Asynq.PersistTimeout)
		// Wire executor defaults so local-executor claim responses carry the same
		// model + max-turns the cloud DockerExecutor uses (desktop-built argv parity).
		taskSvc.SetExecutorDefaults(cfg.Claude.Model, cfg.Claude.MaxTurns)
		if cfg.Claude.Executor == "kubernetes" {
			var err error
			executionTokens, err = auth.NewExecutionTokenService(cfg.Claude.Kubernetes.ExecutionTokenSecret)
			if err != nil {
				log.Fatal().Err(err).Msg("failed to create Kubernetes execution token service")
			}
			kubeDispatcher, err = agent.NewKubernetesDispatcherWithClient(cfg.Claude.Kubernetes, cfg.AgentServerURL(), kubeClient)
			if err != nil {
				log.Fatal().Err(err).Msg("failed to create Kubernetes Job dispatcher")
			}
			kubeVerifier, err = agent.NewKubernetesWorkloadVerifier(kubeClient, cfg.Claude.Kubernetes.Namespace, cfg.Claude.Kubernetes.ServiceAccount)
			if err != nil {
				log.Fatal().Err(err).Msg("failed to create Kubernetes workload verifier")
			}
			activeDeadline := time.Duration(cfg.Claude.Kubernetes.ActiveDeadlineSeconds) * time.Second
			bootstrapSvc = service.NewAgentBootstrapService(repo, executionTokens, service.AgentBootstrapConfig{
				Model:                   cfg.Claude.Model,
				MaxTurns:                cfg.Claude.MaxTurns,
				TokenTTL:                activeDeadline,
				ActiveDeadline:          activeDeadline,
				SignedURLTTL:            cfg.Storage.DirectUploadExpiresSeconds,
				Store:                   store,
				ImageAPIConfig:          &cfg.ImageAPI,
				MontageToolPolicy:       cfg.Montage.ToolPolicy,
				MontagePipelineDefaults: cfg.Montage.PipelineDefaults,
				RuntimeEnv:              cfg.Claude.Env,
			}, *log)
			taskSvc.SetKubernetesDispatcher(kubeDispatcher)
			workspaceLifecycle, ok := kubeDispatcher.(service.TaskWorkspaceLifecycle)
			if !ok {
				log.Fatal().Msg("Kubernetes dispatcher does not manage task workspaces")
			}
			taskSvc.SetTaskWorkspaceLifecycle(workspaceLifecycle)
			projectSvc.SetProjectMemoryLifecycle(kubeDispatcher)
			kubeReconciler = agent.NewKubernetesReconciler(kubeDispatcher, taskSvc, agent.KubernetesReconcilerConfig{
				PreStartRetryLimit: cfg.Claude.Kubernetes.PreStartRetryLimit,
				HeartbeatTimeout:   time.Duration(cfg.Claude.Kubernetes.HeartbeatTimeoutSeconds) * time.Second,
			}, log)
			taskSvc.SetNASResumeEnabled(true)
			taskSvc.SetProjectConcurrencyCap(1)
			log.Info().Msg("Kubernetes Job runtime enabled: project task concurrency capped at 1 per memory PVC")
		}
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
		// Writing LLM comes from model_routes.writing.
		llmBaseURL := cfg.Writing.BaseURL
		llmAPIKey := cfg.Writing.Key
		llmModel := cfg.Writing.Model
		if llmModel == "" {
			llmModel = cfg.Claude.Model
		}
		if llmBaseURL != "" && llmAPIKey != "" && llmModel != "" {
			writingLLMClient = service.NewOpenAILLMClient(llmBaseURL, llmAPIKey, llmModel, cfg.Writing.Timeout)
			if strings.Contains(llmBaseURL, "/anthropic") {
				log.Warn().
					Str("base_url", llmBaseURL).
					Msg("model_routes.writing base_url contains '/anthropic' — the writing service uses the OpenAI SDK; ensure the endpoint supports /v1/chat/completions")
			}
			log.Info().Str("endpoint", llmBaseURL).Str("model", llmModel).Msg("writing LLM client initialized")
		}
	}

	var imageUnderstandingClient service.LLMClient
	if cfg.ImageUnderstanding.BaseURL != "" && cfg.ImageUnderstanding.Key != "" && cfg.ImageUnderstanding.Model != "" {
		imageUnderstandingClient = service.NewOpenAILLMClient(cfg.ImageUnderstanding.BaseURL, cfg.ImageUnderstanding.Key, cfg.ImageUnderstanding.Model, cfg.ImageUnderstanding.Timeout)
		log.Info().Str("endpoint", cfg.ImageUnderstanding.BaseURL).Str("model", cfg.ImageUnderstanding.Model).Msg("image understanding LLM client initialized")
	}
	var videoUnderstandingClient service.LLMClient
	if cfg.VideoUnderstanding.BaseURL != "" && cfg.VideoUnderstanding.Key != "" && cfg.VideoUnderstanding.Model != "" {
		videoUnderstandingClient = service.NewOpenAILLMClient(cfg.VideoUnderstanding.BaseURL, cfg.VideoUnderstanding.Key, cfg.VideoUnderstanding.Model, cfg.VideoUnderstanding.Timeout)
		log.Info().Str("endpoint", cfg.VideoUnderstanding.BaseURL).Str("model", cfg.VideoUnderstanding.Model).Msg("video understanding LLM client initialized")
	}

	var aiEntrySvc *service.AIEntryService
	if repo != nil && taskSvc != nil {
		aiEntrySvc = service.NewAIEntryService(repo, taskSvc, writingLLMClient, log)
		if modelConfigSvc != nil {
			aiEntrySvc.SetModelConfigService(modelConfigSvc, cfg.Writing.Timeout)
		}
		log.Info().Bool("llm_configured", writingLLMClient != nil).Msg("AI entry service initialized")
	}

	if repo != nil {
		seednoteTrackingSvc = service.NewSeednoteTrackingService(repo, platform.NewSeednoteProvider(seednoteClient), writingLLMClient, asynqClient, log)
		log.Info().Bool("llm_configured", writingLLMClient != nil).Msg("SeedNote tracking service initialized")
		if taskSvc != nil {
			viralAnalysisSvc = service.NewViralAnalysisService(repo, platform.NewSeednoteProvider(seednoteClient), writingLLMClient, asynqClient, log)
			viralAnalysisSvc.SetCreditService(creditSvc)
			taskSvc.SetSeednoteTrackingService(seednoteTrackingSvc)
			log.Info().Bool("llm_configured", writingLLMClient != nil).Msg("Viral analysis service initialized")
		}
	}

	// 12.2 Wire ilink: platform WeChat assistant binding, inbound gateway,
	// natural-language conversation, and reliable terminal notification outbox.
	var ilinkBindingSvc *service.IlinkBindingService
	var ilinkPoller *service.IlinkPoller
	var ilinkWorker *service.IlinkNotificationWorker
	if repo != nil {
		assistantAccount := &service.IlinkAssistantAccount{
			AccountID:   cfg.Ilink.AssistantAccountID,
			DisplayName: cfg.Ilink.AssistantName,
			WechatID:    cfg.Ilink.AssistantWechatID,
			QRCodeURL:   cfg.Ilink.AssistantQRCodeURL,
		}
		ilinkBindingSvc = service.NewIlinkBindingService(repo, cfg.Ilink.Enabled, assistantAccount, log)
		if wcfClient != nil {
			ilinkNotifier := service.NewIlinkNotifier(repo, true, log)
			if taskSvc != nil {
				taskSvc.SetIlinkNotifier(ilinkNotifier)
			}
			conversation := service.NewIlinkConversationService(taskSvc, wcfClient, log)
			conversation.SetAIEntryService(aiEntrySvc)
			gateway := service.NewIlinkGateway(repo, ilinkBindingSvc, conversation, wcfClient, log)
			ilinkPoller = service.NewIlinkPoller(wcfClient, gateway, rdb, time.Duration(cfg.Ilink.PollInterval)*time.Second, log)
			ilinkPoller.SetReadiness(ilinkMonitor)
			ilinkWorker = service.NewIlinkNotificationWorker(repo, wcfClient, cfg.Ilink.NotificationRetryMax, 2*time.Second, log)
			ilinkWorker.SetReadiness(ilinkMonitor)
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
	authHandler := handler.NewAuthHandler(jwtSvc, wechatSvc, &cfg.WeChat, repo, emailSvc, log, wsHub, cfg.Invitation.Enabled, cfg.Invitation.MaxPerUser, creditSvc, &cfg.Credits, rdb)

	// 14. Create handlers.
	var planHandler *handler.PlanHandler
	var taskHandler *handler.TaskHandler
	var seednoteAnalyticsHandler *handler.SeednoteAnalyticsHandler
	var agentHandler *handler.AgentHandler
	var projectHandler *handler.ProjectHandler
	var timelineHandler *handler.TimelineHandler
	var creditHandler *handler.CreditHandler
	var videoHandler *handler.VideoHandler
	var apiKeyHandler *handler.APIKeyHandler
	var fileHandler *handler.FileHandler
	var uploadHandler *handler.UploadHandler
	var aiEntryHandler *handler.AIEntryHandler
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
	var ilinkHandler *handler.IlinkHandler

	if repo != nil {
		planHandler = handler.NewPlanHandler(planSvc, log)
		if store != nil {
			planHandler.SetStore(store)
		}
		// Pass local dataDir so ServeLocalFile can serve files from disk.
		taskHandler = handler.NewTaskHandler(taskSvc, log, cfg.Storage.LocalDataDir)
		seednoteAnalyticsHandler = buildSeednoteAnalyticsHandler(repo, log)
		if ilinkBindingSvc != nil {
			ilinkHandler = handler.NewIlinkHandler(ilinkBindingSvc, log)
		}
		projectHandler = handler.NewProjectHandler(projectSvc, log)
		if modelConfigSvc != nil {
			projectHandler.SetModelConfigService(modelConfigSvc)
		}
		if writingLLMClient != nil {
			projectHandler.SetLLMClient(writingLLMClient, cfg.Writing.Timeout)
		}
		if imageUnderstandingClient != nil {
			projectHandler.SetVisionClient(imageUnderstandingClient)
		}
		if templateSvc != nil {
			projectHandler.SetTemplateService(templateSvc)
		}
		if store != nil {
			projectHandler.SetStore(store)
			projectHandler.SetPendingUploadRepository(repo.PendingUploads())
		}
		projectHandler.SetSeednoteClient(seednoteClient)
		projectHandler.SetSeednoteReadiness(seednoteMonitor)
		timelineHandler = handler.NewTimelineHandler(repo, log)
		if creditSvc != nil {
			creditHandler = handler.NewCreditHandler(creditSvc, cfg, cfg.Credits.AdminAPIKey, log)
		}
		videoHandler = handler.NewVideoHandler(repo, creditSvc, videoCatalog, videoCreditMultiplier, log)
		videoHandler.SetBillingConfig(cfg.Billing)
		if apiKeySvc != nil {
			apiKeyHandler = handler.NewAPIKeyHandler(apiKeySvc, log)
		}
		agentHandler = handler.NewAgentHandler(taskSvc, apiKeySvc, store, cfg.MCP.APIKey, log)
		agentHandler.SetAdminAPIKey(cfg.Credits.AdminAPIKey)
		if executionTokens != nil {
			agentHandler.SetExecutionTokenService(executionTokens)
			agentHandler.SetBootstrap(kubeVerifier, bootstrapSvc)
		}
		agentHandler.SetDirectUploadConfig(service.DirectUploadConfig{
			Storage: cfg.Storage,
		})
		if store != nil {
			fileHandler = handler.NewFileHandler(store, log)
			fileHandler.SetPendingUploadRepository(repo.PendingUploads())
			uploadHandler = handler.NewUploadHandler(store, repo.PendingUploads(), service.DirectUploadConfig{
				Storage: cfg.Storage,
			}, log)
		}
		if aiEntrySvc != nil {
			aiEntryHandler = handler.NewAIEntryHandler(aiEntrySvc, repo.PendingUploads(), log)
		}
		feedbackHandler = handler.NewFeedbackHandler(feedbackSvc, log)
		templateHandler = handler.NewTemplateHandler(templateSvc, log)
		if store != nil {
			templateHandler.SetStore(store)
		}
		if repo != nil {
			templateHandler.SetPendingUploadRepository(repo.PendingUploads())
		}
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
	if projectSvc != nil && taskSvc != nil && creditSvc != nil && planSvc != nil {
		// Create AI operation services for MCP tools.
		var imageSvc *service.ImageService
		var videoSvc *service.VideoService
		var audioASRSvc *service.AudioASRService
		var writingSvc *service.WritingService
		var liveSliceSvc *service.LiveSliceService

		if store != nil {
			imageSvc = service.NewImageService(&cfg.ImageAPI, store, repo, log)
			if modelConfigSvc != nil {
				imageSvc.SetModelConfigService(modelConfigSvc)
			}
		}
		if cfg.VideoAPI.Key != "" {
			videoSvc = service.NewVideoService(&cfg.VideoAPI)
		} else {
			log.Warn().Msg("video generation service not configured (set model_routes.video_generation provider/model_catalog), video tools unavailable")
		}
		if cfg.FunASR.Complete() {
			var err error
			audioASRSvc, err = service.NewAudioASRService(cfg.FunASR, store, log)
			if err != nil {
				log.Warn().Err(err).Msg("audio ASR service unavailable")
			} else {
				log.Info().
					Bool("funasr_configured", cfg.FunASR.Complete()).
					Bool("storage_configured", store != nil).
					Msg("audio ASR service initialized")
			}
		}
		if mysqlDB != nil && imageSvc != nil {
			designerSvc = service.NewDesignerService(mysqlDB, imageSvc, creditSvc, cfg, store, log)
			designerHandler = handler.NewDesignerHandler(designerSvc, log)
		}
		if repo != nil {
			if writingLLMClient != nil || imageUnderstandingClient != nil || videoUnderstandingClient != nil {
				writersDir := ""
				if cfg.Claude.PluginDir != "" {
					writersDir = filepath.Join(cfg.Claude.PluginDir, "writers")
				}
				writingSvc = service.NewWritingService(repo, writingLLMClient, writersDir, cfg.Writing.Timeout, log)
				if imageUnderstandingClient != nil {
					writingSvc.SetImageUnderstandingClient(imageUnderstandingClient)
				}
				if videoUnderstandingClient != nil {
					writingSvc.SetVideoUnderstandingClient(videoUnderstandingClient)
				}
				if modelConfigSvc != nil {
					writingSvc.SetModelConfigService(modelConfigSvc)
				}
				if writingLLMClient == nil {
					log.Warn().Msg("writing LLM client not configured; understanding-only MCP tools remain available when image/video understanding routes are configured")
				}
			} else {
				log.Warn().Msg("LLM client not configured (set model_routes.writing provider/model), writing tools unavailable")
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
			ProjectSvc:            projectSvc,
			Store:                 store,
			TaskSvc:               taskSvc,
			CreditSvc:             creditSvc,
			PlanSvc:               planSvc,
			ImageSvc:              imageSvc,
			ImageModelResolver:    modelConfigSvc,
			ImageGenerator:        imageSvc,
			ImageGenerationBiller: mcp.NewImageGenerationBiller(),
			VideoSvc:              videoSvc,
			AudioASRSvc:           audioASRSvc,
			WritingSvc:            writingSvc,
			PublishingSvc:         publishingSvc,
			WorkspaceSvc:          workspaceSvc,
			TemplateSvc:           templateSvc,
			LiveSliceSvc:          liveSliceSvc,
			SeednoteClient:        seednoteClient,
			SeednoteReadiness:     seednoteMonitor,
			TopicPoolSvc:          topicPoolSvc,
			AgentFeedbackSvc:      agentFeedbackSvc,
			TingWuConfigured:      cfg.TingWu.Complete(),
			FunASRConfigured:      cfg.FunASR.Complete(),
		})
		mcp.SetBillingServices(creditSvc, modelConfigSvc, cfg)
		mcp.SetLogger(log)
		mcpHandler = mcp.NewMCPHandler(apiKeySvc, cfg.MCP.APIKey, log, mcp.WithExecutionAuthentication(executionTokens, taskSvc))
		log.Info().
			Bool("mcp_static_key_set", cfg.MCP.APIKey != "").
			Bool("image_tools", imageSvc != nil).
			Bool("video_tools", videoSvc != nil).
			Bool("writing_tools", writingSvc != nil).
			Bool("live_slice_tools", liveSliceSvc != nil).
			Bool("publishing_tools", publishingSvc != nil).
			Msg("MCP handler initialized with tools (official SDK)")
	} else {
		mcpHandler = mcp.NewMCPHandler(apiKeySvc, cfg.MCP.APIKey, log, mcp.WithExecutionAuthentication(executionTokens, taskSvc))
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
	if kubeReconciler != nil {
		reconcilerCtx, reconcilerCancel := context.WithCancel(context.Background())
		defer reconcilerCancel()
		go kubeReconciler.Run(reconcilerCtx)
	}

	// 15.2 Clean up expirable derived records without touching NAS task workspaces.
	if repo != nil {
		cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
		defer cleanupCancel()
		go startPeriodicArtifactCleanup(cleanupCtx, viralAnalysisSvc, posterSvc, log)
	}

	// 15.3 Start the local-claim fallback worker (every 10s). Flips
	// pending local-target tasks past their claim deadline back to cloud
	// execution so they're never stuck when no desktop is online.
	if repo != nil && taskSvc != nil {
		reclaimCtx, reclaimCancel := context.WithCancel(context.Background())
		defer reclaimCancel()
		go startLocalClaimFallback(reclaimCtx, taskSvc, log)
	}
	if repo != nil && store != nil && store.Name() == "oss" {
		uploadCleanupCtx, uploadCleanupCancel := context.WithCancel(context.Background())
		defer uploadCleanupCancel()
		service.StartPendingUploadCleanup(uploadCleanupCtx, store, repo.PendingUploads(), 30*time.Minute, log)
	}

	// 15.4 Start ilink inbound poller and terminal notification worker.
	if ilinkPoller != nil {
		ilinkPollerCtx, ilinkPollerCancel := context.WithCancel(context.Background())
		defer ilinkPollerCancel()
		go ilinkPoller.Run(ilinkPollerCtx)
	}
	if ilinkWorker != nil {
		ilinkWorkerCtx, ilinkWorkerCancel := context.WithCancel(context.Background())
		defer ilinkWorkerCancel()
		go ilinkWorker.Run(ilinkWorkerCtx)
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
		ProjectHandler:           projectHandler,
		PlanHandler:              planHandler,
		TaskHandler:              taskHandler,
		SeednoteAnalyticsHandler: seednoteAnalyticsHandler,
		AgentHandler:             agentHandler,
		CreditHandler:            creditHandler,
		VideoHandler:             videoHandler,
		TimelineHandler:          timelineHandler,
		APIKeyHandler:            apiKeyHandler,
		FileHandler:              fileHandler,
		UploadHandler:            uploadHandler,
		AIEntryHandler:           aiEntryHandler,
		FeedbackHandler:          feedbackHandler,
		ModelConfigHandler:       modelConfigHandler,
		ImageModelHandler:        imageModelHandler,
		TemplateHandler:          templateHandler,
		ViralAnalysisHandler:     viralAnalysisHandler,
		PosterHandler:            posterHandler,
		ResourceHandler:          resourceHandler,
		TopicPoolHandler:         topicPoolHandler,
		DesignerHandler:          designerHandler,
		IlinkHandler:             ilinkHandler,
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

	go seednoteMonitor.Run(ctx)
	if ilinkMonitor != nil {
		go ilinkMonitor.Run(ctx)
	}

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

		// 1b. Signal the WebSocket hub to stop and close every live client
		// connection. Best-effort with no completion barrier: run() drains its
		// quit signal asynchronously and closeAllClients unblocks each serve()/ping
		// loop as its conn closes. Run before HTTP shutdown so the hub and ping
		// loops tear down rather than leak to process exit — Fiber force-closes
		// the sockets regardless, but without the quit signal run() never exits.
		wsHub.Shutdown()

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

	listenConfig := fiber.ListenConfig{}
	if cfg.Server.TLSCertFile != "" || cfg.Server.TLSKeyFile != "" {
		if cfg.Server.TLSCertFile == "" || cfg.Server.TLSKeyFile == "" {
			log.Fatal().Msg("server TLS requires both certificate and key files")
		}
		listenConfig.CertFile = cfg.Server.TLSCertFile
		listenConfig.CertKeyFile = cfg.Server.TLSKeyFile
	}
	log.Info().Str("addr", addr).Bool("tls", listenConfig.CertFile != "").Msg("server starting")
	if err := app.Listen(addr, listenConfig); err != nil {
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

func migrateModels(db *gorm.DB, migrate func(*gorm.DB) error) error {
	if db == nil {
		return nil
	}
	return migrate(db)
}

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

func buildSeednoteAnalyticsHandler(repo repository.Repository, log *zerolog.Logger) *handler.SeednoteAnalyticsHandler {
	if repo == nil {
		return nil
	}
	trackingSvc := service.NewSeednoteTrackingService(repo, nil, nil, nil, log)
	return handler.NewSeednoteAnalyticsHandler(trackingSvc, log)
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

// startPeriodicArtifactCleanup removes expirable derived records without touching NAS task workspaces.
func startPeriodicArtifactCleanup(ctx context.Context, viralSvc *service.ViralAnalysisService, posterSvc *service.PosterService, log *zerolog.Logger) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	cleanup := func() {
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

// startLocalClaimFallback periodically (every ~10s) flips pending local-target
// tasks past their claim deadline back to cloud execution, so tasks aren't
// stuck when no desktop executor is online. Paired with LocalClaimWindow (30s):
// the desktop polls /api/v1/agent/claim every couple of seconds, so under normal
// operation a task is claimed long before this fallback fires.
func startLocalClaimFallback(ctx context.Context, taskSvc *service.TaskService, log *zerolog.Logger) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("local claim fallback stopped")
			return
		case <-ticker.C:
			n, err := taskSvc.ReclaimExpiredLocalTasks(ctx)
			if err != nil {
				log.Warn().Err(err).Msg("local claim fallback failed")
				continue
			}
			if n > 0 {
				log.Info().Int("reclaimed", n).Msg("reclaimed expired local tasks to cloud execution")
			}
		}
	}
}
