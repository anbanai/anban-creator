package storage

import (
	"github.com/anbanai/anban-creator/server/config"
	"github.com/rs/zerolog"
)

// NewProvider creates a storage Provider based on the configuration.
// Supported providers: "oss" (Alibaba Cloud OSS), anything else falls back
// to "local" (filesystem).
func NewProvider(cfg config.StorageConfig, logger *zerolog.Logger) (Provider, error) {
	switch cfg.Provider {
	case "oss":
		return NewOSSProvider(cfg, logger)
	default:
		return NewLocalProviderWithLogger(cfg.LocalDataDir, logger)
	}
}
