package main

import (
	"os"
	"strings"
	"testing"
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
