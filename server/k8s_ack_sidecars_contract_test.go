package main

import (
	"os"
	"reflect"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/yaml"
)

const (
	ackSidecarsTestNamespace       = "anbanai-test"
	ackSidecarsTestIlinkImage      = "registry.cn-hangzhou.aliyuncs.com/anban/sidecar-ilink:latest"
	ackSidecarsTestSeednoteImage   = "registry.cn-hangzhou.aliyuncs.com/anban/sidecar-seednote:latest"
	ackSidecarsTestImagePullSecret = "anban-acr-pull"
	ackSidecarsTestIlinkStorage    = "10Gi"
	ackSidecarsTestSeednoteStorage = "10Gi"
	ackSidecarsTestServerAppLabel  = "anban-creator-server"
)

func TestACKIlinkManifestContract(t *testing.T) {
	raw := readACKSidecarsManifest(t, "../deploy/k8s/ack-sidecar-ilink.yaml")
	assertACKIlinkRawTemplateContract(t, raw)
	documents := splitACKSidecarsManifest(t, renderACKSidecarsManifest(raw))
	if len(documents) != 3 {
		t.Fatalf("ACK iLink manifest must contain exactly 3 documents, got %d", len(documents))
	}
	var ilinkPVC corev1.PersistentVolumeClaim
	strictUnmarshalACKSidecarsDocument(t, documents[0], &ilinkPVC)
	assertACKSidecarsObject(t, ilinkPVC.TypeMeta, ilinkPVC.ObjectMeta, "PersistentVolumeClaim", "sidecar-ilink-state", "sidecar-ilink")
	assertACKSidecarsPVC(t, ilinkPVC, ackSidecarsTestIlinkStorage)

	var ilinkDeployment appsv1.Deployment
	strictUnmarshalACKSidecarsDocument(t, documents[1], &ilinkDeployment)
	assertACKSidecarsObject(t, ilinkDeployment.TypeMeta, ilinkDeployment.ObjectMeta, "Deployment", "sidecar-ilink", "sidecar-ilink")
	assertACKSidecarsDeployment(t, ilinkDeployment, "sidecar-ilink")
	assertIlinkPodSpec(t, ilinkDeployment.Spec.Template.Spec)

	var ilinkService corev1.Service
	strictUnmarshalACKSidecarsDocument(t, documents[2], &ilinkService)
	assertACKSidecarsObject(t, ilinkService.TypeMeta, ilinkService.ObjectMeta, "Service", "sidecar-ilink", "sidecar-ilink")
	assertACKSidecarsService(t, ilinkService, "sidecar-ilink", 18070)
}

