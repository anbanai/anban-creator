package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"github.com/royalrick/anbanwriter/server/config"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to server config file")
	flag.Parse()

	// Initialize zerolog
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})

	logger := &log.Logger

	// Load configuration
	cfg, err := config.NewConfig(*configPath)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to load config")
	}
	logger.Info().
		Int("port", cfg.Server.Port).
		Str("host", cfg.Server.Host).
		Msg("config loaded")

	// Attempt MySQL connection (log result, do not block startup for now)
	mysqlDB, err := gorm.Open(mysql.Open(cfg.Database.DSN), &gorm.Config{})
	if err != nil {
		logger.Error().Err(err).Msg("failed to connect to MySQL")
	} else {
		sqlDB, _ := mysqlDB.DB()
		sqlDB.SetMaxOpenConns(cfg.Database.MaxOpenConns)
		sqlDB.SetMaxIdleConns(cfg.Database.MaxIdleConns)
		sqlDB.SetConnMaxLifetime(time.Duration(cfg.Database.ConnMaxLifetime) * time.Second)
		logger.Info().Msg("MySQL connection established")
	}

	// Attempt Redis connection (log result, do not block startup for now)
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	ctx := log.Logger.WithContext(nil)
	if _, err := rdb.Ping(ctx).Result(); err != nil {
		logger.Error().Err(err).Msg("failed to connect to Redis")
	} else {
		logger.Info().Str("addr", cfg.Redis.Addr).Msg("Redis connection established")
	}

	_ = mysqlDB  // used in subsequent tasks
	_ = rdb      // used in subsequent tasks

	logger.Info().Str("addr", fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)).Msg("server starting")

	// Fiber app will be wired up in a subsequent task
	// For now, exit after verifying infrastructure connectivity
}
