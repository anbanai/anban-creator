package main

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type k8sManifestDoc struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Subjects []struct {
		Kind      string `yaml:"kind"`
		Name      string `yaml:"name"`
		Namespace string `yaml:"namespace"`
	} `yaml:"subjects"`
	RoleRef struct {
		Kind string `yaml:"kind"`
		Name string `yaml:"name"`
	} `yaml:"roleRef"`
	Rules []struct {
		Resources []string `yaml:"resources"`
		Verbs     []string `yaml:"verbs"`
	} `yaml:"rules"`
}

type deploymentDoc struct {
	Kind string `yaml:"kind"`
	Spec struct {
		Template struct {
			Spec struct {
				ServiceAccountName string `yaml:"serviceAccountName"`
				Containers         []struct {
					Env []struct {
						Name  string `yaml:"name"`
						Value string `yaml:"value"`
					} `yaml:"env"`
				} `yaml:"containers"`
			} `yaml:"spec"`
		} `yaml:"template"`
	} `yaml:"spec"`
}

func TestACKAgentRuntimeManifest(t *testing.T) {
	raw, err := os.ReadFile("../deploy/k8s/ack-agent-runtime.yaml")
	if err != nil {
		t.Fatalf("read ACK agent runtime manifest: %v", err)
	}
	text := string(raw)
	docs := splitKubernetesYAMLDocuments(text)
	if len(docs) != 4 {
		t.Fatalf("ACK agent runtime manifest has %d YAML document(s), want server ServiceAccount, agent ServiceAccount, Role, and RoleBinding", len(docs))
	}
	parsedDocs := make([]k8sManifestDoc, 0, len(docs))
	for _, doc := range docs {
		var parsed k8sManifestDoc
		if err := yaml.Unmarshal([]byte(doc), &parsed); err != nil {
			t.Fatalf("parse ACK runtime manifest doc:\n%s\nerror: %v", doc, err)
		}
		parsedDocs = append(parsedDocs, parsed)
	}
	wantKindsAndNames := []struct{ kind, name string }{
		{kind: "ServiceAccount", name: "creator-server"},
		{kind: "ServiceAccount", name: "creator-agent-runner"},
		{kind: "Role", name: "creator-agent-runner"},
		{kind: "RoleBinding", name: "creator-agent-runner"},
	}
	for i, want := range wantKindsAndNames {
		if got := parsedDocs[i]; got.Kind != want.kind || got.Metadata.Name != want.name {
			t.Fatalf("ACK manifest doc %d = %s/%s, want %s/%s", i+1, got.Kind, got.Metadata.Name, want.kind, want.name)
		}
	}
	role := parsedDocs[2]
	if !roleAllows(role, "pods", "get", "list", "watch", "create", "delete") {
		t.Fatalf("Role must allow pod lifecycle management: %#v", role.Rules)
	}
	if !roleAllows(role, "pods/exec", "create", "get") {
		t.Fatalf("Role must allow pod exec: %#v", role.Rules)
	}
	binding := parsedDocs[3]
	if len(binding.Subjects) != 1 || binding.Subjects[0].Kind != "ServiceAccount" || binding.Subjects[0].Name != "creator-server" || binding.Subjects[0].Namespace != "${namespace}" {
		t.Fatalf("RoleBinding subject = %#v, want creator-server service account in template namespace", binding.Subjects)
	}
	if binding.RoleRef.Kind != "Role" || binding.RoleRef.Name != "creator-agent-runner" {
		t.Fatalf("RoleBinding roleRef = %#v, want Role/creator-agent-runner", binding.RoleRef)
	}
	if strings.Contains(text, "\nkind: Deployment\n") {
		t.Fatalf("ACK agent runtime manifest must not include server Deployment; server deployment lives in server/Deployment.yaml")
	}
	for _, forbidden := range []string{
		"PersistentVolumeClaim",
		"anban-agent-nas",
		"anban-server",
		"anban-agent-runner",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("ACK agent runtime manifest must not include obsolete resource %q", forbidden)
		}
	}

	if !strings.Contains(text, "server does not mount NAS") {
		t.Fatalf("ACK agent runtime manifest must document that the server does not mount NAS")
	}

	serverDeployment, err := os.ReadFile("Deployment.yaml")
	if err != nil {
		t.Fatalf("read server deployment: %v", err)
	}
	var deployment deploymentDoc
	if err := yaml.Unmarshal(serverDeployment, &deployment); err != nil {
		t.Fatalf("parse server deployment: %v", err)
	}
	if deployment.Kind != "Deployment" {
		t.Fatalf("first server deployment doc kind = %q, want Deployment", deployment.Kind)
	}
	if got := deployment.Spec.Template.Spec.ServiceAccountName; got != "creator-server" {
		t.Fatalf("server serviceAccountName = %q, want creator-server", got)
	}
	env := deploymentEnvMap(t, deployment)
	for name, want := range map[string]string{
		"ANBAN_CLAUDE_EXECUTOR":         "kubernetes",
		"ANBAN_CLAUDE_AGENT_SERVER_URL": "http://${micro_service_name}-svc.${namespace}.svc.cluster.local:8080",
		"ANBAN_AGENT_NAMESPACE":         "${namespace}",
		"ANBAN_AGENT_IMAGE":             "${agent_image_repo}",
		"ANBAN_AGENT_POD_REVISION":      "${version_switch}",
		"ANBAN_AGENT_SERVICE_ACCOUNT":   "creator-agent-runner",
		"ANBAN_AGENT_WORKSPACE_PVC":     "anban-creator",
		"ANBAN_AGENT_IMAGE_PULL_SECRET": "${imagePullSecret}",
	} {
		if got := env[name]; got != want {
			t.Fatalf("server env %s = %q, want %q", name, got, want)
		}
	}
	for _, forbidden := range []string{"anban-agent-nas", "serviceAccountName: anban-server"} {
		if strings.Contains(string(serverDeployment), forbidden) {
			t.Fatalf("server deployment must not include obsolete value %q", forbidden)
		}
	}

	configExample, err := os.ReadFile("config.example.yaml")
	if err != nil {
		t.Fatalf("read config example: %v", err)
	}
	if !strings.Contains(string(configExample), `workspace_pvc_name: "${ANBAN_AGENT_WORKSPACE_PVC:-anban-creator}"`) {
		t.Fatalf("config example must use the existing anban-creator PVC default")
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
			`executor: "${ANBAN_CLAUDE_EXECUTOR:-local}"`,
			`agent_image: "${ANBAN_AGENT_IMAGE}"`,
			`service_account: "${ANBAN_AGENT_SERVICE_ACCOUNT:-creator-agent-runner}"`,
			`workspace_pvc_name: "${ANBAN_AGENT_WORKSPACE_PVC:-anban-creator}"`,
			`pod_revision: "${ANBAN_AGENT_POD_REVISION}"`,
		} {
			if !strings.Contains(body.text, want) {
				t.Fatalf("%s must include Kubernetes config %q", body.name, want)
			}
		}
	}
}

func roleAllows(doc k8sManifestDoc, resource string, verbs ...string) bool {
	for _, rule := range doc.Rules {
		if !containsString(rule.Resources, resource) {
			continue
		}
		all := true
		for _, verb := range verbs {
			if !containsString(rule.Verbs, verb) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func deploymentEnvMap(t *testing.T, doc deploymentDoc) map[string]string {
	t.Helper()
	if len(doc.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("server deployment containers = %d, want 1", len(doc.Spec.Template.Spec.Containers))
	}
	env := make(map[string]string)
	for _, pair := range doc.Spec.Template.Spec.Containers[0].Env {
		if _, exists := env[pair.Name]; exists {
			t.Fatalf("duplicate server env %s", pair.Name)
		}
		env[pair.Name] = pair.Value
	}
	return env
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