func TestACKSeednoteManifestContract(t *testing.T) {
	raw := readACKSidecarsManifest(t, "../deploy/k8s/ack-sidecar-seednote.yaml")
	assertACKSeednoteRawTemplateContract(t, raw)
	documents := splitACKSidecarsManifest(t, renderACKSidecarsManifest(raw))
	if len(documents) != 4 {
		t.Fatalf("ACK Seednote manifest must contain exactly 4 documents, got %d", len(documents))
	}
	var seednotePVC corev1.PersistentVolumeClaim
	strictUnmarshalACKSidecarsDocument(t, documents[0], &seednotePVC)
	assertACKSidecarsObject(t, seednotePVC.TypeMeta, seednotePVC.ObjectMeta, "PersistentVolumeClaim", "sidecar-seednote-data", "sidecar-seednote")
	assertACKSidecarsPVC(t, seednotePVC, ackSidecarsTestSeednoteStorage)

	var seednoteDeployment appsv1.Deployment
	strictUnmarshalACKSidecarsDocument(t, documents[1], &seednoteDeployment)
	assertACKSidecarsObject(t, seednoteDeployment.TypeMeta, seednoteDeployment.ObjectMeta, "Deployment", "sidecar-seednote", "sidecar-seednote")
	assertACKSidecarsDeployment(t, seednoteDeployment, "sidecar-seednote")
	assertSeednoteACKPodSpec(t, seednoteDeployment.Spec.Template.Spec)

	var seednoteService corev1.Service
	strictUnmarshalACKSidecarsDocument(t, documents[2], &seednoteService)
	assertACKSidecarsObject(t, seednoteService.TypeMeta, seednoteService.ObjectMeta, "Service", "sidecar-seednote", "sidecar-seednote")
	assertACKSidecarsService(t, seednoteService, "sidecar-seednote", 18060)

	var seednotePolicy networkingv1.NetworkPolicy
	strictUnmarshalACKSidecarsDocument(t, documents[3], &seednotePolicy)
	assertACKSidecarsObject(t, seednotePolicy.TypeMeta, seednotePolicy.ObjectMeta, "NetworkPolicy", "sidecar-seednote-server-only", "sidecar-seednote")
	if !reflect.DeepEqual(seednotePolicy.Spec.PodSelector.MatchLabels, map[string]string{"app": "sidecar-seednote"}) {
		t.Fatalf("NetworkPolicy pod selector = %v, want app=sidecar-seednote", seednotePolicy.Spec.PodSelector.MatchLabels)
	}
	if !reflect.DeepEqual(seednotePolicy.Spec.PolicyTypes, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}) || len(seednotePolicy.Spec.Ingress) != 1 {
		t.Fatalf("NetworkPolicy types/ingress = %v/%v, want one Ingress rule", seednotePolicy.Spec.PolicyTypes, seednotePolicy.Spec.Ingress)
	}
	rule := seednotePolicy.Spec.Ingress[0]
	if len(rule.From) != 1 || rule.From[0].PodSelector == nil || !reflect.DeepEqual(rule.From[0].PodSelector.MatchLabels, map[string]string{"app": ackSidecarsTestServerAppLabel}) {
		t.Fatalf("NetworkPolicy ingress source = %v, want server app label", rule.From)
	}
	if len(rule.Ports) != 1 || rule.Ports[0].Port == nil || rule.Ports[0].Port.IntVal != 18060 || rule.Ports[0].Protocol == nil || *rule.Ports[0].Protocol != corev1.ProtocolTCP {
		t.Fatalf("NetworkPolicy ingress ports = %v, want TCP/18060", rule.Ports)
	}
}

func readACKSidecarsManifest(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ACK sidecar manifest %s: %v", path, err)
	}
	return string(raw)
}

func assertACKIlinkRawTemplateContract(t *testing.T, raw string) {
	t.Helper()
	for placeholder, wantCount := range map[string]int{
		"${namespace}": 3, "${sidecar_ilink_storage_size}": 1, "${sidecar_ilink_image_repo}": 1, "${imagePullSecret}": 1,
	} {
		if gotCount := strings.Count(raw, placeholder); gotCount != wantCount {
			t.Fatalf("raw ACK iLink template placeholder %q occurs %d times, want %d", placeholder, gotCount, wantCount)
		}
	}
}

func assertACKSeednoteRawTemplateContract(t *testing.T, raw string) {
	t.Helper()
	for placeholder, wantCount := range map[string]int{
		"${namespace}":                     4,
		"${sidecar_seednote_storage_size}": 1,
		"${sidecar_seednote_image_repo}":   1,
		"${imagePullSecret}":               1,
		"${server_app_label}":              1,
	} {
		if gotCount := strings.Count(raw, placeholder); gotCount != wantCount {
			t.Fatalf("raw ACK sidecars template placeholder %q occurs %d times, want %d", placeholder, gotCount, wantCount)
		}
	}
	for _, obsoletePath := range []string{
		"../deploy/k8s/ack-sidecars.yaml",
		"../deploy/k8s/ack-wcflink.yaml",
		"../deploy/k8s/ack-seednote.yaml",
		"../deploy/k8s/ack-seednote.md",
		"../docs/superpowers/specs/2026-07-20-ack-seednote-standalone-design.md",
		"../docs/superpowers/plans/2026-07-20-ack-seednote-standalone.md",
	} {
		if _, err := os.Stat(obsoletePath); err == nil {
			t.Fatalf("obsolete standalone ACK Seednote file still exists: %s", obsoletePath)
		} else if !os.IsNotExist(err) {
			t.Fatalf("stat obsolete standalone ACK Seednote file %s: %v", obsoletePath, err)
		}
	}
}

