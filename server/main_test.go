package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMainUsesAsyncSidecarMonitors(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	src := string(raw)

	if strings.Contains(src, "awaitSidecar") {
		t.Fatal("main.go must not synchronously await optional sidecars during startup")
	}
	for _, want := range []string{
		"seednoteMonitor := service.NewSidecarMonitor",
		"go seednoteMonitor.Run(ctx)",
		"ilinkMonitor = service.NewSidecarMonitor",
		"go ilinkMonitor.Run(ctx)",
		"projectHandler.SetSeednoteReadiness(seednoteMonitor)",
		"SeednoteReadiness:",
		"ilinkPoller.SetReadiness(ilinkMonitor)",
		"ilinkWorker.SetReadiness(ilinkMonitor)",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("main.go missing async sidecar wiring %q", want)
		}
	}
}

func TestMigrateModelsReturnsMigrationFailure(t *testing.T) {
	want := errors.New("migration failed")
	called := false
	err := migrateModels(&gorm.DB{}, func(*gorm.DB) error {
		called = true
		return want
	})
	if !called {
		t.Fatal("migration function was not called")
	}
	if !errors.Is(err, want) {
		t.Fatalf("migrateModels error = %v, want %v", err, want)
	}
}

func TestMainFailsFastWhenModelMigrationFails(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	src := string(raw)
	start := strings.Index(src, "// 6. Auto-migrate models.")
	end := strings.Index(src, "// 6.1 One-time backfill")
	if start < 0 || end <= start {
		t.Fatal("main.go migration startup section is missing")
	}
	section := src[start:end]
	if !strings.Contains(section, "migrateModels(mysqlDB, model.AutoMigrate)") {
		t.Fatal("main.go must route model migration through the tested startup helper")
	}
	if !strings.Contains(section, `log.Fatal().Err(err).Msg("failed to auto-migrate models")`) {
		t.Fatal("model migration errors must terminate startup")
	}
	if strings.Contains(section, "log.Error()") {
		t.Fatal("model migration errors must not log and continue startup")
	}
}

func TestBuildBillingRuntime(t *testing.T) {
	catalogDir, err := filepath.Abs("billing")
	if err != nil {
		t.Fatal(err)
	}
	newRepo := func(t *testing.T) repository.Repository {
		t.Helper()
		db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		if err := model.AutoMigrate(db); err != nil {
			t.Fatal(err)
		}
		repo := repository.New(db)
		t.Cleanup(func() { _ = repo.Close() })
		return repo
	}
	logger := zerolog.New(os.Stderr)

	t.Run("required config", func(t *testing.T) {
		for _, cfg := range []*config.Config{
			{BillingRuntime: config.BillingRuntimeConfig{AdminAPIKey: "key"}},
			{BillingRuntime: config.BillingRuntimeConfig{ConfigDir: catalogDir}},
		} {
			if _, err := buildBillingRuntime(t.Context(), newRepo(t), cfg, &logger); err == nil {
				t.Fatalf("buildBillingRuntime(%+v) error = nil", cfg.BillingRuntime)
			}
		}
	})

	t.Run("load failure", func(t *testing.T) {
		cfg := &config.Config{BillingRuntime: config.BillingRuntimeConfig{ConfigDir: t.TempDir(), AdminAPIKey: "key"}}
		if _, err := buildBillingRuntime(t.Context(), newRepo(t), cfg, &logger); err == nil || !strings.Contains(err.Error(), "load billing bundle") {
			t.Fatalf("load failure = %v", err)
		}
	})

	t.Run("publish failure", func(t *testing.T) {
		repo := newRepo(t)
		if err := repo.Close(); err != nil {
			t.Fatal(err)
		}
		cfg := &config.Config{BillingRuntime: config.BillingRuntimeConfig{ConfigDir: catalogDir, AdminAPIKey: "key"}}
		if _, err := buildBillingRuntime(t.Context(), repo, cfg, &logger); err == nil || !strings.Contains(err.Error(), "publish billing catalog") {
			t.Fatalf("publish failure = %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		repo := newRepo(t)
		cfg := &config.Config{BillingRuntime: config.BillingRuntimeConfig{ConfigDir: catalogDir, AdminAPIKey: "key"}}
		runtime, err := buildBillingRuntime(t.Context(), repo, cfg, &logger)
		if err != nil || runtime == nil || runtime.Handler == nil || runtime.Catalog == nil || runtime.Wallet == nil || runtime.Referrals == nil {
			t.Fatalf("buildBillingRuntime = %+v, %v", runtime, err)
		}
		if _, err := repo.Billing().FindCatalogVersion(t.Context(), "retail-2026-07-17-v1"); err != nil {
			t.Fatalf("published production catalog: %v", err)
		}
	})
}

func TestMainWiresRequiredBillingRuntime(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, required := range []string{
		"buildBillingRuntime(context.Background(), repo, cfg, log)",
		"BillingHandler:",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("main billing wiring missing %q", required)
		}
	}
}
