package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type k8sManifestDoc struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name      string `yaml:"name"`
		Namespace string `yaml:"namespace"`
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
		Selector struct {
			MatchLabels map[string]string `yaml:"matchLabels"`
		} `yaml:"selector"`
		Template struct {
			Spec struct {
				ServiceAccountName           string `yaml:"serviceAccountName"`
				AutomountServiceAccountToken *bool  `yaml:"automountServiceAccountToken"`
				InitContainers               []struct {
					Name            string   `yaml:"name"`
					Image           string   `yaml:"image"`
					ImagePullPolicy string   `yaml:"imagePullPolicy"`
					Command         []string `yaml:"command"`
					Args            []string `yaml:"args"`
					VolumeMounts    []struct {
						Name      string `yaml:"name"`
						MountPath string `yaml:"mountPath"`
					} `yaml:"volumeMounts"`
					SecurityContext struct {
						RunAsUser                int64 `yaml:"runAsUser"`
						RunAsGroup               int64 `yaml:"runAsGroup"`
						AllowPrivilegeEscalation bool  `yaml:"allowPrivilegeEscalation"`
						ReadOnlyRootFilesystem   bool  `yaml:"readOnlyRootFilesystem"`
						Capabilities             struct {
							Drop []string `yaml:"drop"`
							Add  []string `yaml:"add"`
						} `yaml:"capabilities"`
					} `yaml:"securityContext"`
				} `yaml:"initContainers"`
				Containers []struct {
					Name            string `yaml:"name"`
					Image           string `yaml:"image"`
					ImagePullPolicy string `yaml:"imagePullPolicy"`
					Env             []struct {
						Name      string `yaml:"name"`
						Value     string `yaml:"value"`
						ValueFrom *struct {
							SecretKeyRef struct {
								Name     string `yaml:"name"`
								Key      string `yaml:"key"`
								Optional bool   `yaml:"optional"`
							} `yaml:"secretKeyRef"`
						} `yaml:"valueFrom"`
					} `yaml:"env"`
					VolumeMounts []struct {
						Name      string `yaml:"name"`
						MountPath string `yaml:"mountPath"`
						SubPath   string `yaml:"subPath"`
						ReadOnly  bool   `yaml:"readOnly"`
					} `yaml:"volumeMounts"`
				} `yaml:"containers"`
				Volumes []struct {
					Name                  string `yaml:"name"`
					PersistentVolumeClaim struct {
						ClaimName string `yaml:"claimName"`
					} `yaml:"persistentVolumeClaim"`
					Secret struct {
						SecretName string `yaml:"secretName"`
						Items      []struct {
							Key  string `yaml:"key"`
							Path string `yaml:"path"`
						} `yaml:"items"`
					} `yaml:"secret"`
				} `yaml:"volumes"`
			} `yaml:"spec"`
		} `yaml:"template"`
	} `yaml:"spec"`
}