func renderACKSidecarsManifest(raw string) string {
	return strings.NewReplacer(
		"${namespace}", ackSidecarsTestNamespace,
		"${imagePullSecret}", ackSidecarsTestImagePullSecret,
		"${sidecar_ilink_image_repo}", ackSidecarsTestIlinkImage,
		"${sidecar_seednote_image_repo}", ackSidecarsTestSeednoteImage,
		"${sidecar_ilink_storage_size}", ackSidecarsTestIlinkStorage,
		"${sidecar_seednote_storage_size}", ackSidecarsTestSeednoteStorage,
		"${server_app_label}", ackSidecarsTestServerAppLabel,
	).Replace(raw)
}

func splitACKSidecarsManifest(t *testing.T, raw string) []string {
	t.Helper()
	documents := strings.Split(strings.TrimSpace(raw), "\n---\n")
	for index, document := range documents {
		if strings.TrimSpace(document) == "" {
			t.Fatalf("ACK sidecars manifest contains empty document %d", index+1)
		}
	}
	return documents
}

func strictUnmarshalACKSidecarsDocument(t *testing.T, document string, target any) {
	t.Helper()
	if err := yaml.UnmarshalStrict([]byte(document), target); err != nil {
		t.Fatalf("strictly decode ACK sidecars document: %v", err)
	}
}

func assertACKSidecarsObject(t *testing.T, typeMeta metav1.TypeMeta, objectMeta metav1.ObjectMeta, wantKind, wantName, app string) {
	t.Helper()
	if typeMeta.APIVersion != apiVersionForKind(wantKind) || typeMeta.Kind != wantKind || objectMeta.Name != wantName || objectMeta.Namespace != ackSidecarsTestNamespace {
		t.Fatalf("object = %s %s %s/%s, want %s %s %s/%s", typeMeta.APIVersion, typeMeta.Kind, objectMeta.Namespace, objectMeta.Name, apiVersionForKind(wantKind), wantKind, ackSidecarsTestNamespace, wantName)
	}
	if !reflect.DeepEqual(objectMeta.Labels, map[string]string{"app": app}) {
		t.Fatalf("object labels = %v, want app=%s", objectMeta.Labels, app)
	}
}

func apiVersionForKind(kind string) string {
	if kind == "Deployment" {
		return "apps/v1"
	}
	if kind == "NetworkPolicy" {
		return "networking.k8s.io/v1"
	}
	return "v1"
}

func assertACKSidecarsPVC(t *testing.T, pvc corev1.PersistentVolumeClaim, storage string) {
	t.Helper()
	if pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName != "nas-sc-creator" {
		t.Fatalf("PVC storage class = %v, want nas-sc-creator", pvc.Spec.StorageClassName)
	}
	if !reflect.DeepEqual(pvc.Spec.AccessModes, []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}) {
		t.Fatalf("PVC access modes = %v, want [ReadWriteOnce]", pvc.Spec.AccessModes)
	}
	assertACKSidecarsResources(t, "PVC storage requests", pvc.Spec.Resources.Requests, corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(storage)})
}

func assertACKSidecarsDeployment(t *testing.T, deployment appsv1.Deployment, app string) {
	t.Helper()
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 || deployment.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType {
		t.Fatalf("deployment replicas/strategy = %v/%s, want 1/Recreate", deployment.Spec.Replicas, deployment.Spec.Strategy.Type)
	}
	wantLabels := map[string]string{"app": app}
	if deployment.Spec.Selector == nil || !reflect.DeepEqual(deployment.Spec.Selector.MatchLabels, wantLabels) {
		t.Fatalf("deployment selector = %v, want %v", deployment.Spec.Selector, wantLabels)
	}
	if !reflect.DeepEqual(deployment.Spec.Template.Labels, wantLabels) {
		t.Fatalf("pod template labels = %v, want %v", deployment.Spec.Template.Labels, wantLabels)
	}
}

