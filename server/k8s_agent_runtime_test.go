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
		APIGroups []string `yaml:"apiGroups"`
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
	if len(docs) != 6 {
		t.Fatalf("ACK agent runtime manifest has %d YAML document(s), want two ServiceAccounts, Role, ClusterRole, ClusterRoleBinding, and RoleBinding", len(docs))
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
		{kind: "ClusterRole", name: "creator-server-tokenreview"},
		{kind: "ClusterRoleBinding", name: "creator-server-tokenreview"},
		{kind: "RoleBinding", name: "creator-agent-runner"},
	}
	for i, want := range wantKindsAndNames {
		if got := parsedDocs[i]; got.Kind != want.kind || got.Metadata.Name != want.name {
			t.Fatalf("ACK manifest doc %d = %s/%s, want %s/%s", i+1, got.Kind, got.Metadata.Name, want.kind, want.name)
		}
	}
	role := parsedDocs[2]
	if !roleAllows(role, "jobs", "get", "list", "watch", "create", "delete") {
		t.Fatalf("Role must allow Job lifecycle management: %#v", role.Rules)
	}
	if !roleAllows(role, "persistentvolumeclaims", "get", "create", "delete") {
		t.Fatalf("Role must allow PVC lifecycle management: %#v", role.Rules)
	}
	if !roleAllows(role, "pods", "get", "list", "watch") || !roleAllows(role, "pods/log", "get", "list", "watch") {
		t.Fatalf("Role must allow Pod and log reads: %#v", role.Rules)
	}
	if roleAllows(role, "pods", "create") || roleAllows(role, "pods/exec", "create") {
		t.Fatalf("Role must not allow Pod creation or exec: %#v", role.Rules)
	}
	clusterRole := parsedDocs[3]
	if !roleAllows(clusterRole, "tokenreviews", "create") || len(clusterRole.Rules) != 1 || !containsString(clusterRole.Rules[0].APIGroups, "authentication.k8s.io") {
		t.Fatalf("ClusterRole must only create TokenReviews: %#v", clusterRole.Rules)
	}
	binding := parsedDocs[5]
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
		"ANBAN_CLAUDE_EXECUTOR":            "kubernetes",
		"ANBAN_CLAUDE_AGENT_SERVER_URL":    "https://${micro_service_name}-svc.${namespace}.svc.cluster.local:8443",
		"ANBAN_AGENT_NAMESPACE":            "${namespace}",
		"ANBAN_AGENT_IMAGE":                "${agent_image_repo}",
		"ANBAN_AGENT_SERVICE_ACCOUNT":      "creator-agent-runner",
		"ANBAN_AGENT_SERVER_CA_SECRET":     "anban-server-tls",
		"ANBAN_AGENT_IMAGE_PULL_SECRET":    "${imagePullSecret}",
		"ANBAN_AGENT_MEMORY_STORAGE_CLASS": "nas-sc-creator",
		"ANBAN_SERVER_TLS_CERT_FILE":       "/var/run/secrets/anban-server-tls/tls.crt",
		"ANBAN_SERVER_TLS_KEY_FILE":        "/var/run/secrets/anban-server-tls/tls.key",
		"SSL_CERT_FILE":                    "/var/run/secrets/anban-server-tls/ca.crt",
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
			`server_ca_secret: "${ANBAN_AGENT_SERVER_CA_SECRET:-anban-server-tls}"`,
			`execution_token_secret: "${ANBAN_AGENT_EXECUTION_TOKEN_SECRET}"`,
		} {
			if !strings.Contains(body.text, want) {
				t.Fatalf("%s must include Kubernetes config %q", body.name, want)
			}
		}
	}
	for _, forbidden := range []string{"workspace_mount_path", "workspace_pvc_name", "pod_revision", "pod_ttl_seconds", "exec_timeout_seconds"} {
		if strings.Contains(string(configExample), forbidden) || strings.Contains(string(configYAML), forbidden) {
			t.Fatalf("obsolete reusable-Pod config key %q remains", forbidden)
		}
	}
	for _, want := range []string{"port: 8443", "targetPort: 8080", "secretName: anban-server-tls", "mountPath: /var/run/secrets/anban-server-tls"} {
		if !strings.Contains(string(serverDeployment), want) {
			t.Fatalf("server deployment missing TLS contract %q", want)
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