func TestKubernetesAgentRuntime(t *testing.T) {
	raw, err := os.ReadFile("../deploy/k8s/ack-agent-runtime.yaml")
	if err != nil {
		t.Fatalf("read ACK agent runtime manifest: %v", err)
	}
	text := string(raw)
	docs := splitKubernetesYAMLDocuments(text)
	if len(docs) != 2 {
		t.Fatalf("ACK agent runtime manifest has %d YAML document(s), want ClusterRole and ClusterRoleBinding", len(docs))
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
		{kind: "ClusterRole", name: "creator-server-tokenreview"},
		{kind: "ClusterRoleBinding", name: "creator-server-tokenreview"},
	}
	for i, want := range wantKindsAndNames {
		if got := parsedDocs[i]; got.Kind != want.kind || got.Metadata.Name != want.name {
			t.Fatalf("ACK manifest doc %d = %s/%s, want %s/%s", i+1, got.Kind, got.Metadata.Name, want.kind, want.name)
		}
	}
	const renderedServiceAccount = "creator-custom-runner"
	renderedRuntimeDocs := splitKubernetesYAMLDocuments(strings.NewReplacer(
		"${agent_service_account}", renderedServiceAccount,
		"${namespace}", "anbanai-prod",
	).Replace(text))
	var renderedClusterBinding k8sManifestDoc
	if err := yaml.Unmarshal([]byte(renderedRuntimeDocs[1]), &renderedClusterBinding); err != nil {
		t.Fatalf("parse rendered ClusterRoleBinding: %v", err)
	}
	if len(renderedClusterBinding.Subjects) != 1 || renderedClusterBinding.Subjects[0].Name != renderedServiceAccount || renderedClusterBinding.Subjects[0].Namespace != "anbanai-prod" {
		t.Fatalf("rendered ClusterRoleBinding subject = %#v, want agent_service_account in anbanai-prod", renderedClusterBinding.Subjects)
	}
	clusterRole := parsedDocs[0]
	if !roleAllows(clusterRole, "tokenreviews", "create") || len(clusterRole.Rules) != 1 || !containsString(clusterRole.Rules[0].APIGroups, "authentication.k8s.io") {
		t.Fatalf("ClusterRole must only create TokenReviews: %#v", clusterRole.Rules)
	}
	binding := parsedDocs[1]
	if len(binding.Subjects) != 1 || binding.Subjects[0].Kind != "ServiceAccount" || binding.Subjects[0].Name != "${agent_service_account}" || binding.Subjects[0].Namespace != "${namespace}" {
		t.Fatalf("ClusterRoleBinding subject = %#v, want agent_service_account in template namespace", binding.Subjects)
	}
	if binding.RoleRef.Kind != "ClusterRole" || binding.RoleRef.Name != "creator-server-tokenreview" {
		t.Fatalf("ClusterRoleBinding roleRef = %#v, want ClusterRole/creator-server-tokenreview", binding.RoleRef)
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

	if !strings.Contains(text, "Server and one-shot Agent Jobs share creator-project-memory") {
		t.Fatalf("ACK agent runtime manifest must document shared project memory")
	}

	serverDeployment, err := os.ReadFile("Deployment.yaml")
	if err != nil {
		t.Fatalf("read server deployment: %v", err)
	}
	serverDocs := splitKubernetesYAMLDocuments(string(serverDeployment))
	if len(serverDocs) < 2 {
		t.Fatalf("server deployment manifest has %d documents, want PVC and Deployment", len(serverDocs))
	}
	var serverServiceAccount, serverRole, serverRoleBinding, serverTokenReviewRole, serverTokenReviewBinding k8sManifestDoc
	for _, rawDoc := range serverDocs {
		var doc k8sManifestDoc
		if err := yaml.Unmarshal([]byte(rawDoc), &doc); err != nil {
			t.Fatalf("parse server deployment document: %v", err)
		}
		switch {
		case doc.Kind == "ClusterRole" && doc.Metadata.Name == "creator-server-tokenreview":
			serverTokenReviewRole = doc
		case doc.Kind == "ClusterRoleBinding" && doc.Metadata.Name == "creator-server-tokenreview":
			serverTokenReviewBinding = doc
		case doc.Kind == "ServiceAccount" && doc.Metadata.Name == "${agent_service_account}":
			serverServiceAccount = doc
		case doc.Kind == "Role" && doc.Metadata.Name == "creator-agent-runner":
			serverRole = doc
		case doc.Kind == "RoleBinding" && doc.Metadata.Name == "creator-agent-runner":
			serverRoleBinding = doc
		}
	}
	if !roleAllows(serverTokenReviewRole, "tokenreviews", "create") || len(serverTokenReviewRole.Rules) != 1 {
		t.Fatalf("server deployment must reconcile the TokenReview ClusterRole: %#v", serverTokenReviewRole)
	}
	if len(serverTokenReviewBinding.Subjects) != 1 || serverTokenReviewBinding.Subjects[0].Kind != "ServiceAccount" || serverTokenReviewBinding.Subjects[0].Name != "${agent_service_account}" || serverTokenReviewBinding.Subjects[0].Namespace != "${namespace}" || serverTokenReviewBinding.RoleRef.Kind != "ClusterRole" || serverTokenReviewBinding.RoleRef.Name != "creator-server-tokenreview" {
		t.Fatalf("server deployment must bind TokenReview access to the configured ServiceAccount: %#v", serverTokenReviewBinding)
	}
	if serverServiceAccount.Kind != "ServiceAccount" {
		t.Fatal("server deployment must reconcile the Agent ServiceAccount")
	}
	if serverServiceAccount.Metadata.Namespace != "${namespace}" {
		t.Fatalf("server ServiceAccount namespace = %q, want ${namespace}", serverServiceAccount.Metadata.Namespace)
	}
	if !roleAllows(serverRole, "jobs", "get", "list", "watch", "create", "update", "delete") ||
		!roleAllows(serverRole, "persistentvolumeclaims", "get", "list", "create", "delete") ||
		!roleAllows(serverRole, "pods", "get", "list", "watch") ||
		!roleAllows(serverRole, "pods/attach", "create") ||
		!roleAllows(serverRole, "secrets", "list", "create", "patch", "delete") {
		t.Fatalf("server deployment Role must allow Pod and DSH Secret lifecycle management: %#v", serverRole.Rules)
	}
	if !roleAllows(serverRole, "events", "get", "list") {
		t.Fatalf("server deployment Role must allow read-only Pod event diagnostics: %#v", serverRole.Rules)
	}
	if roleAllows(serverRole, "secrets", "get") || roleAllows(serverRole, "secrets", "update") || roleAllows(serverRole, "events", "create") || roleAllows(serverRole, "pods/log", "get") || roleAllows(serverRole, "pods/exec", "create") || roleAllows(serverRole, "pods", "create", "delete") {
		t.Fatalf("server deployment Role grants unused sensitive or event permissions: %#v", serverRole.Rules)
	}
	if serverRoleBinding.Metadata.Namespace != "${namespace}" || len(serverRoleBinding.Subjects) != 1 || serverRoleBinding.Subjects[0].Kind != "ServiceAccount" || serverRoleBinding.Subjects[0].Name != "${agent_service_account}" || serverRoleBinding.Subjects[0].Namespace != "${namespace}" || serverRoleBinding.RoleRef.Kind != "Role" || serverRoleBinding.RoleRef.Name != "creator-agent-runner" {
		t.Fatalf("server deployment RoleBinding = %#v, want creator-agent-runner bound to agent_service_account", serverRoleBinding)
	}
	var serverPVCDoc string
	for _, rawDoc := range serverDocs {
		var doc k8sManifestDoc
		if err := yaml.Unmarshal([]byte(rawDoc), &doc); err != nil {
			t.Fatalf("parse server deployment document for PVC: %v", err)
		}
		if doc.Kind == "PersistentVolumeClaim" {
			serverPVCDoc = rawDoc
			break
		}
	}
	if !strings.Contains(serverPVCDoc, "kind: PersistentVolumeClaim") || !strings.Contains(serverPVCDoc, "storage: 20Gi") || !strings.Contains(serverPVCDoc, "ReadWriteMany") {
		t.Fatalf("server deployment must declare the shared 20Gi RWX project memory PVC")
	}
	var deployment deploymentDoc
	var serverDeploymentDoc string
	for _, rawDoc := range serverDocs {
		var doc k8sManifestDoc
		if err := yaml.Unmarshal([]byte(rawDoc), &doc); err != nil {
			t.Fatalf("parse server deployment document for Deployment: %v", err)
		}
		if doc.Kind == "Deployment" {
			serverDeploymentDoc = rawDoc
			break
		}
	}
	if err := yaml.Unmarshal([]byte(serverDeploymentDoc), &deployment); err != nil {
		t.Fatalf("parse server deployment: %v", err)
	}
	if strings.Contains(serverDeploymentDoc, "catalog_digest") || strings.Contains(serverDeploymentDoc, "agent-pack-catalog-digest") {
		t.Fatal("server Deployment template must not require the release Catalog digest as a cloud template variable")
	}
	if strings.Contains(serverDeploymentDoc, "name: ANBAN_AGENT_PACK_CATALOG_DIGEST") {
		t.Fatal("Server runtime must use its embedded Catalog digest instead of a deployment environment setting")
	}
	if deployment.Kind != "Deployment" {
		t.Fatalf("first server deployment doc kind = %q, want Deployment", deployment.Kind)
	}
	if len(deployment.Spec.Selector.MatchLabels) != 1 || deployment.Spec.Selector.MatchLabels["app"] != "${micro_service_name}" {
		t.Fatalf("server Deployment selector = %#v, want only the stable app label so existing Deployments remain updatable", deployment.Spec.Selector.MatchLabels)
	}
	if got := deployment.Spec.Template.Spec.ServiceAccountName; got != "${agent_service_account}" {
		t.Fatalf("server serviceAccountName = %q, want rendered agent_service_account", got)
	}
	var renderedDeployment deploymentDoc
	renderedServerDocs := splitKubernetesYAMLDocuments(strings.NewReplacer(
		"${agent_service_account}", renderedServiceAccount,
		"${namespace}", "anbanai-prod",
	).Replace(string(serverDeployment)))
	var renderedDeploymentDoc string
	for _, rawDoc := range renderedServerDocs {
		var doc k8sManifestDoc
		if err := yaml.Unmarshal([]byte(rawDoc), &doc); err != nil {
			t.Fatalf("parse rendered server document: %v", err)
		}
		if doc.Kind == "Deployment" {
			renderedDeploymentDoc = rawDoc
			break
		}
	}
	if err := yaml.Unmarshal([]byte(renderedDeploymentDoc), &renderedDeployment); err != nil {
		t.Fatalf("parse rendered server deployment: %v", err)
	}
	if renderedDeployment.Spec.Template.Spec.ServiceAccountName != renderedServiceAccount {
		t.Fatalf("rendered server serviceAccountName = %q, want %q", renderedDeployment.Spec.Template.Spec.ServiceAccountName, renderedServiceAccount)
	}
	if deployment.Spec.Template.Spec.AutomountServiceAccountToken == nil || !*deployment.Spec.Template.Spec.AutomountServiceAccountToken {
		t.Fatal("server Deployment must explicitly automount the ServiceAccount token for in-cluster Kubernetes API access")
	}
	if len(deployment.Spec.Template.Spec.InitContainers) != 1 {
		t.Fatalf("server initContainers = %#v, want one project-memory initializer", deployment.Spec.Template.Spec.InitContainers)
	}
	memoryInit := deployment.Spec.Template.Spec.InitContainers[0]
	if memoryInit.Name != "project-memory-init" || memoryInit.Image != "${image_repo}" {
		t.Fatalf("project memory initializer identity = %#v", memoryInit)
	}
	if memoryInit.ImagePullPolicy != "Always" {
		t.Fatalf("project memory initializer imagePullPolicy = %q, want Always", memoryInit.ImagePullPolicy)
	}
	initScript := strings.Join(append(memoryInit.Command, memoryInit.Args...), " ")
	for _, want := range []string{"mkdir -p /memory/projects", "chown 1000:1000 /memory /memory/projects", "chmod 0750 /memory /memory/projects"} {
		if !strings.Contains(initScript, want) {
			t.Fatalf("project memory initializer script %q missing %q", initScript, want)
		}
	}
	if memoryInit.SecurityContext.RunAsUser != 0 || memoryInit.SecurityContext.RunAsGroup != 0 || memoryInit.SecurityContext.AllowPrivilegeEscalation || !memoryInit.SecurityContext.ReadOnlyRootFilesystem {
		t.Fatalf("project memory initializer security context = %#v", memoryInit.SecurityContext)
	}
	if !containsString(memoryInit.SecurityContext.Capabilities.Drop, "ALL") || !containsString(memoryInit.SecurityContext.Capabilities.Add, "CHOWN") {
		t.Fatalf("project memory initializer capabilities = %#v", memoryInit.SecurityContext.Capabilities)
	}
	if len(memoryInit.VolumeMounts) != 1 || memoryInit.VolumeMounts[0].Name != "project-memory" || memoryInit.VolumeMounts[0].MountPath != "/memory" {
		t.Fatalf("project memory initializer mounts = %#v", memoryInit.VolumeMounts)
	}
	env := deploymentEnvMap(t, deployment)
	for name, want := range map[string]string{
		"ANBAN_AGENT_EXECUTOR":              "kubernetes",
		"ANBAN_CLAUDE_AGENT_SERVER_URL":     "https://${micro_service_name}-svc.${namespace}.svc.cluster.local:8443",
		"ANBAN_AGENT_NAMESPACE":             "${namespace}",
		"ANBAN_AGENT_IMAGE_WECHAT_ARTICLE":  "${wechat_article_agent_image_repo}",
		"ANBAN_AGENT_IMAGE_WECHAT_PICTURE":  "${wechat_picture_agent_image_repo}",
		"ANBAN_AGENT_IMAGE_SEEDNOTE":        "${seednote_agent_image_repo}",
		"ANBAN_AGENT_IMAGE_MONTAGE":         "${montage_agent_image_repo}",
		"ANBAN_AGENT_IMAGE_VIDEO_EDIT":      "${video_edit_agent_image_repo}",
		"ANBAN_AGENT_IMAGE_LOCAL_LIFE":      "${local_life_agent_image_repo}",
		"ANBAN_DSH_IMAGE":                   "${dsh_image_repo}",
		"ANBAN_AGENT_SERVICE_ACCOUNT":       "${agent_service_account}",
		"ANBAN_AGENT_SERVER_CA_SECRET":      "anban-internal-ca",
		"ANBAN_AGENT_IMAGE_PULL_SECRET":     "${imagePullSecret}",
		"ANBAN_AGENT_NAS_STORAGE_CLASS":     "nas-sc-creator",
		"ANBAN_AGENT_PROJECT_MEMORY_CLAIM":  "creator-project-memory",
		"ANBAN_AGENT_TASK_WORKSPACE_SIZE":   "10Gi",
		"ANBAN_MOONSHOT_ANTHROPIC_BASE_URL": "https://api.moonshot.cn/anthropic",
		"ANBAN_ZHIPU_ANTHROPIC_BASE_URL":    "https://open.bigmodel.cn/api/anthropic",
		"ANBAN_SERVER_TLS_CERT_FILE":        "/var/run/secrets/anban-server-tls/tls.crt",
		"ANBAN_SERVER_TLS_KEY_FILE":         "/var/run/secrets/anban-server-tls/tls.key",
		"SSL_CERT_FILE":                     "/var/run/secrets/anban-internal-ca/ca.crt",
		"NODE_EXTRA_CA_CERTS":               "/var/run/secrets/anban-internal-ca/ca.crt",
	} {
		if got := env[name]; got != want {
			t.Fatalf("server env %s = %q, want %q", name, got, want)
		}
	}
	var scribeSecretFound bool
	for _, envVar := range deployment.Spec.Template.Spec.Containers[0].Env {
		if envVar.Name == "VIDEO_EDIT_ELEVENLABS_API_KEY" && envVar.Value == "" && envVar.ValueFrom != nil && envVar.ValueFrom.SecretKeyRef.Name == "video-edit-elevenlabs" && envVar.ValueFrom.SecretKeyRef.Key == "api-key" {
			scribeSecretFound = true
		}
	}
	if !scribeSecretFound {
		t.Fatal("Server deployment must read the Scribe key from video-edit-elevenlabs/api-key")
	}
	for _, legacy := range []string{"ANBAN_CLAUDE_EXECUTOR", "ANBAN_ARTICLE_AGENT_IMAGE", "ANBAN_SEEDNOTE_AGENT_IMAGE", "ANBAN_MONTAGE_AGENT_IMAGE"} {
		if _, ok := env[legacy]; ok {
			t.Fatalf("server deployment retains legacy managed runtime env %s", legacy)
		}
	}
	for _, forbidden := range []string{"anban-agent-nas", "serviceAccountName: anban-server", "ANBAN_AGENT_MEMORY_STORAGE_CLASS", "ANBAN_AGENT_MEMORY_SIZE"} {
		if strings.Contains(string(serverDeployment), forbidden) {
			t.Fatalf("server deployment must not include obsolete value %q", forbidden)
		}
	}
	container := deployment.Spec.Template.Spec.Containers[0]
	if container.Image != "${image_repo}" || container.ImagePullPolicy != "Always" {
		t.Fatalf("server image/pull policy = %q/%q, want ${image_repo}/Always", container.Image, container.ImagePullPolicy)
	}
	configMounts := 0
	for _, mount := range container.VolumeMounts {
		if mount.MountPath == "/app/conf" {
			t.Fatal("server config must not mount the whole /app/conf directory because it hides the image billing catalog")
		}
		if mount.MountPath == "/app/conf/config.yaml" {
			configMounts++
			if mount.SubPath != "config.yaml" || !mount.ReadOnly {
				t.Fatalf("server config mount = %+v, want readOnly subPath config.yaml", mount)
			}
		}
	}
	if configMounts != 1 {
		t.Fatalf("server config file mounts = %d, want exactly one", configMounts)
	}
	memoryMounts := 0
	for _, mount := range container.VolumeMounts {
		if mount.Name == "project-memory" && mount.MountPath == "/app/data/project-memory" && !mount.ReadOnly {
			memoryMounts++
		}
	}
	if memoryMounts != 1 {
		t.Fatalf("server project memory mounts = %d, want one read-write mount", memoryMounts)
	}
	assertDeploymentSecretEnv(t, deployment, "ANBAN_BILLING_ADMIN_API_KEY", "anban-billing-admin-api-key", "api-key")
	assertDeploymentSecretEnv(t, deployment, "ANBAN_AGENT_EXECUTION_TOKEN_SECRET", "anban-agent-execution-token", "token-secret")
	assertOptionalDeploymentSecretEnv(t, deployment, "ANBAN_DEEPSEEK_SEARCH_API_KEY", "anban-agent-profile-providers", "deepseek-search-api-key")
	assertOptionalDeploymentSecretEnv(t, deployment, "ANBAN_DOUBAO_SEARCH_API_KEY", "anban-agent-profile-providers", "doubao-search-api-key")
	for envName, secretKey := range map[string]string{
		"ANBAN_DEEPSEEK_ANTHROPIC_BASE_URL": "deepseek-anthropic-base-url",
		"ANBAN_DEEPSEEK_API_KEY":            "deepseek-api-key",
		"ANBAN_MOONSHOT_API_KEY":            "moonshot-api-key",
		"ANBAN_ZHIPU_API_KEY":               "zhipu-api-key",
	} {
		assertDeploymentSecretEnv(t, deployment, envName, "anban-agent-profile-providers", secretKey)
	}
	for _, obsolete := range []string{"ANBAN_KIMI_API_KEY", "ANBAN_DOUBAO_AGENT_BASE_URL", "ANBAN_DOUBAO_AGENT_API_KEY"} {
		if _, ok := env[obsolete]; ok {
			t.Fatalf("server deployment retains removed %s", obsolete)
		}
	}
	assertDeploymentTLSVolume(t, deployment, "anban-server-tls", "/var/run/secrets/anban-server-tls", "anban-server-tls", []string{"tls.crt", "tls.key"})
	assertDeploymentTLSVolume(t, deployment, "anban-internal-ca", "/var/run/secrets/anban-internal-ca", "anban-internal-ca", []string{"ca.crt"})

	configExample, err := os.ReadFile("config.example.yaml")
	if err != nil {
		t.Fatalf("read config example: %v", err)
	}
	for _, body := range []struct {
		name string
		text string
	}{
		{name: "config.example.yaml", text: string(configExample)},
	} {
		for _, want := range []string{
			`executor: "${ANBAN_AGENT_EXECUTOR}"`,
			`wechat-article: "${ANBAN_AGENT_IMAGE_WECHAT_ARTICLE:-creator-agent-wechat-article:latest}"`,
			`wechat-picture: "${ANBAN_AGENT_IMAGE_WECHAT_PICTURE:-creator-agent-wechat-picture:latest}"`,
			`seednote: "${ANBAN_AGENT_IMAGE_SEEDNOTE:-creator-agent-seednote:latest}"`,
			`montage: "${ANBAN_AGENT_IMAGE_MONTAGE:-creator-agent-montage:latest}"`,
			`video-edit: "${ANBAN_AGENT_IMAGE_VIDEO_EDIT:-creator-agent-video-edit:latest}"`,
			`service_account: "${ANBAN_AGENT_SERVICE_ACCOUNT:-creator-agent-runner}"`,
			`kubernetes_namespace: "${ANBAN_AGENT_NAMESPACE:-anbanai-prod}"`,
			`kubernetes_image_pull_secret: "${ANBAN_AGENT_IMAGE_PULL_SECRET:-acr-secret}"`,
			`kubernetes_project_memory_claim: "${ANBAN_AGENT_PROJECT_MEMORY_CLAIM:-creator-project-memory}"`,
			`server_ca_secret: "${ANBAN_AGENT_SERVER_CA_SECRET:-anban-internal-ca}"`,
			`execution_token_secret: "${ANBAN_AGENT_EXECUTION_TOKEN_SECRET}"`,
		} {
			if !strings.Contains(body.text, want) {
				t.Fatalf("%s must include Kubernetes config %q", body.name, want)
			}
		}
	}
	for _, forbidden := range []string{"workspace_mount_path", "workspace_pvc_name", "pod_revision", "pod_ttl_seconds", "exec_timeout_seconds"} {
		if strings.Contains(string(configExample), forbidden) {
			t.Fatalf("obsolete reusable-Pod config key %q remains", forbidden)
		}
	}
	for _, want := range []string{
		"port: 8443",
		"targetPort: 8080",
		"secretName: anban-server-tls",
		"mountPath: /var/run/secrets/anban-server-tls",
		"secretName: anban-internal-ca",
		"mountPath: /var/run/secrets/anban-internal-ca",
	} {
		if !strings.Contains(string(serverDeployment), want) {
			t.Fatalf("server deployment missing TLS contract %q", want)
		}
	}
}

func TestACKRenderIncludesTokenReviewRBAC(t *testing.T) {
	if _, err := exec.LookPath("envsubst"); err != nil {
		t.Skipf("envsubst is required by the ACK renderer: %v", err)
	}
	output := filepath.Join(t.TempDir(), "server-release.yaml")
	cmd := exec.Command("bash", filepath.Join("..", "scripts", "render-ack-server-deployment.sh"), "--output", output)
	cmd.Env = append(os.Environ(), ackRenderEnv(filepath.Join(t.TempDir(), "missing-manifest.json"))...)
	if outputBytes, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("render ACK release: %v\n%s", err, outputBytes)
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	docs := splitKubernetesYAMLDocuments(string(raw))
	if len(docs) < 4 {
		t.Fatalf("combined ACK release has %d documents, want cluster RBAC before Server resources", len(docs))
	}
	var clusterRole, binding k8sManifestDoc
	if err := yaml.Unmarshal([]byte(docs[0]), &clusterRole); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal([]byte(docs[1]), &binding); err != nil {
		t.Fatal(err)
	}
	if clusterRole.Kind != "ClusterRole" || clusterRole.Metadata.Name != "creator-server-tokenreview" || !roleAllows(clusterRole, "tokenreviews", "create") {
		t.Fatalf("first ACK release resource = %#v, want TokenReview ClusterRole", clusterRole)
	}
	if binding.Kind != "ClusterRoleBinding" || binding.Metadata.Name != "creator-server-tokenreview" || len(binding.Subjects) != 1 || binding.Subjects[0].Name != "creator-agent-runner" || binding.Subjects[0].Namespace != "anbanai-prod" {
		t.Fatalf("second ACK release resource = %#v, want rendered TokenReview binding", binding)
	}
	if strings.Contains(string(raw), "${") {
		t.Fatal("combined ACK release contains unresolved template variables")
	}
	for index, document := range docs {
		var parsed map[string]any
		if err := yaml.Unmarshal([]byte(document), &parsed); err != nil {
			t.Fatalf("combined ACK release document %d is not valid YAML: %v", index+1, err)
		}
	}
}

func TestACKRenderLoadsVideoEditImageFromReleaseManifest(t *testing.T) {
	if _, err := exec.LookPath("envsubst"); err != nil {
		t.Skipf("envsubst is required by the ACK renderer: %v", err)
	}
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skipf("jq is required by the ACK renderer: %v", err)
	}
	catalog, err := os.ReadFile(filepath.Join("agentpack", "catalog.generated.json"))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(catalog)
	const videoEditImage = "registry.example.com/creator-video-edit-from-manifest:release"
	manifest := filepath.Join(t.TempDir(), "agent-runtime-manifest.json")
	manifestBody := `{"schema_version":1,"catalog_digest":"` + hex.EncodeToString(digest[:]) + `","images":{"video-edit":"` + videoEditImage + `"}}`
	if err := os.WriteFile(manifest, []byte(manifestBody), 0o600); err != nil {
		t.Fatal(err)
	}

	env := ackRenderEnv(manifest)
	for index, value := range env {
		if strings.HasPrefix(value, "video_edit_agent_image_repo=") {
			env[index] = "video_edit_agent_image_repo="
		}
	}
	output := filepath.Join(t.TempDir(), "server-release.yaml")
	cmd := exec.Command("bash", filepath.Join("..", "scripts", "render-ack-server-deployment.sh"), "--output", output)
	cmdEnv := make([]string, 0, len(os.Environ())+len(env))
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "video_edit_agent_image_repo=") {
			cmdEnv = append(cmdEnv, value)
		}
	}
	cmd.Env = append(cmdEnv, env...)
	if outputBytes, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("render ACK release with video-edit image in manifest: %v\n%s", err, outputBytes)
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	for _, document := range splitKubernetesYAMLDocuments(string(raw)) {
		var deployment deploymentDoc
		if err := yaml.Unmarshal([]byte(document), &deployment); err != nil {
			t.Fatalf("parse rendered ACK release document: %v", err)
		}
		if deployment.Kind == "Deployment" {
			if got := deploymentEnvMap(t, deployment)["ANBAN_AGENT_IMAGE_VIDEO_EDIT"]; got != videoEditImage {
				t.Fatalf("rendered ANBAN_AGENT_IMAGE_VIDEO_EDIT = %q, want manifest image %q", got, videoEditImage)
			}
			return
		}
	}
	t.Fatal("rendered ACK release does not contain the Server Deployment")
}

