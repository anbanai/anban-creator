package main

import (
	"os"
	"strings"
	"testing"
)

func TestK8sSidecarManifestDefinesInternalWcflinkAndSeednoteServices(t *testing.T) {
	raw, err := os.ReadFile("../deploy/k8s/sidecars.yaml")
	if err != nil {
		t.Fatalf("read sidecar manifest: %v", err)
	}
	text := string(raw)

	for _, want := range []string{
		"name: wcflink",
		"name: seednote",
		"kind: PersistentVolumeClaim",
		"claimName: wcflink-state",
		"claimName: seednote-data",
		"type: ClusterIP",
		"port: 18070",
		"targetPort: 18070",
		"port: 18060",
		"targetPort: 18060",
		"path: /health/live",
		"path: /health/ready",
		"path: /health",
		"kubernetes.io/arch",
		"amd64",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("sidecar manifest missing %q", want)
		}
	}

	if strings.Contains(text, "kind: Ingress") || strings.Contains(text, "type: LoadBalancer") || strings.Contains(text, "type: NodePort") {
		t.Fatalf("sidecar manifest must keep sidecars internal-only")
	}
}

func TestServerDeploymentInjectsSidecarURLs(t *testing.T) {
	raw, err := os.ReadFile("Deployment.yaml")
	if err != nil {
		t.Fatalf("read server deployment: %v", err)
	}
	text := string(raw)

	for _, want := range []string{
		"name: ANBAN_SEEDNOTE_BASE_URL",
		"value: http://seednote:18060",
		"name: ANBAN_ILINK_ENABLED",
		"value: \"true\"",
		"name: ANBAN_ILINK_BASE_URL",
		"value: http://wcflink:18070",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("server deployment missing %q", want)
		}
	}
}

func TestConfigExampleDocumentsSeednoteSidecarEnv(t *testing.T) {
	raw, err := os.ReadFile("config.example.yaml")
	if err != nil {
		t.Fatalf("read config example: %v", err)
	}
	text := string(raw)

	for _, want := range []string{
		"seednote:",
		`base_url: "${ANBAN_SEEDNOTE_BASE_URL:-http://localhost:18060}"`,
		"timeout: 30",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("config example missing %q", want)
		}
	}
}
