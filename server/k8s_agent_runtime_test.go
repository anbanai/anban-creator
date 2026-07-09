package main

import (
	"os"
	"strings"
	"testing"
)

func TestACKAgentRuntimeManifest(t *testing.T) {
	raw, err := os.ReadFile("../deploy/k8s/ack-agent-runtime.yaml")
	if err != nil {
		t.Fatalf("read ACK agent runtime manifest: %v", err)
	}
	text := string(raw)

	for _, want := range []string{
		"kind: ServiceAccount",
		"name: anban-server",
		"kind: Role",
		"name: anban-agent-runner",
		"resources:",
		"- pods",
		"- get",
		"- list",
		"- watch",
		"- create",
		"- delete",
		"- pods/exec",
		"kind: RoleBinding",
		"kind: PersistentVolumeClaim",
		"name: anban-agent-nas",
		"ReadWriteMany",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("ACK agent runtime manifest missing %q", want)
		}
	}

	if !strings.Contains(text, "server does not mount this PVC") {
		t.Fatalf("ACK agent runtime manifest must document that the server does not mount NAS")
	}

	serverDeployment, err := os.ReadFile("Deployment.yaml")
	if err != nil {
		t.Fatalf("read server deployment: %v", err)
	}
	if strings.Contains(string(serverDeployment), "anban-agent-nas") {
		t.Fatalf("server deployment must not mount the agent NAS PVC")
	}

	configExample, err := os.ReadFile("config.example.yaml")
	if err != nil {
		t.Fatalf("read config example: %v", err)
	}
	if !strings.Contains(string(configExample), `workspace_pvc_name: "${ANBAN_AGENT_WORKSPACE_PVC:-anban-agent-nas}"`) {
		t.Fatalf("config example must use the same anban-agent-nas PVC default")
	}
}