func TestACKRenderRejectsStaleCatalogManifest(t *testing.T) {
	if _, err := exec.LookPath("envsubst"); err != nil {
		t.Skipf("envsubst is required by the ACK renderer: %v", err)
	}
	manifest := filepath.Join(t.TempDir(), "stale-manifest.json")
	if err := os.WriteFile(manifest, []byte(`{"schema_version":1,"catalog_digest":"0000000000000000000000000000000000000000000000000000000000000000","images":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", filepath.Join("..", "scripts", "render-ack-server-deployment.sh"))
	cmd.Env = append(os.Environ(), ackRenderEnv(manifest)...)
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "release manifest Catalog digest does not match Server Catalog") {
		t.Fatalf("stale Catalog manifest result = %v, output = %s", err, output)
	}
}

func TestACKRenderRejectsMalformedDshImageReference(t *testing.T) {
	if _, err := exec.LookPath("envsubst"); err != nil {
		t.Skipf("envsubst is required by the ACK renderer: %v", err)
	}
	env := ackRenderEnv(filepath.Join(t.TempDir(), "missing-manifest.json"))
	for index, value := range env {
		if strings.HasPrefix(value, "dsh_image_repo=") {
			env[index] = "dsh_image_repo=registry.example.com/runtime-dsh: broken@sha256:" + strings.Repeat("a", 64)
		}
	}
	cmd := exec.Command("bash", filepath.Join("..", "scripts", "render-ack-server-deployment.sh"))
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "dsh_image_repo must be a valid container image reference without whitespace") {
		t.Fatalf("malformed DSH image render result = %v, output = %s", err, output)
	}
}

func TestAgentRuntimeReleaseRejectsMalformedDshImageReference(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skipf("jq is required by the runtime release script: %v", err)
	}
	manifest := filepath.Join(t.TempDir(), "manifest.json")
	env := []string{
		"SERVER_IMAGE=registry.example.com/server:release",
		"AGENT_IMAGE_WECHAT_ARTICLE=registry.example.com/article:release",
		"AGENT_IMAGE_WECHAT_PICTURE=registry.example.com/picture:release",
		"AGENT_IMAGE_SEEDNOTE=registry.example.com/seednote:release",
		"AGENT_IMAGE_MONTAGE=registry.example.com/montage:release",
		"AGENT_IMAGE_MOMENTS=registry.example.com/moments:release",
		"AGENT_IMAGE_ECOMMERCE=registry.example.com/ecommerce:release",
		"AGENT_IMAGE_PROFILE_ANALYSIS=registry.example.com/profile-analysis:release",
		"AGENT_IMAGE_FEEDBACK_ANALYSIS=registry.example.com/feedback-analysis:release",
		"AGENT_IMAGE_WHITEBOARD_ANIMATION=registry.example.com/whiteboard-animation:release",
		"AGENT_IMAGE_HYPIT=registry.example.com/hypit:release",
		"DSH_IMAGE=registry.example.com/runtime-dsh: broken@sha256:" + strings.Repeat("a", 64),
		"AGENT_RUNTIME_RELEASE_MANIFEST=" + manifest,
	}
	cmd := exec.Command("bash", filepath.Join("..", "scripts", "agent-runtime-release.sh"))
	cmd.Env = append(os.Environ(), "AGENT_RUNTIME_CATALOG_PATH="+filepath.Join("agentpack", "catalog.generated.json"))
	cmd.Env = append(cmd.Env, env...)
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "DSH_IMAGE must be a valid container image reference without whitespace") {
		t.Fatalf("malformed DSH image release result = %v, output = %s", err, output)
	}
	if _, err := os.Stat(manifest); !os.IsNotExist(err) {
		t.Fatalf("malformed DSH image wrote release manifest, stat error = %v", err)
	}
}

func TestDshImageReferenceShellValidator(t *testing.T) {
	validator := filepath.Join("..", "scripts", "lib", "dsh-image-reference.sh")
	for _, test := range []struct {
		name  string
		image string
		valid bool
	}{
		{name: "digest only", image: "registry.example.com/runtime-dsh@sha256:" + strings.Repeat("a", 64), valid: true},
		{name: "bare image name", image: "runtime-dsh", valid: true},
		{name: "commit tag", image: "crpi-671lgv9lrdyll4t9.cn-chengdu.personal.cr.aliyuncs.com/bx_anbanai/runtime-dsh:e5e395c067ef3fe548347a1e61f292bcdd96dc9b", valid: true},
		{name: "release tag", image: "registry.example.com/runtime-dsh:release", valid: true},
		{name: "tag and digest", image: "registry.example.com/runtime-dsh:release@sha256:" + strings.Repeat("b", 64), valid: true},
		{name: "reported whitespace", image: "registry.example.com/runtime-dsh: release@sha256:" + strings.Repeat("c", 64)},
		{name: "invalid repository separator", image: "registry.example.com/runtime..dsh:latest"},
		{name: "short digest", image: "registry.example.com/runtime-dsh@sha256:abc"},
		{name: "short sha256 digest", image: "registry.example.com/runtime-dsh@sha256:" + strings.Repeat("a", 32)},
		{name: "sha512 digest", image: "registry.example.com/runtime-dsh@sha512:" + strings.Repeat("a", 128), valid: true},
		{name: "uppercase registry domain", image: "REGISTRY.example.com/runtime-dsh:release", valid: true},
		{name: "ipv6 registry domain", image: "[2001:db8::1]/runtime-dsh:release", valid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			cmd := exec.Command("bash", "-c", `source "$1" && is_valid_dsh_image_reference "$2"`, "bash", validator, test.image)
			err := cmd.Run()
			if test.valid && err != nil {
				t.Fatalf("validator rejected valid image %q: %v", test.image, err)
			}
			if !test.valid && err == nil {
				t.Fatalf("validator accepted invalid image %q", test.image)
			}
		})
	}
}

func ackRenderEnv(manifestPath string) []string {
	return []string{
		"AGENT_RUNTIME_RELEASE_MANIFEST=" + manifestPath,
		"namespace=anbanai-prod",
		"agent_service_account=creator-agent-runner",
		"image_repo=registry.example.com/creator-server:release",
		"wechat_article_agent_image_repo=registry.example.com/creator-article:release",
		"wechat_picture_agent_image_repo=registry.example.com/creator-picture:release",
		"seednote_agent_image_repo=registry.example.com/creator-seednote:release",
		"montage_agent_image_repo=registry.example.com/creator-montage:release",
		"video_edit_agent_image_repo=registry.example.com/creator-video-edit:release",
		"moments_agent_image_repo=registry.example.com/creator-moments:release",
		"ecommerce_agent_image_repo=registry.example.com/creator-ecommerce:release",
		"profile_analysis_agent_image_repo=registry.example.com/creator-profile-analysis:release",
		"feedback_analysis_agent_image_repo=registry.example.com/creator-feedback-analysis:release",
		"whiteboard_animation_agent_image_repo=registry.example.com/creator-whiteboard-animation:release",
		"hypit_agent_image_repo=registry.example.com/creator-hypit:release",
		"local_life_agent_image_repo=registry.example.com/creator-local-life:release",
		"dsh_image_repo=registry.example.com/runtime-dsh@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"micro_service_name=creator-api",
		"version_switch=base",
		// A colon is legal in a log-store value and must not turn the rendered
		// `value:` scalar into a YAML mapping.
		"log_store=creator:prod",
		"requests_cpu=500m",
		"requests_memory=1Gi",
		"limits_cpu=2",
		"limits_memory=4Gi",
		"imagePullSecret=acr-secret",
		"configmap_name=creator-prod",
	}
}

func assertDeploymentSecretEnv(t *testing.T, doc deploymentDoc, envName, secretName, secretKey string) {
	t.Helper()
	if len(doc.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("server deployment containers = %d, want 1", len(doc.Spec.Template.Spec.Containers))
	}
	for _, env := range doc.Spec.Template.Spec.Containers[0].Env {
		if env.Name != envName {
			continue
		}
		if env.Value != "" || env.ValueFrom == nil || env.ValueFrom.SecretKeyRef.Name != secretName || env.ValueFrom.SecretKeyRef.Key != secretKey {
			t.Fatalf("server env %s = %+v, want secret %s key %s", envName, env, secretName, secretKey)
		}
		return
	}
	t.Fatalf("server deployment missing secret env %s", envName)
}

func assertOptionalDeploymentSecretEnv(t *testing.T, doc deploymentDoc, envName, secretName, secretKey string) {
	t.Helper()
	if len(doc.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("server deployment containers = %d, want 1", len(doc.Spec.Template.Spec.Containers))
	}
	for _, env := range doc.Spec.Template.Spec.Containers[0].Env {
		if env.Name != envName {
			continue
		}
		if env.Value != "" || env.ValueFrom == nil || env.ValueFrom.SecretKeyRef.Name != secretName || env.ValueFrom.SecretKeyRef.Key != secretKey || !env.ValueFrom.SecretKeyRef.Optional {
			t.Fatalf("server env %s = %+v, want optional secret %s key %s", envName, env, secretName, secretKey)
		}
		return
	}
	t.Fatalf("server deployment missing optional secret env %s", envName)
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

func assertDeploymentTLSVolume(t *testing.T, doc deploymentDoc, volumeName, mountPath, secretName string, keys []string) {
	t.Helper()
	container := doc.Spec.Template.Spec.Containers[0]
	foundMount := false
	for _, mount := range container.VolumeMounts {
		if mount.Name == volumeName {
			foundMount = mount.MountPath == mountPath && mount.ReadOnly
			break
		}
	}
	if !foundMount {
		t.Fatalf("server volume mount %q must be read-only at %q", volumeName, mountPath)
	}
	for _, volume := range doc.Spec.Template.Spec.Volumes {
		if volume.Name != volumeName {
			continue
		}
		if volume.Secret.SecretName != secretName {
			t.Fatalf("server volume %q secret = %q, want %q", volumeName, volume.Secret.SecretName, secretName)
		}
		gotKeys := make([]string, 0, len(volume.Secret.Items))
		for _, item := range volume.Secret.Items {
			if item.Key != item.Path {
				t.Fatalf("server volume %q item %q path = %q, want same name", volumeName, item.Key, item.Path)
			}
			gotKeys = append(gotKeys, item.Key)
		}
		if strings.Join(gotKeys, ",") != strings.Join(keys, ",") {
			t.Fatalf("server volume %q keys = %v, want %v", volumeName, gotKeys, keys)
		}
		return
	}
	t.Fatalf("server volume %q is missing", volumeName)
}

func splitKubernetesYAMLDocuments(text string) []string {
	var docs []string
	for _, part := range strings.Split(text, "\n---") {
		part = strings.TrimSpace(part)
		for strings.HasPrefix(part, "#") {
			if newline := strings.IndexByte(part, '\n'); newline >= 0 {
				part = strings.TrimSpace(part[newline+1:])
			} else {
				part = ""
			}
		}
		if part == "" {
			continue
		}
		docs = append(docs, part)
	}
	return docs
}
