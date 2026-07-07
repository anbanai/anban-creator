package config

import (
	"strings"
	"testing"
)

func validOSSConfig(customDomain string) *Config {
	cfg := &Config{
		Database: DatabaseConfig{DSN: "dsn"},
		JWT:      JWTConfig{SecretKey: "secret"},
		Storage: StorageConfig{
			Provider:        "oss",
			Endpoint:        "oss-cn-chengdu.aliyuncs.com",
			AccessKeyID:     "ak",
			AccessKeySecret: "sk",
			BucketName:      "anbancreator",
			CustomDomain:    customDomain,
		},
		Claude: ClaudeConfig{Executor: "docker"},
	}
	cfg.applyDefaults()
	return cfg
}

func TestValidateRejectsStorageCustomDomainWithPath(t *testing.T) {
	cfg := validOSSConfig("https://api.creator.anbanai.com/api/v1/files")

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want storage.custom_domain path error")
	}
	if !strings.Contains(err.Error(), "storage.custom_domain") {
		t.Fatalf("Validate() error = %v, want storage.custom_domain mention", err)
	}
}

func TestValidateAllowsEmptyStorageCustomDomain(t *testing.T) {
	cfg := validOSSConfig("")

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() with empty storage.custom_domain: %v", err)
	}
}
