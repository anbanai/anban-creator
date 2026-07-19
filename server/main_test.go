package main

import (
	"errors"
	"os"
	"strings"
	"testing"

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

func TestMainWiresReferenceAssetServiceEvenWithoutStorage(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	src := string(raw)
	serviceStart := strings.Index(src, "taskSvc = service.NewTaskService")
	serviceEnd := strings.Index(src[serviceStart:], "taskSvc.SetProjectMemoryManager")
	if serviceStart < 0 || serviceEnd < 0 {
		t.Fatal("task service wiring section is missing")
	}
	serviceSection := src[serviceStart : serviceStart+serviceEnd]
	constructAt := strings.Index(serviceSection, "referenceAssetSvc = service.NewReferenceAssetService(repo, store, time.Now)")
	storeGuardAt := strings.Index(serviceSection, "if store != nil")
	if constructAt < 0 || (storeGuardAt >= 0 && constructAt > storeGuardAt) {
		t.Fatalf("reference asset service construction must be unconditional after repository setup:\n%s", serviceSection)
	}
	for _, want := range []string{
		"planSvc.SetReferenceAssetService(referenceAssetSvc)",
		"taskSvc.SetReferenceAssetService(referenceAssetSvc)",
		"aiEntrySvc.SetReferenceAssetService(referenceAssetSvc)",
		"planHandler.SetReferenceAssetService(referenceAssetSvc)",
		"taskHandler.SetReferenceAssetService(referenceAssetSvc)",
		"projectHandler.SetReferenceAssetService(referenceAssetSvc)",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("main.go missing reference asset wiring %q", want)
		}
	}
	projectStart := strings.Index(src, "projectHandler = handler.NewProjectHandler")
	projectEnd := strings.Index(src[projectStart:], "projectHandler.SetSeednoteClient")
	if projectStart < 0 || projectEnd < 0 {
		t.Fatal("project handler wiring section is missing")
	}
	projectSection := src[projectStart : projectStart+projectEnd]
	presentAt := strings.Index(projectSection, "projectHandler.SetReferenceAssetService(referenceAssetSvc)")
	storeGuardAt = strings.Index(projectSection, "if store != nil")
	if presentAt < 0 || (storeGuardAt >= 0 && presentAt > storeGuardAt) {
		t.Fatalf("project handler reference service wiring must not depend on storage:\n%s", projectSection)
	}
}
