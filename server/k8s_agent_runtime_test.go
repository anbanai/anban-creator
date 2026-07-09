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
	docs := splitKubernetesYAMLDocuments(text)
	if len(docs) != 5 {
		t.Fatalf("ACK agent runtime manifest has %d YAML document(s), want server ServiceAccount, agent ServiceAccount, Role, RoleBinding, and PVC", len(docs))
	}
	for i, want := range []string{"name: anban-server", "name: anban-agent-runner", "kind: Role", "kind: RoleBinding", "kind: PersistentVolumeClaim"} {
		if !strings.Contains(docs[i], want) {
			t.Fatalf("ACK agent runtime manifest doc %d missing %q:\n%s", i+1, want, docs[i])
		}
	}
	if strings.Contains(text, "\nkind: Deployment\n") {
		t.Fatalf("ACK agent runtime manifest must not include server Deployment; server deployment lives in server/Deployment.yaml")
	}

	for _, want := range []string{
		"kind: ServiceAccount",
		"name: anban-server",
		"name: anban-agent-runner",
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
	configYAML, err := os.ReadFile("config.yaml")
	if err != nil {
		t.Fatalf("read config yaml: %v", err)
	}
	for _, body := range []struct {
		name string
		text string
	}{
		{name: "config.example.yaml", text: string(configExample)},
		{name: "config.yaml", text: string(configYAML)},
	} {
		for _, want := range []string{
			`agent_image: "${ANBAN_AGENT_IMAGE:-anban-creator-server:latest}"`,
			`service_account: "${ANBAN_AGENT_SERVICE_ACCOUNT:-anban-agent-runner}"`,
			`workspace_pvc_name: "${ANBAN_AGENT_WORKSPACE_PVC:-anban-agent-nas}"`,
			`pod_revision: "${ANBAN_AGENT_POD_REVISION}"`,
		} {
			if !strings.Contains(body.text, want) {
				t.Fatalf("%s must include Kubernetes config %q", body.name, want)
			}
		}
	}
}

func splitKubernetesYAMLDocuments(text string) []string {
	var docs []string
	for _, part := range strings.Split(text, "\n---") {
		part = strings.TrimSpace(part)
		if part == "" || strings.HasPrefix(part, "#") {
			continue
		}
		docs = append(docs, part)
	}
	return docs
}
