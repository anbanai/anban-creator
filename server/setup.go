package main

import (
	"context"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/storage"
)

// defaultConfigPaths lists config file locations to try when -config is not set.
var defaultConfigPaths = []string{"./config.yaml", "./server/config.yaml"}

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

// setupStorage creates the storage provider. Returns nil on failure (logs error).
func setupStorage(cfg *config.Config, log *zerolog.Logger) storage.Provider {
	store, err := storage.NewProvider(cfg.Storage, log)
	if err != nil {
		if cfg.Storage.Provider == "oss" {
			log.Fatal().Err(err).Msg("failed to create OSS storage provider (check credentials and configuration)")
		}
		log.Warn().Err(err).Msg("failed to create storage provider, file uploads will be unavailable")
		return nil
	}
	log.Info().Str("provider", store.Name()).Msg("storage provider initialized")
	return store
}