func assertACKSidecarsPodBase(t *testing.T, pod corev1.PodSpec, wantPullSecrets []corev1.LocalObjectReference) {
	t.Helper()
	if pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken {
		t.Fatal("pod must set automountServiceAccountToken to false")
	}
	if pod.HostNetwork {
		t.Fatal("pod hostNetwork must be false")
	}
	if !reflect.DeepEqual(pod.NodeSelector, map[string]string{"kubernetes.io/arch": "amd64"}) {
		t.Fatalf("pod node selector = %v, want kubernetes.io/arch=amd64", pod.NodeSelector)
	}
	if !reflect.DeepEqual(pod.ImagePullSecrets, wantPullSecrets) {
		t.Fatalf("pod imagePullSecrets = %v, want %v", pod.ImagePullSecrets, wantPullSecrets)
	}
	if len(pod.Containers) != 1 {
		t.Fatalf("pod containers = %d, want 1", len(pod.Containers))
	}
}

func assertIlinkPodSpec(t *testing.T, pod corev1.PodSpec) {
	t.Helper()
	assertACKSidecarsPodBase(t, pod, []corev1.LocalObjectReference{{Name: ackSidecarsTestImagePullSecret}})
	container := pod.Containers[0]
	if container.Name != "sidecar-ilink" || container.Image != ackSidecarsTestIlinkImage || container.ImagePullPolicy != corev1.PullAlways {
		t.Fatalf("container name/image/pull policy = %s/%s/%s", container.Name, container.Image, container.ImagePullPolicy)
	}
	assertACKSidecarsPort(t, container, 18070)
	assertACKSidecarsEnv(t, container.Env, map[string]string{
		"WCFLINK_LISTEN_ADDR": ":18070", "WCFLINK_STATE_DIR": "/app/state", "WCFLINK_DB_PATH": "/app/state/wcf.db", "WCFLINK_LOG_LEVEL": "info",
	})
	assertACKSidecarsProbe(t, "readiness", container.ReadinessProbe, "/health/ready", 18070, ackSidecarsProbeTiming{initialDelaySeconds: 5, periodSeconds: 10, timeoutSeconds: 3, failureThreshold: 6})
	assertACKSidecarsProbe(t, "liveness", container.LivenessProbe, "/health/live", 18070, ackSidecarsProbeTiming{initialDelaySeconds: 10, periodSeconds: 20, timeoutSeconds: 3, failureThreshold: 3})
	assertACKSidecarsResources(t, "container resource requests", container.Resources.Requests, corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("256Mi")})
	assertACKSidecarsResources(t, "container resource limits", container.Resources.Limits, corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("512Mi")})
	if !reflect.DeepEqual(container.VolumeMounts, []corev1.VolumeMount{{Name: "sidecar-ilink-state", MountPath: "/app/state", ReadOnly: false}}) || len(pod.Volumes) != 1 || pod.Volumes[0].Name != "sidecar-ilink-state" || pod.Volumes[0].PersistentVolumeClaim == nil || pod.Volumes[0].PersistentVolumeClaim.ClaimName != "sidecar-ilink-state" || pod.Volumes[0].PersistentVolumeClaim.ReadOnly {
		t.Fatalf("iLink volumes/mounts = %v/%v, want PVC sidecar-ilink-state at /app/state", pod.Volumes, container.VolumeMounts)
	}
}

