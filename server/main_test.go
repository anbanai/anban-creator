package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	serverbilling "github.com/anbanai/anban-creator/server/billing"
	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
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
		"NewSeednoteCapabilityService(seednoteClient, seednoteMonitor)",
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

func TestAgentExecutionProfileSchemaReadiness(t *testing.T) {
	fresh, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := requireAgentExecutionProfileSchema(fresh); err != nil {
		t.Fatalf("fresh database readiness: %v", err)
	}

	legacy, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Exec("CREATE TABLE tasks (id text primary key)").Error; err != nil {
		t.Fatal(err)
	}
	if err := requireAgentExecutionProfileSchema(legacy); err == nil || !strings.Contains(err.Error(), "20260728_agent_execution_profiles.sql") {
		t.Fatalf("legacy schema readiness = %v, want migration instruction", err)
	}

	ready, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(ready); err != nil {
		t.Fatal(err)
	}
	if err := requireAgentExecutionProfileSchema(ready); err != nil {
		t.Fatalf("current schema readiness: %v", err)
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
	readiness := strings.Index(section, "requireAgentExecutionProfileSchema(mysqlDB)")
	autoMigrate := strings.Index(section, "migrateModels(mysqlDB, model.AutoMigrate)")
	if readiness < 0 || autoMigrate < 0 || readiness >= autoMigrate {
		t.Fatalf("schema readiness must run before AutoMigrate: readiness=%d auto_migrate=%d", readiness, autoMigrate)
	}
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
	bundle, err := serverbilling.LoadBundle(catalogDir)
	if err != nil {
		t.Fatal(err)
	}
	newRepo := func(t *testing.T) (*gorm.DB, repository.Repository) {
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
		return db, repo
	}
	logger := zerolog.New(os.Stderr)

	t.Run("required config", func(t *testing.T) {
		for _, cfg := range []*config.Config{
			{BillingRuntime: config.BillingRuntimeConfig{AdminAPIKey: "key"}},
			{BillingRuntime: config.BillingRuntimeConfig{ConfigDir: catalogDir}},
		} {
			db, repo := newRepo(t)
			if _, err := buildBillingRuntime(t.Context(), db, repo, bundle, cfg, &logger); err == nil {
				t.Fatalf("buildBillingRuntime(%+v) error = nil", cfg.BillingRuntime)
			}
		}
	})

	t.Run("bundle required", func(t *testing.T) {
		cfg := &config.Config{BillingRuntime: config.BillingRuntimeConfig{ConfigDir: catalogDir, AdminAPIKey: "key"}}
		db, repo := newRepo(t)
		if _, err := buildBillingRuntime(t.Context(), db, repo, nil, cfg, &logger); err == nil || !strings.Contains(err.Error(), "billing bundle is required") {
			t.Fatalf("nil bundle = %v", err)
		}
	})

	t.Run("migration failure", func(t *testing.T) {
		db, repo := newRepo(t)
		if err := repo.Close(); err != nil {
			t.Fatal(err)
		}
		cfg := &config.Config{BillingRuntime: config.BillingRuntimeConfig{ConfigDir: catalogDir, AdminAPIKey: "key"}}
		if _, err := buildBillingRuntime(t.Context(), db, repo, bundle, cfg, &logger); err == nil || !strings.Contains(err.Error(), "migrate billing tier prices") {
			t.Fatalf("migration failure = %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		db, repo := newRepo(t)
		cfg := &config.Config{BillingRuntime: config.BillingRuntimeConfig{ConfigDir: catalogDir, AdminAPIKey: "key"}}
		runtime, err := buildBillingRuntime(t.Context(), db, repo, bundle, cfg, &logger)
		if err != nil || runtime == nil || runtime.Handler == nil || runtime.AdminHandler == nil || runtime.Catalog == nil || runtime.Wallet == nil || runtime.Referrals == nil || runtime.Worker == nil || runtime.Cost == nil || runtime.Margin == nil {
			t.Fatalf("buildBillingRuntime = %+v, %v", runtime, err)
		}
		if _, err := repo.Billing().FindCatalogVersion(t.Context(), bundle.Products.CatalogID); err != nil {
			t.Fatalf("published production catalog: %v", err)
		}
	})

	t.Run("roll forward from previous catalog", func(t *testing.T) {
		db, repo := newRepo(t)
		previous, err := serverbilling.LoadBundle(catalogDir)
		if err != nil {
			t.Fatal(err)
		}
		previous.Products.CatalogID = "retail-2026-07-20-v2"
		previous.Products.SKUs[0].PriceCredits++
		if _, err := service.NewBillingCatalogService(repo, previous, service.BillingCatalogOptions{}).Publish(t.Context()); err != nil {
			t.Fatalf("publish previous catalog: %v", err)
		}

		cfg := &config.Config{BillingRuntime: config.BillingRuntimeConfig{ConfigDir: catalogDir, AdminAPIKey: "key"}}
		if _, err := buildBillingRuntime(t.Context(), db, repo, bundle, cfg, &logger); err != nil {
			t.Fatalf("buildBillingRuntime with previous catalog: %v", err)
		}
		for _, catalogID := range []string{"retail-2026-07-20-v2", bundle.Products.CatalogID} {
			if _, err := repo.Billing().FindCatalogVersion(t.Context(), catalogID); err != nil {
				t.Fatalf("catalog %s not preserved: %v", catalogID, err)
			}
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
		"serverbilling.LoadBundle(cfg.BillingRuntime.ConfigDir)",
		"service.NewAgentProfileRegistryFromConfig(cfg.Claude.ExecutionProfiles, billingBundle.Costs)",
		"buildBillingRuntime(context.Background(), mysqlDB, repo, billingBundle, cfg, log)",
		"service.NewBillingMaintenanceWorker(wallet",
		"BillingHandler:",
		"go func() {",
		"fixedBilling.Worker.Run(ctx)",
		"billingWorkerWG.Wait()",
		"signalCtx, stop := signal.NotifyContext",
		"ctx, cancel := context.WithCancel(signalCtx)",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("main billing wiring missing %q", required)
		}
	}
	buildStart := strings.Index(text, "func buildBillingRuntime(")
	if buildStart < 0 {
		t.Fatal("buildBillingRuntime is missing")
	}
	buildSection := text[buildStart:]
	tierMigration := strings.Index(buildSection, "service.MigrateBillingTierPrices(ctx, db, log)")
	catalogPublish := strings.Index(buildSection, "catalog.Publish(ctx)")
	if tierMigration < 0 || catalogPublish < 0 || tierMigration >= catalogPublish {
		t.Fatalf("billing tier migration must run before catalog publication: migration=%d publish=%d", tierMigration, catalogPublish)
	}
	workerStart := strings.Index(text, "fixedBilling.Worker.Run(ctx)")
	workerWait := strings.Index(text, "billingWorkerWG.Wait()")
	repoClose := strings.Index(text, "repo.Close()")
	if workerStart < 0 || workerWait <= workerStart || repoClose <= workerWait {
		t.Fatalf("billing worker lifecycle order invalid: start=%d wait=%d repoClose=%d", workerStart, workerWait, repoClose)
	}
	listen := strings.Index(text, "app.Listen(addr, listenConfig)")
	if listen < 0 || !strings.Contains(text[listen:], "cancel()") {
		t.Fatal("main must cancel the shared lifecycle context when Listen returns")
	}
}
