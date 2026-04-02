package main

import (
	"context"
	"flag"
	"fmt"
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
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/router"
	"github.com/royalrick/anbanwriter/server/scheduler"
	"github.com/royalrick/anbanwriter/server/service"
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

	// 3. Init logger (JSON to stderr, level from LOG_LEVEL or "info").
	logLevel := parseLogLevel(os.Getenv("LOG_LEVEL"))
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	logger := zerolog.New(os.Stderr).With().Timestamp().Logger().Level(logLevel)
	log := &logger

	log.Info().
		Int("port", cfg.Server.Port).
		Str("host", cfg.Server.Host).
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
	wsHub := handler.NewWebSocketHub()

	// 11. Create auth handler.
	var authHandler *handler.AuthHandler
	if repo != nil {
		authHandler = handler.NewAuthHandler(jwtSvc, wechatSvc, repo, log, wsHub)
	}

	// 12. Create agent executor.
	agentExecutor := agent.NewExecutor(log)

	// 13. Create services.
	var planSvc *service.PlanService
	var taskSvc *service.TaskService
	var asynqClient *scheduler.AsynqClient

	if repo != nil {
		planSvc = service.NewPlanService(repo, log)

		// Create Asynq client if Redis is available.
		if rdb != nil {
			asynqClient = scheduler.NewAsynqClient(
				cfg.Redis.Addr,
				cfg.Redis.Password,
				cfg.Redis.DB,
			)
			log.Info().Msg("Asynq client initialized")
		}

		taskSvc = service.NewTaskService(repo, agentExecutor, asynqClient, log)
	}

	// 14. Create handlers.
	var planHandler *handler.PlanHandler
	var taskHandler *handler.TaskHandler
	var configHandler *handler.ConfigHandler
	var timelineHandler *handler.TimelineHandler

	if repo != nil {
		planHandler = handler.NewPlanHandler(planSvc, log)
		taskHandler = handler.NewTaskHandler(taskSvc, log)
		configHandler = handler.NewConfigHandler(repo, log)
		timelineHandler = handler.NewTimelineHandler(repo, log)
	}

	// 15. Start Asynq worker if Redis is available.
	var asynqServer *scheduler.TaskProcessor
	if rdb != nil && taskSvc != nil {
		asynqServer = startAsynqServer(taskSvc, cfg, log)
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
		PlanHandler:     planHandler,
		TaskHandler:     taskHandler,
		ConfigHandler:   configHandler,
		TimelineHandler: timelineHandler,
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

		// Close Asynq client.
		if asynqClient != nil {
			if err := asynqClient.Close(); err != nil {
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
	}()

	log.Info().Str("addr", addr).Msg("server starting")
	if err := app.Listen(addr); err != nil {
		log.Error().Err(err).Msg("server listen error")
	}

	log.Info().Msg("server exited")
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
			log.Info().Msg("task cleanup: placeholder")
			return nil
		},
		cfg.Redis.Addr,
		cfg.Redis.Password,
		cfg.Redis.DB,
		1,
		log,
	)

	go func() {
		log.Info().Msg("starting Asynq task processor")
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