func assertSeednoteACKPodSpec(t *testing.T, pod corev1.PodSpec) {
	t.Helper()
	assertACKSidecarsPodBase(t, pod, []corev1.LocalObjectReference{{Name: ackSidecarsTestImagePullSecret}})
	container := pod.Containers[0]
	if container.Name != "sidecar-seednote" || container.Image != ackSidecarsTestSeednoteImage || container.ImagePullPolicy != corev1.PullAlways {
		t.Fatalf("container name/image/pull policy = %s/%s/%s", container.Name, container.Image, container.ImagePullPolicy)
	}
	assertACKSidecarsPort(t, container, 18060)
	assertACKSidecarsEnv(t, container.Env, map[string]string{
		"COOKIES_PATH": "/app/data/cookies.json", "HOME": "/app/data/home", "XDG_CACHE_HOME": "/app/cache", "XDG_CONFIG_HOME": "/app/data/config",
	})
	assertACKSidecarsProbe(t, "readiness", container.ReadinessProbe, "/health", 18060, ackSidecarsProbeTiming{initialDelaySeconds: 10, periodSeconds: 10, timeoutSeconds: 5, failureThreshold: 6})
	assertACKSidecarsProbe(t, "liveness", container.LivenessProbe, "/health", 18060, ackSidecarsProbeTiming{initialDelaySeconds: 30, periodSeconds: 20, timeoutSeconds: 5, failureThreshold: 3})
	assertACKSidecarsResources(t, "container resource requests", container.Resources.Requests, corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("1Gi")})
	assertACKSidecarsResources(t, "container resource limits", container.Resources.Limits, corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("4Gi")})
	if !reflect.DeepEqual(container.VolumeMounts, []corev1.VolumeMount{{Name: "sidecar-seednote-data", MountPath: "/app/data", ReadOnly: false}, {Name: "browser-shm", MountPath: "/dev/shm", ReadOnly: false}}) || len(pod.Volumes) != 2 {
		t.Fatalf("Seednote volumes/mounts = %v/%v, want data PVC and browser shared-memory volume", pod.Volumes, container.VolumeMounts)
	}
	if pod.Volumes[0].Name != "sidecar-seednote-data" || pod.Volumes[0].PersistentVolumeClaim == nil || pod.Volumes[0].PersistentVolumeClaim.ClaimName != "sidecar-seednote-data" || pod.Volumes[0].PersistentVolumeClaim.ReadOnly {
		t.Fatalf("Seednote data volume = %+v, want PVC sidecar-seednote-data", pod.Volumes[0])
	}
	shm := pod.Volumes[1]
	if shm.Name != "browser-shm" || shm.EmptyDir == nil || shm.EmptyDir.Medium != corev1.StorageMediumMemory || shm.EmptyDir.SizeLimit == nil || shm.EmptyDir.SizeLimit.Cmp(resource.MustParse("1Gi")) != 0 {
		t.Fatalf("Seednote shared-memory volume = %+v, want Memory emptyDir with 1Gi size limit", shm)
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
		"value: http://sidecar-seednote:18060",
		"name: ANBAN_ILINK_ENABLED",
		"value: \"true\"",
		"name: ANBAN_ILINK_BASE_URL",
		"value: http://sidecar-ilink:18070",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("server deployment missing %q", want)
		}
	}
}

