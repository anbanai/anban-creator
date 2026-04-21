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

	"github.com/gofiber/fiber/v3"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/router"
	"github.com/royalrick/anbanwriter/server/storage"
)

func main() {
	configPath, port := parseFlags()
	cfg := loadConfig(resolveConfigPath(configPath))
	applyPortOverride(cfg, port)
	log := initLogger(cfg)

	mysqlDB := connectMySQL(context.Background(), cfg, log)
	rdb := connectRedis(context.Background(), cfg, log)
	autoMigrate(mysqlDB, log)

	var repo repository.Repository
	if mysqlDB != nil {
		repo = repository.New(mysqlDB)
	}

	store := setupStorage(cfg, log)
	core := setupCoreServices(cfg, rdb, repo, store, log)
	handlers := setupHandlers(cfg, core, repo, log)
	mcpHandler := setupMCPHandler(cfg, core, repo, log)
	workers := startWorkers(cfg, core, repo, rdb, log)

	svcs := buildRouterServices(cfg, log, mysqlDB, rdb, repo, core, handlers, mcpHandler, store)
	app := router.NewRouter(svcs)
	runServer(app, cfg, workers, core, repo, mysqlDB, rdb, log)
}

// parseFlags parses command-line flags and returns the config path and port override.
func parseFlags() (configPath string, port int) {
	cp := flag.String("config", "", "path to server config file (default: ./config.yaml or ./server/config.yaml)")
	p := flag.Int("port", 0, "server listen port (overrides config and ANBAN_SERVER_PORT)")
	flag.Parse()
	return *cp, *p
}

// loadConfig creates a new config from the given file path.
func loadConfig(cfgFile string) *config.Config {
	cfg, err := config.NewConfig(cfgFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}
	return cfg
}

// applyPortOverride sets the server port from the -port flag or ANBAN_SERVER_PORT env var.
func applyPortOverride(cfg *config.Config, port int) {
	if port > 0 {
		cfg.Server.Port = port
	} else if v := os.Getenv("ANBAN_SERVER_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			cfg.Server.Port = p
		}
	}
}

// initLogger initializes the global zerolog logger.
func initLogger(cfg *config.Config) *zerolog.Logger {
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

	return log
}

// autoMigrate runs database auto-migration if MySQL is available.
func autoMigrate(mysqlDB *gorm.DB, log *zerolog.Logger) {
	if mysqlDB == nil {
		return
	}
	if err := model.AutoMigrate(mysqlDB); err != nil {
		log.Error().Err(err).Msg("failed to auto-migrate models")
	} else {
		log.Info().Msg("database migration completed")
	}
}

// buildRouterServices assembles the router.Services struct from all wired components.
func buildRouterServices(
	cfg *config.Config,
	log *zerolog.Logger,
	mysqlDB *gorm.DB,
	rdb *redis.Client,
	repo repository.Repository,
	core *coreServices,
	h *handlerInstances,
	mcpHandler http.Handler,
	store storage.Provider,
) *router.Services {
	return &router.Services{
		Config:          cfg,
		Logger:          log,
		DB:              mysqlDB,
		Redis:           rdb,
		Repo:            repo,
		JWTService:      core.jwtSvc,
		WechatSvc:       core.wechatSvc,
		WSHub:           core.wsHub,
		AuthHandler:     h.authHandler,
		Executor:        core.agentExecutor,
		PlanService:     core.planSvc,
		TaskService:     core.taskSvc,
		CreditService:   core.creditSvc,
		ChannelHandler:  h.channelHandler,
		PlanHandler:     h.planHandler,
		TaskHandler:     h.taskHandler,
		AgentHandler:    h.agentHandler,
		CreditHandler:   h.creditHandler,
		TimelineHandler: h.timelineHandler,
		APIKeyHandler:   h.apiKeyHandler,
		FileHandler:     h.fileHandler,
		MCPHandler:      mcpHandler,
		StorageProvider: store,
	}
}

// runServer starts the HTTP server and blocks until it exits, handling graceful shutdown.
func runServer(
	app *fiber.App,
	cfg *config.Config,
	workers *workerInstances,
	core *coreServices,
	repo repository.Repository,
	mysqlDB *gorm.DB,
	rdb *redis.Client,
	log *zerolog.Logger,
) {
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		log.Info().Msg("shutdown signal received, stopping server...")
		gracefulShutdown(app, workers, core, repo, mysqlDB, rdb, log)
	}()

	log.Info().Str("addr", addr).Msg("server starting")
	if err := app.Listen(addr); err != nil {
		log.Error().Err(err).Msg("server listen error")
	}
	log.Info().Msg("server exited")
}

// gracefulShutdown handles graceful shutdown of all components.
func gracefulShutdown(
	app *fiber.App,
	workers *workerInstances,
	core *coreServices,
	repo repository.Repository,
	mysqlDB *gorm.DB,
	rdb *redis.Client,
	log *zerolog.Logger,
) {
	if workers.schedulerCancel != nil {
		workers.schedulerCancel()
	}
	if workers.cleanupCancel != nil {
		workers.cleanupCancel()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := app.ShutdownWithContext(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("server shutdown error")
	}

	// Close WebSocket hub gracefully.
	if core.wsHub != nil {
		core.wsHub.Shutdown()
		log.Info().Msg("WebSocket hub stopped")
	}

	// Stop Asynq server.
	if workers.asynqServer != nil {
		workers.asynqServer.Shutdown()
		log.Info().Msg("Asynq server stopped")
	}

	// Close Docker executor client if applicable.
	if closer, ok := core.agentExecutor.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			log.Error().Err(err).Msg("failed to close agent executor")
		}
	}

	// Close Asynq client.
	if core.asynqClient != nil {
		if err := core.asynqClient.Close(); err != nil {
			log.Error().Err(err).Msg("failed to close Asynq client")
		}
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
}
