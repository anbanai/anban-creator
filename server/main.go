package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	dockerclient "github.com/docker/docker/client"
	"github.com/gofiber/fiber/v3"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/auth"
	serverbilling "github.com/anbanai/anban-creator/server/billing"
	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/handler"
	"github.com/anbanai/anban-creator/server/mcp"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/platform"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/resources"
	"github.com/anbanai/anban-creator/server/router"
	"github.com/anbanai/anban-creator/server/scheduler"
	"github.com/anbanai/anban-creator/server/seednote"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
	"github.com/anbanai/anban-creator/server/wcf"
	"github.com/anbanai/anban-creator/server/worldtree"
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

	billingBundle, err := serverbilling.LoadBundle(cfg.BillingRuntime.ConfigDir)
	if err != nil {
		log.Fatal().Err(err).Msg("load billing bundle")
	}
	agentProfiles, err := service.NewAgentProfileRegistryFromConfig(cfg.Claude.ExecutionProfiles, billingBundle.Costs)
	if err != nil {
		log.Fatal().Err(err).Msg("invalid agent execution profile configuration")
	}

	// 4. Connect MySQL.
	mysqlDB := connectMySQL(context.Background(), cfg, log)

	// 5. Connect Redis.
	rdb := connectRedis(context.Background(), cfg, log)

	// 6. Auto-migrate models.
	if mysqlDB != nil {
		if err := requireAgentExecutionProfileSchema(mysqlDB); err != nil {
			log.Fatal().Err(err).Msg("database schema is not ready for Agent execution profiles")
		}
		if _, err := service.MigrateImageCapabilities(context.Background(), mysqlDB, cfg.ModelRoutes.ImageGeneration.DefaultCapability, log); err != nil {
			log.Fatal().Err(err).Msg("failed to migrate image capabilities")
		}
		if err := migrateModels(mysqlDB, model.AutoMigrate); err != nil {
			log.Fatal().Err(err).Msg("failed to auto-migrate models")
		} else {
			log.Info().Msg("database migration completed")
		}
		if err := service.MigrateGoalModeRemoval(context.Background(), mysqlDB, log); err != nil {
			log.Fatal().Err(err).Msg("failed to remove goal mode schema")
		}
		if err := service.MigrateDesignerRemoval(context.Background(), mysqlDB, log); err != nil {
			log.Fatal().Err(err).Msg("failed to remove Designer schema")
		}
		if err := service.MigrateTemplatePrompt(context.Background(), mysqlDB, log); err != nil {
			log.Fatal().Err(err).Msg("failed to migrate canonical template prompts")
		}
		if err := service.MigratePlanReferenceAttachments(context.Background(), mysqlDB, log); err != nil {
			log.Fatal().Err(err).Msg("failed to migrate plan reference attachments")
		}
		if _, err := service.MigrateTaskExecutionContracts(context.Background(), mysqlDB, log); err != nil {
			log.Fatal().Err(err).Msg("failed to migrate task execution contracts")
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
	fixedBilling, err := buildBillingRuntime(context.Background(), mysqlDB, repo, billingBundle, cfg, log)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialize fixed-SKU billing runtime")
	}
	if fixedBilling != nil && fixedBilling.Catalog != nil {
		fixedBilling.Catalog.SetAgentProfileRegistry(agentProfiles)
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
	worldtreeClient := worldtree.NewClient(cfg.Worldtree.BaseURL, cfg.Worldtree.Key, time.Duration(cfg.Worldtree.Timeout)*time.Second)
	log.Info().Str("base_url", cfg.Worldtree.BaseURL).Bool("configured", worldtreeClient.Configured()).Msg("WorldTree Channels analytics client configured")

	// 9.2 Create the iLink transport client. The iLink sidecar is the underlying
	// HTTP transport, while ilink is the platform channel name.
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

	// 12. Create the selected managed runtime provider.
	var kubeClient kubernetes.Interface
	var runtimeDispatcher agent.RuntimeDispatcher
	var workloadVerifier agent.WorkloadVerifier
	var runtimeClientCloser interface{ Close() error }
	executionTokens, err := auth.NewExecutionTokenService(cfg.Claude.ExecutionTokenSecret)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create execution token service")
	}
	workloadTokens, err := auth.NewWorkloadTokenService(cfg.Claude.ExecutionTokenSecret)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create workload token service")
	}
	var bootstrapSvc *service.AgentBootstrapService
	var runtimeReconciler *agent.RuntimeReconciler
	switch cfg.Claude.Executor {
	case "docker":
		dockerClient, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
		if err != nil {
			log.Fatal().Err(err).Msg("failed to create Docker client")
		}
		runtimeClientCloser = dockerClient
		runtimeDispatcher, err = agent.NewDockerDispatcher(cfg.Claude.RuntimeImages, cfg.Claude.Docker, cfg.AgentServerURL(), workloadTokens, dockerClient, time.Now)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to create Docker runtime dispatcher")
		}
		workloadVerifier, err = agent.NewDockerWorkloadVerifier(dockerClient, workloadTokens)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to create Docker workload verifier")
		}
		log.Info().
			Str("article_image", cfg.Claude.RuntimeImages.ForTask(model.PlatformArticle).Image).
			Int64("cpu_cores", cfg.Claude.Docker.CPUCores).
			Int64("memory_mb", cfg.Claude.Docker.MemoryMB).
			Int("timeout_sec", cfg.Claude.Docker.TimeoutSec).
			Msg("Docker one-shot runtime dispatcher created")
	case "kubernetes":
		restConfig, err := rest.InClusterConfig()
		if err != nil {
			log.Fatal().Err(err).Msg("failed to load Kubernetes in-cluster config")
		}
		kubeClient, err = kubernetes.NewForConfig(restConfig)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to create Kubernetes client")
		}
		runtimeDispatcher, err = agent.NewKubernetesDispatcherWithClient(cfg.Claude.Kubernetes, cfg.Claude.RuntimeImages, cfg.AgentServerURL(), kubeClient)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to create Kubernetes Job dispatcher")
		}
		workloadVerifier, err = agent.NewKubernetesWorkloadVerifier(kubeClient, cfg.Claude.Kubernetes.Namespace, cfg.Claude.Kubernetes.ServiceAccount)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to create Kubernetes workload verifier")
		}
		log.Info().
			Str("namespace", cfg.Claude.Kubernetes.Namespace).
			Str("article_image", cfg.Claude.RuntimeImages.ForTask(model.PlatformArticle).Image).
			Msg("Kubernetes Job runtime client created")
	default:
		log.Fatal().Str("executor", cfg.Claude.Executor).Msg("unsupported Claude executor")
	}
	reconcilerConfig := managedRuntimeReconcilerConfig(cfg.Claude.Executor, cfg.Claude.Docker, cfg.Claude.Kubernetes)
	activeDeadline := reconcilerConfig.ActiveDeadline
	defaultImageAPI, _ := cfg.ImageAPIForCapability("")
	bootstrapSvc = service.NewAgentBootstrapService(repo, executionTokens, service.AgentBootstrapConfig{
		MaxTurns:                cfg.Claude.MaxTurns,
		TokenTTL:                activeDeadline,
		ActiveDeadline:          activeDeadline,
		SignedURLTTL:            cfg.Storage.DirectUploadExpiresSeconds,
		Store:                   store,
		ImageAPIConfig:          defaultImageAPI,
		MontageToolPolicy:       cfg.Montage.ToolPolicy,
		MontagePipelineDefaults: cfg.Montage.PipelineDefaults,
		MontageEnv:              cfg.Montage.Env,
		Registry:                agentProfiles,
	}, *log)

	// 13. Create services.
	var planSvc *service.PlanService
	var taskSvc *service.TaskService
	var projectSvc *service.ProjectService
	var feedbackSvc *service.FeedbackService
	var publishingSvc *service.PublishingService
	var wechatPublicationSvc *service.WechatPublicationService
	var seednoteTrackingSvc *service.SeednoteTrackingService
	var wechatTrackingSvc *service.WechatTrackingService
	var channelsTrackingSvc *service.ChannelsTrackingService
	var templateSvc *service.TemplateService
	var viralAnalysisHistorySvc *service.ViralAnalysisHistoryService
	var posterSvc *service.PosterService
	var referenceAssetSvc *service.ReferenceAssetService
	var asynqClient *scheduler.AsynqClient
	if repo != nil {
		planSvc = service.NewPlanService(repo, log)
		projectSvc = service.NewProjectService(repo, log)
		feedbackSvc = service.NewFeedbackService(repo, log)
		publishingSvc = service.NewPublishingService(repo, log)
		wechatPublicationSvc = service.NewWechatPublicationService(repo, nil, log)
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
		wechatPublicationSvc.SetEnqueuer(asynqClient)

		taskSvc = service.NewTaskService(repo, asynqClient, store, log, cfg.Claude.TaskLogDir, service.NewRedisPubSub(rdb, log), publishingSvc)
		referenceAssetSvc = service.NewReferenceAssetService(repo, store, time.Now)
		planSvc.SetReferenceAssetService(referenceAssetSvc)
		planSvc.SetAgentProfileRegistry(agentProfiles)
		taskSvc.SetReferenceAssetService(referenceAssetSvc)
		taskSvc.SetAgentProfileRegistry(agentProfiles)
		taskSvc.SetProviderCostService(fixedBilling.Cost)
		taskSvc.SetBillingWalletService(fixedBilling.Wallet)
		taskSvc.SetBillingCatalogService(fixedBilling.Catalog)
		planSvc.SetBillingWalletService(fixedBilling.Wallet)
		taskSvc.SetMontageConfig(cfg.Montage)
		taskSvc.SetExecutionTimeouts(cfg.Asynq.ContentGenerateTimeout, cfg.Asynq.PersistTimeout)
		taskSvc.SetExecutorMaxTurns(cfg.Claude.MaxTurns)
		taskSvc.SetRuntimeDispatcher(runtimeDispatcher)
		workspaceLifecycle, ok := runtimeDispatcher.(service.TaskWorkspaceLifecycle)
		if !ok {
			log.Fatal().Str("provider", cfg.Claude.Executor).Msg("runtime dispatcher does not manage task workspaces")
		}
		taskSvc.SetTaskWorkspaceLifecycle(workspaceLifecycle)
		projectMemoryLifecycle, ok := runtimeDispatcher.(service.ProjectMemoryLifecycle)
		if !ok {
			log.Fatal().Str("provider", cfg.Claude.Executor).Msg("runtime dispatcher does not manage project memory")
		}
		projectSvc.SetProjectMemoryLifecycle(projectMemoryLifecycle)
		runtimeReconciler = agent.NewRuntimeReconciler(runtimeDispatcher, taskSvc, reconcilerConfig, *log)
		taskSvc.SetNASResumeEnabled(true)
		if cap := managedRuntimeProjectConcurrencyCap(cfg.Claude.Executor); cap > 0 {
			taskSvc.SetProjectConcurrencyCap(cap)
			log.Info().
				Str("executor", cfg.Claude.Executor).
				Int("cap", cap).
				Msg("managed runtime project concurrency capped for shared project memory")
		}
		if count, err := taskSvc.ClearArtifactTitles(context.Background()); err != nil {
			log.Warn().Err(err).Msg("failed to clear artifact task titles")
		} else if count > 0 {
			log.Info().Int64("count", count).Msg("cleared artifact task titles")
		}

	}

	imageCapabilityResolver := service.NewImageCapabilityResolver(repo, cfg)
	if taskSvc != nil {
		taskSvc.SetImageCapabilityResolver(imageCapabilityResolver)
	}

	var serverInternalLLMClient service.ResultLLMClient
	if route := cfg.ServerInternal; route.BaseURL != "" && route.Key != "" && route.Model != "" {
		client := service.NewOpenAILLMClient(route.BaseURL, route.Key, route.Model, route.Timeout)
		serverInternalLLMClient, _ = client.(service.ResultLLMClient)
		log.Info().Str("endpoint", route.BaseURL).Str("server_internal_model", route.Model).Msg("server internal model client initialized")
	}

	var imageUnderstandingBaseClient service.LLMClient
	var imageUnderstandingClient service.ImageUnderstandingClient
	if cfg.ImageUnderstanding.BaseURL != "" && cfg.ImageUnderstanding.Key != "" && cfg.ImageUnderstanding.Model != "" {
		imageUnderstandingBaseClient = service.NewOpenAILLMClient(cfg.ImageUnderstanding.BaseURL, cfg.ImageUnderstanding.Key, cfg.ImageUnderstanding.Model, cfg.ImageUnderstanding.Timeout)
		imageUnderstandingClient, _ = imageUnderstandingBaseClient.(service.ImageUnderstandingClient)
		log.Info().Str("endpoint", cfg.ImageUnderstanding.BaseURL).Str("model", cfg.ImageUnderstanding.Model).Msg("image understanding LLM client initialized")
	}
	var videoUnderstandingClient service.LLMClient
	if cfg.VideoUnderstanding.BaseURL != "" && cfg.VideoUnderstanding.Key != "" && cfg.VideoUnderstanding.Model != "" {
		videoUnderstandingClient = service.NewOpenAILLMClient(cfg.VideoUnderstanding.BaseURL, cfg.VideoUnderstanding.Key, cfg.VideoUnderstanding.Model, cfg.VideoUnderstanding.Timeout)
		log.Info().Str("endpoint", cfg.VideoUnderstanding.BaseURL).Str("model", cfg.VideoUnderstanding.Model).Msg("video understanding LLM client initialized")
	}
	var aiEntrySvc *service.AIEntryService
	if repo != nil && taskSvc != nil {
		aiEntrySvc = service.NewAIEntryService(repo, taskSvc, serverInternalLLMClient, fixedBilling.Cost, service.AIEntryModelConfig{
			ProviderKey: cfg.ServerInternal.ProviderKey,
			Model:       cfg.ServerInternal.Model,
		}, log)
		aiEntrySvc.SetReferenceAssetService(referenceAssetSvc)
		log.Info().Bool("llm_configured", serverInternalLLMClient != nil).Str("server_internal_model", cfg.ServerInternal.Model).Msg("AI entry service initialized")
	}

	if repo != nil {
		seednoteTrackingSvc = service.NewSeednoteTrackingService(repo, platform.NewSeednoteProvider(seednoteClient), asynqClient, log)
		wechatTrackingSvc = service.NewWechatTrackingService(repo, platform.NewWechatOfficialAnalyticsProvider(log), asynqClient, log)
		channelsTrackingSvc = service.NewChannelsTrackingService(repo, worldtreeClient, asynqClient, log)
		log.Info().Msg("SeedNote tracking service initialized")
		log.Info().Msg("WeChat article tracking service initialized")
		log.Info().Msg("WeChat Channels tracking service initialized")
		if taskSvc != nil {
			viralAnalysisHistorySvc = service.NewViralAnalysisHistoryService(repo)
			log.Info().Msg("Viral analysis history service initialized")
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

	authHandler := handler.NewAuthHandler(jwtSvc, wechatSvc, &cfg.WeChat, repo, emailSvc, log, wsHub, cfg.Invitation.Enabled, cfg.Invitation.MaxPerUser, rdb)

	// 14. Create handlers.
	var planHandler *handler.PlanHandler
	var taskHandler *handler.TaskHandler
	var seednoteAnalyticsHandler *handler.SeednoteAnalyticsHandler
	var seednoteImportHandler *handler.SeednoteImportHandler
	var wechatAnalyticsHandler *handler.WechatAnalyticsHandler
	var wechatPublicationHandler *handler.WechatPublicationHandler
	var channelsAnalyticsHandler *handler.ChannelsAnalyticsHandler
	var agentHandler *handler.AgentHandler
	var agentProfileHandler *handler.AgentProfileHandler
	agentPackHandler := handler.NewAgentPackHandler()
	var projectHandler *handler.ProjectHandler
	var timelineHandler *handler.TimelineHandler
	var apiKeyHandler *handler.APIKeyHandler
	var fileHandler *handler.FileHandler
	var uploadHandler *handler.UploadHandler
	var aiEntryHandler *handler.AIEntryHandler
	var feedbackHandler *handler.FeedbackHandler
	var imageCapabilityHandler *handler.ImageCapabilityHandler
	var templateHandler *handler.TemplateHandler
	var viralAnalysisHandler *handler.ViralAnalysisHandler
	var posterHandler *handler.PosterHandler
	var resourceHandler *handler.ResourceHandler
	var topicPoolHandler *handler.TopicPoolHandler
	var topicPoolSvc *service.TopicPoolService
	var agentFeedbackSvc *service.AgentFeedbackService
	var ilinkHandler *handler.IlinkHandler

	if repo != nil {
		planHandler = handler.NewPlanHandler(planSvc, log)
		planHandler.SetScheduleRecommendationService(service.NewScheduleRecommendationService(repo, billingBundle.Products.CatalogID, billingBundle.Products.TaskTimePricing, log))
		planHandler.SetReferenceAssetService(referenceAssetSvc)
		if store != nil {
			planHandler.SetStore(store)
		}
		// Pass local dataDir so ServeLocalFile can serve files from disk.
		taskHandler = handler.NewTaskHandler(taskSvc, log, cfg.Storage.LocalDataDir)
		taskHandler.SetReferenceAssetService(referenceAssetSvc)
		if store != nil {
			taskHandler.SetStore(store)
		}
		seednoteAnalyticsHandler = handler.NewSeednoteAnalyticsHandler(seednoteTrackingSvc, log)
		seednoteImportHandler = handler.NewSeednoteImportHandler(service.NewSeednoteImportService(repo, store), log)
		wechatAnalyticsHandler = handler.NewWechatAnalyticsHandler(wechatTrackingSvc, log)
		wechatPublicationHandler = handler.NewWechatPublicationHandler(wechatPublicationSvc, log)
		channelsAnalyticsHandler = handler.NewChannelsAnalyticsHandler(channelsTrackingSvc, log)
		if ilinkBindingSvc != nil {
			ilinkHandler = handler.NewIlinkHandler(ilinkBindingSvc, log)
		}
		projectHandler = handler.NewProjectHandler(projectSvc, log)
		projectHandler.SetReferenceAssetService(referenceAssetSvc)
		projectHandler.SetUploadRepository(repo)
		projectHandler.SetImageCapabilities(cfg.ModelRoutes.ImageGeneration)
		if imageUnderstandingBaseClient != nil {
			projectHandler.SetVisionClient(imageUnderstandingBaseClient)
		}
		if templateSvc != nil {
			projectHandler.SetTemplateService(templateSvc)
		}
		if store != nil {
			projectHandler.SetStore(store)
		}
		projectHandler.SetSeednoteClient(seednoteClient)
		projectHandler.SetSeednoteReadiness(seednoteMonitor)
		timelineHandler = handler.NewTimelineHandler(repo, log)
		if apiKeySvc != nil {
			apiKeyHandler = handler.NewAPIKeyHandler(apiKeySvc, log)
		}
		agentHandler = handler.NewAgentHandler(taskSvc, apiKeySvc, store, cfg.MCP.APIKey, log)
		agentProfileHandler = handler.NewAgentProfileHandler(repo, agentProfiles, log)
		agentHandler.SetAdminAPIKey(cfg.BillingRuntime.AdminAPIKey)
		agentHandler.SetExecutionTokenService(executionTokens)
		agentHandler.SetBootstrap(workloadVerifier, bootstrapSvc)
		agentHandler.SetDirectUploadConfig(service.DirectUploadConfig{
			Storage: cfg.Storage,
		})
		if store != nil {
			fileHandler = handler.NewFileHandler(store, log)
			fileHandler.SetUploadSessionRepository(repo.UploadSessions())
			uploadHandler = handler.NewUploadHandler(store, repo, service.DirectUploadConfig{
				Storage: cfg.Storage,
			}, log)
			uploadHandler.SetRepository(repo)
		}
		if aiEntrySvc != nil {
			aiEntryHandler = handler.NewAIEntryHandler(aiEntrySvc, repo, store, log)
		}
		feedbackHandler = handler.NewFeedbackHandler(feedbackSvc, log)
		templateHandler = handler.NewTemplateHandler(templateSvc, log)
		templateHandler.SetUploadRepository(repo)
		if imageUnderstandingBaseClient != nil {
			templateHandler.SetVisionClient(imageUnderstandingBaseClient)
		}
		if store != nil {
			templateHandler.SetStore(store)
		}
		if viralAnalysisHistorySvc != nil {
			viralAnalysisHandler = handler.NewViralAnalysisHandler(viralAnalysisHistorySvc, log)
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
	// Public image capability catalog. Provider and model routing stay server-side.
	var imageCatalog *service.BillingCatalogService
	if fixedBilling != nil {
		imageCatalog = fixedBilling.Catalog
	}
	imageCapabilityHandler = handler.NewImageCapabilityHandler(cfg.ModelRoutes.ImageGeneration, repo, imageCatalog, log)
	// Wire capability routing + repo into task/plan handlers for tier-gated validation.
	if taskHandler != nil {
		taskHandler.SetImageCapabilities(cfg.ModelRoutes.ImageGeneration)
		if repo != nil {
			taskHandler.SetRepository(repo)
		}
	}
	if planHandler != nil {
		planHandler.SetImageCapabilities(cfg.ModelRoutes.ImageGeneration)
		if repo != nil {
			planHandler.SetRepository(repo)
		}
	}

	// 14.1. Create MCP handler (using official MCP Go SDK).
	var mcpHandler http.Handler
	if projectSvc != nil && taskSvc != nil && planSvc != nil {
		// Create AI operation services for MCP tools.
		var imageSvc *service.ImageService
		var contentRenderSvc *service.ContentRenderService
		var liveSliceSvc *service.LiveSliceService

		if store != nil {
			imageSvc = service.NewImageService(defaultImageAPI, store, repo, log)
			imageSvc.SetImageCapabilityResolver(imageCapabilityResolver)
		}
		if imageSvc != nil {
			imageSvc.SetProviderCostService(fixedBilling.Cost)
		}
		if repo != nil {
			contentRenderSvc = service.NewContentRenderService(repo, log)
		}
		if cfg.TingWu.Complete() || store != nil {
			var err error
			liveSliceSvc, err = service.NewLiveSliceService(cfg.TingWu, store, log)
			if err != nil {
				log.Warn().Err(err).Msg("live-slice service unavailable")
			} else {
				log.Info().
					Bool("llm_configured", false).
					Bool("tingwu_configured", cfg.TingWu.Complete()).
					Bool("storage_configured", store != nil).
					Msg("live-slice service initialized")
			}
		}

		mcp.SetServices(&mcp.Services{
			ProjectSvc:             projectSvc,
			TaskSvc:                taskSvc,
			PlanSvc:                planSvc,
			ImageSvc:               imageSvc,
			ImageModelResolver:     imageCapabilityResolver,
			ImageGenerator:         imageSvc,
			ProviderCostSvc:        fixedBilling.Cost,
			BillingCatalogSvc:      fixedBilling.Catalog,
			GenerateImageTimeout:   cfg.MCP.ToolTimeouts.GenerateImage,
			ContentRenderSvc:       contentRenderSvc,
			PublishingSvc:          publishingSvc,
			WechatPublicationSvc:   wechatPublicationSvc,
			LiveSliceSvc:           liveSliceSvc,
			SeednoteCapabilitySvc:  service.NewSeednoteCapabilityService(seednoteClient, seednoteMonitor),
			FileUploadSvc:          service.NewFileUploadService(store),
			MediaPipelineSvc:       service.NewMediaPipelineService(store, cfg.TingWu.Complete()),
			TopicPoolSvc:           topicPoolSvc,
			AgentFeedbackSvc:       agentFeedbackSvc,
			ContentMetadataSvc:     service.NewContentMetadataService(repo, log),
			AgentProjectProfileSvc: service.NewAgentProjectProfileService(projectSvc, taskSvc, resources.Manager(), cfg.Montage, imageCapabilityResolver),
			ArticleScoreSvc:        service.NewArticleScoreService(),
			SeednoteExportSvc:      service.NewSeednoteExportService(),
			ResourceCatalogSvc:     service.NewResourceCatalogService(resources.Manager()),
			TaskImageSvc:           service.NewTaskImageService(taskSvc, imageCapabilityResolver, imageSvc, fixedBilling.Catalog, log),
			TaskImageOperationsSvc: service.NewTaskImageOperationsService(taskSvc, imageSvc, imageUnderstandingClient, fixedBilling.Cost, service.TaskImageOperationsConfig{
				UnderstandingProvider: cfg.ImageUnderstanding.ProviderKey,
				UnderstandingModel:    cfg.ImageUnderstanding.Model,
			}, log),
			TaskVideoOperationsSvc: service.NewTaskVideoOperationsService(repo, store, videoUnderstandingClient, fixedBilling.Cost, service.TaskVideoOperationsConfig{
				UnderstandingProvider: cfg.VideoUnderstanding.ProviderKey,
				UnderstandingModel:    cfg.VideoUnderstanding.Model,
			}, log),
		})
		mcp.SetBillingServices(imageCapabilityResolver, cfg)
		mcp.SetLogger(log)
		mcpHandler = mcp.NewMCPHandler(apiKeySvc, cfg.MCP.APIKey, log, mcp.WithExecutionAuthentication(executionTokens, taskSvc))
		log.Info().
			Bool("mcp_static_key_set", cfg.MCP.APIKey != "").
			Bool("image_tools", imageSvc != nil).
			Bool("image_understanding", imageUnderstandingClient != nil).
			Bool("video_understanding", videoUnderstandingClient != nil).
			Bool("content_render_tools", contentRenderSvc != nil).
			Bool("live_slice_tools", liveSliceSvc != nil).
			Bool("publishing_tools", publishingSvc != nil).
			Msg("MCP handler initialized with tools (official SDK)")
	} else {
		mcpHandler = mcp.NewMCPHandler(apiKeySvc, cfg.MCP.APIKey, log, mcp.WithExecutionAuthentication(executionTokens, taskSvc))
		log.Info().Msg("MCP handler initialized (no tools, services unavailable)")
	}

	// Use one signal-derived lifecycle for runtime reconciliation and server shutdown.
	signalCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(signalCtx)
	defer cancel()

	// 15. Start Asynq worker if Redis is available.
	var asynqServer *scheduler.TaskProcessor
	if rdb != nil && taskSvc != nil {
		asynqServer = startAsynqServer(repo, taskSvc, wechatPublicationSvc, seednoteTrackingSvc, wechatTrackingSvc, channelsTrackingSvc, cfg, log)
	}

	// 15.1 Start plan checker if repository and task service are available.
	if repo != nil && taskSvc != nil {
		schedulerCtx, schedulerCancel := context.WithCancel(context.Background())
		defer schedulerCancel()
		go scheduler.StartPlanChecker(schedulerCtx, repo, taskSvc, log, rdb)
	}
	if asynqClient != nil {
		analyticsRecoveryCtx, analyticsRecoveryCancel := context.WithCancel(context.Background())
		defer analyticsRecoveryCancel()
		go startAnalyticsRecovery(analyticsRecoveryCtx, seednoteTrackingSvc, wechatTrackingSvc, channelsTrackingSvc, log)
		if wechatPublicationSvc != nil {
			publicationRecoveryCtx, publicationRecoveryCancel := context.WithCancel(context.Background())
			defer publicationRecoveryCancel()
			go startWechatPublicationRecovery(publicationRecoveryCtx, wechatPublicationSvc, log)
		}
	}
	var runtimeReconcilerDone <-chan struct{}
	if runtimeReconciler != nil {
		runtimeReconcilerDone = startRuntimeReconciler(ctx, runtimeReconciler)
	}

	// 15.2 Clean up expirable derived records without touching NAS task workspaces.
	if repo != nil {
		cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
		defer cleanupCancel()
		go startPeriodicArtifactCleanup(cleanupCtx, taskSvc, posterSvc, log)
	}

	// 15.3 Start the local-claim fallback worker (every 10s). Flips
	// pending local-target tasks past their claim deadline back to cloud
	// execution so they're never stuck when no desktop is online.
	if repo != nil && taskSvc != nil {
		reclaimCtx, reclaimCancel := context.WithCancel(context.Background())
		defer reclaimCancel()
		go startLocalClaimFallback(reclaimCtx, taskSvc, log)
	}
	if repo != nil && store != nil {
		if finalStore, ok := uploadSessionCleanupStorage(store); ok {
			uploadCleanupCtx, uploadCleanupCancel := context.WithCancel(context.Background())
			defer uploadCleanupCancel()
			service.StartUploadSessionCleanup(uploadCleanupCtx, finalStore, repo, 30*time.Minute, log)
		}
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
		PlanService:              planSvc,
		TaskService:              taskSvc,
		ProjectHandler:           projectHandler,
		PlanHandler:              planHandler,
		TaskHandler:              taskHandler,
		SeednoteAnalyticsHandler: seednoteAnalyticsHandler,
		SeednoteImportHandler:    seednoteImportHandler,
		WechatAnalyticsHandler:   wechatAnalyticsHandler,
		WechatPublicationHandler: wechatPublicationHandler,
		ChannelsAnalyticsHandler: channelsAnalyticsHandler,
		AgentHandler:             agentHandler,
		AgentProfileHandler:      agentProfileHandler,
		AgentPackHandler:         agentPackHandler,
		BillingHandler:           fixedBilling.Handler,
		BillingAdminHandler:      fixedBilling.AdminHandler,
		TimelineHandler:          timelineHandler,
		APIKeyHandler:            apiKeyHandler,
		FileHandler:              fileHandler,
		UploadHandler:            uploadHandler,
		AIEntryHandler:           aiEntryHandler,
		FeedbackHandler:          feedbackHandler,
		ImageCapabilityHandler:   imageCapabilityHandler,
		TemplateHandler:          templateHandler,
		ViralAnalysisHandler:     viralAnalysisHandler,
		PosterHandler:            posterHandler,
		ResourceHandler:          resourceHandler,
		TopicPoolHandler:         topicPoolHandler,
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

	var billingWorkerWG sync.WaitGroup
	if fixedBilling != nil && fixedBilling.Worker != nil {
		billingWorkerWG.Add(1)
		go func() {
			defer billingWorkerWG.Done()
			fixedBilling.Worker.Run(ctx)
		}()
	}

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

		if runtimeReconcilerDone != nil {
			<-runtimeReconcilerDone
		}

		if runtimeClientCloser != nil {
			if err := runtimeClientCloser.Close(); err != nil {
				log.Error().Err(err).Msg("failed to close runtime provider client")
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

		billingWorkerWG.Wait()
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
	cancel()

	// Wait for graceful shutdown to complete, with a timeout guard.
	select {
	case <-shutdownDone:
		log.Info().Msg("graceful shutdown completed")
	case <-time.After(60 * time.Second):
		log.Warn().Msg("graceful shutdown timed out after 60s, forcing exit")
		os.Exit(1)
	}
}

func requireAgentExecutionProfileSchema(db *gorm.DB) error {
	if db == nil || !db.Migrator().HasTable(&model.Task{}) {
		return nil
	}
	required := []struct {
		model  any
		column string
	}{
		{&model.Task{}, "ExecutionProfile"},
		{&model.Task{}, "AgentProfileSnapshot"},
		{&model.Task{}, "AgentProfileFingerprint"},
		{&model.Plan{}, "ExecutionProfile"},
		{&model.TaskExecution{}, "ExecutionProfile"},
		{&model.TaskExecution{}, "Provider"},
		{&model.TaskExecution{}, "ProfileEnvs"},
		{&model.TaskExecution{}, "ProfileFingerprint"},
		{&model.BillingSKU{}, "ExecutionProfile"},
		{&model.BillingQuote{}, "AgentProfileSnapshot"},
	}
	for _, item := range required {
		if !db.Migrator().HasTable(item.model) || !db.Migrator().HasColumn(item.model, item.column) {
			return fmt.Errorf("existing database requires server/migrations/20260728_agent_execution_profiles.sql before startup (missing %T.%s)", item.model, item.column)
		}
	}
	return nil
}

func managedRuntimeProjectConcurrencyCap(executor string) int {
	switch strings.TrimSpace(executor) {
	case "docker", "kubernetes":
		return 1
	default:
		return 0
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
	trackingSvc := service.NewSeednoteTrackingService(repo, nil, nil, log)
	return handler.NewSeednoteAnalyticsHandler(trackingSvc, log)
}

type billingRuntimeServices struct {
	Catalog      *service.BillingCatalogService
	Wallet       *service.BillingWalletService
	Referrals    *service.BillingReferralService
	Handler      *handler.BillingHandler
	Worker       *service.BillingMaintenanceWorker
	Cost         *service.ProviderCostService
	Margin       *service.MarginService
	AdminHandler *handler.BillingAdminHandler
}

func buildBillingRuntime(ctx context.Context, db *gorm.DB, repo repository.Repository, bundle *serverbilling.Bundle, cfg *config.Config, log *zerolog.Logger) (*billingRuntimeServices, error) {
	if repo == nil {
		return nil, fmt.Errorf("billing repository is required")
	}
	if db == nil {
		return nil, fmt.Errorf("provider cost database is required")
	}
	if cfg == nil || strings.TrimSpace(cfg.BillingRuntime.ConfigDir) == "" {
		return nil, fmt.Errorf("billing_runtime.config_dir is required")
	}
	if strings.TrimSpace(cfg.BillingRuntime.AdminAPIKey) == "" {
		return nil, fmt.Errorf("billing_runtime.admin_api_key is required")
	}
	if bundle == nil {
		return nil, fmt.Errorf("billing bundle is required")
	}
	if err := service.MigrateBillingTierPrices(ctx, db, log); err != nil {
		return nil, fmt.Errorf("migrate billing tier prices: %w", err)
	}
	catalog := service.NewBillingCatalogService(repo, bundle, service.BillingCatalogOptions{})
	if _, err := catalog.Publish(ctx); err != nil {
		return nil, fmt.Errorf("publish billing catalog: %w", err)
	}
	wallet := service.NewBillingWalletService(repo, bundle, service.BillingWalletOptions{})
	referrals := service.NewBillingReferralService(repo, wallet, bundle, service.BillingReferralOptions{})
	worker := service.NewBillingMaintenanceWorker(wallet, service.BillingMaintenanceWorkerOptions{}, log)
	cost := service.NewProviderCostService(repository.NewBillingCostRepository(db), bundle)
	margin := service.NewMarginService(repository.NewBillingMarginRepository(db), repo, bundle, service.MarginServiceOptions{})
	billingHandler := handler.NewBillingHandler(repo, catalog, referrals, bundle, handler.BillingHandlerOptions{
		AdminAPIKey:   cfg.BillingRuntime.AdminAPIKey,
		InviteBaseURL: "https://creator.anbanai.com/register?invite=",
	}, log)
	adminHandler := handler.NewBillingAdminHandler(margin)
	return &billingRuntimeServices{Catalog: catalog, Wallet: wallet, Referrals: referrals, Handler: billingHandler, Worker: worker, Cost: cost, Margin: margin, AdminHandler: adminHandler}, nil
}

// startAsynqServer starts the Asynq task processor in a background goroutine.
func startAsynqServer(repo repository.Repository, taskSvc *service.TaskService, wechatPublicationSvc *service.WechatPublicationService, seednoteTrackingSvc *service.SeednoteTrackingService, wechatTrackingSvc *service.WechatTrackingService, channelsTrackingSvc *service.ChannelsTrackingService, cfg *config.Config, log *zerolog.Logger) *scheduler.TaskProcessor {
	var seednoteCaptureHandler scheduler.SeednoteTrackingHandler
	if seednoteTrackingSvc != nil {
		seednoteCaptureHandler = func(ctx context.Context, trackingID string) error {
			return seednoteTrackingSvc.CaptureMetrics(ctx, trackingID)
		}
	}
	var wechatCaptureHandler scheduler.WechatTrackingHandler
	if wechatTrackingSvc != nil {
		wechatCaptureHandler = func(ctx context.Context, trackingID string) error {
			return wechatTrackingSvc.CaptureMetrics(ctx, trackingID)
		}
	}
	var channelsCaptureHandler scheduler.ChannelsTrackingHandler
	if channelsTrackingSvc != nil {
		channelsCaptureHandler = func(ctx context.Context, trackingID string) error {
			return channelsTrackingSvc.CaptureMetrics(ctx, trackingID)
		}
	}

	publicationHandlers := scheduler.WechatPublicationHandlers{}
	if wechatPublicationSvc != nil {
		publicationHandlers.Poll = func(ctx context.Context, publicationID string) error {
			return wechatPublicationSvc.ProcessPoll(ctx, publicationID)
		}
		publicationHandlers.Reconcile = func(ctx context.Context, projectID string) error {
			return wechatPublicationSvc.ReconcileProject(ctx, projectID)
		}
	}
	srv := scheduler.NewTaskProcessor(
		func(ctx context.Context, taskID, userID string) error {
			return taskSvc.HandleExecutionFromPayload(ctx, taskID, userID)
		},
		func(ctx context.Context, planID string) error {
			return scheduler.TriggerPlanNow(ctx, repo, taskSvc, planID, log)
		},
		seednoteCaptureHandler,
		wechatCaptureHandler,
		channelsCaptureHandler,
		cfg.Redis.Addr,
		cfg.Redis.Password,
		cfg.Redis.DB,
		cfg.Asynq.Concurrency,
		log,
		publicationHandlers,
	)

	go func() {
		log.Info().Int("concurrency", cfg.Asynq.Concurrency).Msg("starting Asynq task processor")
		if err := srv.Start(); err != nil {
			log.Error().Err(err).Msg("Asynq server start error")
		}
	}()

	return srv
}

type analyticsRecoveryService interface {
	RecoverDue(ctx context.Context, limit int) error
}

func startAnalyticsRecovery(ctx context.Context, seednote, wechat, channels analyticsRecoveryService, log *zerolog.Logger) {
	run := func() {
		for name, tracker := range map[string]analyticsRecoveryService{"seednote": seednote, "wechat": wechat, "channels": channels} {
			if tracker == nil {
				continue
			}
			if err := tracker.RecoverDue(ctx, 100); err != nil && ctx.Err() == nil {
				log.Error().Err(err).Str("platform", name).Msg("analytics recovery failed")
			}
		}
	}
	run()
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func startWechatPublicationRecovery(ctx context.Context, publications analyticsRecoveryService, log *zerolog.Logger) {
	run := func() {
		if err := publications.RecoverDue(ctx, 100); err != nil && ctx.Err() == nil {
			log.Error().Err(err).Msg("WeChat publication recovery failed")
		}
	}
	run()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func uploadSessionCleanupStorage(store storage.Provider) (service.DirectUploadFinalizationStorage, bool) {
	finalStore, ok := store.(service.DirectUploadFinalizationStorage)
	return finalStore, ok
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
func startPeriodicArtifactCleanup(ctx context.Context, taskSvc *service.TaskService, posterSvc *service.PosterService, log *zerolog.Logger) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	cleanup := func() {
		if taskSvc != nil {
			if _, err := taskSvc.CleanupSupersededTaskFileObjects(ctx, 100); err != nil {
				log.Error().Err(err).Msg("superseded task file object cleanup failed")
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