func TestConfigExampleDocumentsSidecarEnvs(t *testing.T) {
	raw, err := os.ReadFile("config.example.yaml")
	if err != nil {
		t.Fatalf("read config example: %v", err)
	}
	text := string(raw)

	for _, want := range []string{
		"seednote:",
		`base_url: "${ANBAN_SEEDNOTE_BASE_URL:-http://sidecar-seednote:18060}"`,
		"timeout: 30",
		"ilink:",
		`base_url: "${ANBAN_ILINK_BASE_URL:-http://sidecar-ilink:18070}"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("config example missing %q", want)
		}
	}
}

func assertACKSidecarsPort(t *testing.T, container corev1.Container, wantPort int32) {
	t.Helper()
	if len(container.Ports) != 1 || container.Ports[0].ContainerPort != wantPort || container.Ports[0].Protocol != corev1.ProtocolTCP || container.Ports[0].HostPort != 0 {
		t.Fatalf("container ports = %+v, want one TCP port %d without hostPort", container.Ports, wantPort)
	}
}

func assertACKSidecarsEnv(t *testing.T, env []corev1.EnvVar, want map[string]string) {
	t.Helper()
	if len(env) != len(want) {
		t.Fatalf("environment variable count = %d, want %d", len(env), len(want))
	}
	got := make(map[string]string, len(env))
	for _, variable := range env {
		if _, duplicate := got[variable.Name]; variable.ValueFrom != nil || duplicate {
			t.Fatalf("environment variable %q must occur once with a literal value", variable.Name)
		}
		got[variable.Name] = variable.Value
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("environment variables = %v, want %v", got, want)
	}
}

type ackSidecarsProbeTiming struct {
	initialDelaySeconds int32
	periodSeconds       int32
	timeoutSeconds      int32
	failureThreshold    int32
}

func assertACKSidecarsProbe(t *testing.T, name string, probe *corev1.Probe, wantPath string, wantPort int, wantTiming ackSidecarsProbeTiming) {
	t.Helper()
	if probe == nil || probe.HTTPGet == nil || probe.Exec != nil || probe.TCPSocket != nil || probe.GRPC != nil {
		t.Fatalf("%s probe must use only an HTTP GET handler, got %+v", name, probe)
	}
	httpGet := probe.HTTPGet
	if httpGet.Path != wantPath || httpGet.Port != intstr.FromInt(wantPort) || (httpGet.Scheme != "" && httpGet.Scheme != corev1.URISchemeHTTP) || httpGet.Host != "" || len(httpGet.HTTPHeaders) != 0 {
		t.Fatalf("%s HTTP handler = %+v, want default-scheme HTTP %s on port %d without host or headers", name, httpGet, wantPath, wantPort)
	}
	if probe.InitialDelaySeconds != wantTiming.initialDelaySeconds || probe.PeriodSeconds != wantTiming.periodSeconds || probe.TimeoutSeconds != wantTiming.timeoutSeconds || probe.FailureThreshold != wantTiming.failureThreshold || probe.SuccessThreshold != 0 {
		t.Fatalf("%s probe timing = initial=%d period=%d timeout=%d failure=%d success=%d, want initial=%d period=%d timeout=%d failure=%d success=0", name, probe.InitialDelaySeconds, probe.PeriodSeconds, probe.TimeoutSeconds, probe.FailureThreshold, probe.SuccessThreshold, wantTiming.initialDelaySeconds, wantTiming.periodSeconds, wantTiming.timeoutSeconds, wantTiming.failureThreshold)
	}
}

func assertACKSidecarsResources(t *testing.T, name string, got, want corev1.ResourceList) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s count = %d, want %d", name, len(got), len(want))
	}
	for resourceName, wantQuantity := range want {
		gotQuantity, ok := got[resourceName]
		if !ok || gotQuantity.Cmp(wantQuantity) != 0 {
			t.Fatalf("%s %s = %s, want %s", name, resourceName, gotQuantity.String(), wantQuantity.String())
		}
	}
}

func assertACKSidecarsService(t *testing.T, service corev1.Service, app string, port int32) {
	t.Helper()
	if service.Spec.Type != corev1.ServiceTypeClusterIP || !reflect.DeepEqual(service.Spec.Selector, map[string]string{"app": app}) {
		t.Fatalf("service type/selector = %s/%v, want ClusterIP/app=%s", service.Spec.Type, service.Spec.Selector, app)
	}
	if len(service.Spec.Ports) != 1 || service.Spec.Ports[0].Port != port || service.Spec.Ports[0].Protocol != corev1.ProtocolTCP || service.Spec.Ports[0].TargetPort != intstr.FromInt(int(port)) || service.Spec.Ports[0].NodePort != 0 {
		t.Fatalf("service ports = %+v, want one TCP ClusterIP port %d without nodePort", service.Spec.Ports, port)
	}
	if len(service.Spec.ExternalIPs) != 0 || service.Spec.ExternalName != "" || service.Spec.LoadBalancerIP != "" || service.Spec.LoadBalancerClass != nil {
		t.Fatal("service must not define externalIPs, externalName, loadBalancerIP, or loadBalancerClass")
	}
}
