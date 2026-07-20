package main

import (
	"os"
	"reflect"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/yaml"
)

const (
	seednoteTestNamespace = "anbanai-test"
	seednoteTestStorage   = "10Gi"
)

func TestACKStandaloneSeednoteManifest(t *testing.T) {
	raw, err := os.ReadFile("../deploy/k8s/ack-seednote.yaml")
	if err != nil {
		t.Fatalf("read standalone ACK Seednote manifest: %v", err)
	}
	documents := splitSeednoteManifest(t, renderSeednoteManifest(string(raw)))
	if len(documents) != 3 {
		t.Fatalf("standalone ACK Seednote manifest must contain exactly 3 documents, got %d", len(documents))
	}

	var pvc corev1.PersistentVolumeClaim
	strictUnmarshalSeednoteDocument(t, documents[0], &pvc)
	assertSeednoteObject(t, pvc.TypeMeta.APIVersion, pvc.TypeMeta.Kind, pvc.Name, pvc.Namespace, pvc.Labels, "v1", "PersistentVolumeClaim", "seednote-data")
	if !reflect.DeepEqual(pvc.Spec.AccessModes, []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}) {
		t.Fatalf("PVC access modes = %v, want [ReadWriteOnce]", pvc.Spec.AccessModes)
	}
	assertResourceList(t, "PVC storage requests", pvc.Spec.Resources.Requests, corev1.ResourceList{
		corev1.ResourceStorage: resource.MustParse(seednoteTestStorage),
	})

	var deployment appsv1.Deployment
	strictUnmarshalSeednoteDocument(t, documents[1], &deployment)
	assertSeednoteObject(t, deployment.TypeMeta.APIVersion, deployment.TypeMeta.Kind, deployment.Name, deployment.Namespace, deployment.Labels, "apps/v1", "Deployment", "seednote")
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 || deployment.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType {
		t.Fatalf("deployment replicas/strategy = %v/%s, want 1/Recreate", deployment.Spec.Replicas, deployment.Spec.Strategy.Type)
	}
	if deployment.Spec.Selector == nil || !reflect.DeepEqual(deployment.Spec.Selector.MatchLabels, map[string]string{"app": "seednote"}) {
		t.Fatalf("deployment selector = %v, want app=seednote", deployment.Spec.Selector)
	}
	if !reflect.DeepEqual(deployment.Spec.Template.Labels, map[string]string{"app": "seednote"}) {
		t.Fatalf("pod template labels = %v, want app=seednote", deployment.Spec.Template.Labels)
	}
	assertSeednotePodSpec(t, deployment.Spec.Template.Spec)

	var service corev1.Service
	strictUnmarshalSeednoteDocument(t, documents[2], &service)
	assertSeednoteObject(t, service.TypeMeta.APIVersion, service.TypeMeta.Kind, service.Name, service.Namespace, service.Labels, "v1", "Service", "seednote")
	if service.Spec.Type != corev1.ServiceTypeClusterIP || !reflect.DeepEqual(service.Spec.Selector, map[string]string{"app": "seednote"}) {
		t.Fatalf("service type/selector = %s/%v, want ClusterIP/app=seednote", service.Spec.Type, service.Spec.Selector)
	}
	if len(service.Spec.Ports) != 1 {
		t.Fatalf("service port count = %d, want 1", len(service.Spec.Ports))
	}
	servicePort := service.Spec.Ports[0]
	if servicePort.Port != 18060 || servicePort.Protocol != corev1.ProtocolTCP || servicePort.TargetPort != intstr.FromInt(18060) {
		t.Fatalf("service port = %d/%s/%s, want 18060/TCP/18060", servicePort.Port, servicePort.Protocol, servicePort.TargetPort.String())
	}
	if servicePort.NodePort != 0 {
		t.Fatalf("service port nodePort = %d, must be omitted (zero)", servicePort.NodePort)
	}
	if len(service.Spec.ExternalIPs) != 0 || service.Spec.ExternalName != "" || service.Spec.LoadBalancerIP != "" || service.Spec.LoadBalancerClass != nil {
		t.Fatal("service must not define externalIPs, externalName, loadBalancerIP, or loadBalancerClass")
	}
}

func renderSeednoteManifest(raw string) string {
	return strings.NewReplacer(
		"${namespace}", seednoteTestNamespace,
		"${seednote_storage_size}", seednoteTestStorage,
	).Replace(raw)
}

func splitSeednoteManifest(t *testing.T, raw string) []string {
	t.Helper()
	documents := strings.Split(strings.TrimSpace(raw), "\n---\n")
	for index, document := range documents {
		if strings.TrimSpace(document) == "" {
			t.Fatalf("standalone ACK Seednote manifest contains empty document %d", index+1)
		}
	}
	return documents
}

func strictUnmarshalSeednoteDocument(t *testing.T, document string, target any) {
	t.Helper()
	if err := yaml.UnmarshalStrict([]byte(document), target); err != nil {
		t.Fatalf("strictly decode standalone ACK Seednote document: %v", err)
	}
}

func assertSeednoteObject(t *testing.T, apiVersion, kind, name, namespace string, labels map[string]string, wantAPIVersion, wantKind, wantName string) {
	t.Helper()
	if apiVersion != wantAPIVersion || kind != wantKind || name != wantName || namespace != seednoteTestNamespace {
		t.Fatalf("object = %s %s %s/%s, want %s %s %s/%s", apiVersion, kind, namespace, name, wantAPIVersion, wantKind, seednoteTestNamespace, wantName)
	}
	if !reflect.DeepEqual(labels, map[string]string{"app": "seednote"}) {
		t.Fatalf("object labels = %v, want app=seednote", labels)
	}
}

func assertSeednotePodSpec(t *testing.T, pod corev1.PodSpec) {
	t.Helper()
	if pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken {
		t.Fatal("pod must set automountServiceAccountToken to false")
	}
	if pod.HostNetwork {
		t.Fatal("pod hostNetwork must be false")
	}
	if len(pod.ImagePullSecrets) != 0 {
		t.Fatalf("pod imagePullSecrets = %v, must be empty", pod.ImagePullSecrets)
	}
	if !reflect.DeepEqual(pod.NodeSelector, map[string]string{"kubernetes.io/arch": "amd64"}) {
		t.Fatalf("pod node selector = %v, want kubernetes.io/arch=amd64", pod.NodeSelector)
	}
	if len(pod.Containers) != 1 {
		t.Fatalf("pod containers = %d, want 1", len(pod.Containers))
	}

	container := pod.Containers[0]
	if container.Name != "seednote" || container.Image != "xpzouying/xiaohongshu-mcp:latest" || container.ImagePullPolicy != corev1.PullAlways {
		t.Fatalf("container name/image/pull policy = %s/%s/%s", container.Name, container.Image, container.ImagePullPolicy)
	}
	if len(container.Ports) != 1 {
		t.Fatalf("container port count = %d, want 1", len(container.Ports))
	}
	containerPort := container.Ports[0]
	if containerPort.ContainerPort != 18060 || containerPort.Protocol != corev1.ProtocolTCP {
		t.Fatalf("container port = %d/%s, want 18060/TCP", containerPort.ContainerPort, containerPort.Protocol)
	}
	if containerPort.HostPort != 0 {
		t.Fatalf("container hostPort = %d, must be omitted (zero)", containerPort.HostPort)
	}
	assertSeednoteEnv(t, container.Env)
	assertSeednoteProbe(t, "readiness", container.ReadinessProbe)
	assertSeednoteProbe(t, "liveness", container.LivenessProbe)
	assertResourceList(t, "container resource requests", container.Resources.Requests, corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("500m"),
		corev1.ResourceMemory: resource.MustParse("1Gi"),
	})
	assertResourceList(t, "container resource limits", container.Resources.Limits, corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("2"),
		corev1.ResourceMemory: resource.MustParse("4Gi"),
	})
	if !reflect.DeepEqual(container.VolumeMounts, []corev1.VolumeMount{
		{Name: "seednote-data", MountPath: "/app/data"},
		{Name: "browser-shm", MountPath: "/dev/shm"},
	}) {
		t.Fatalf("container volume mounts = %v", container.VolumeMounts)
	}
	if len(pod.Volumes) != 2 {
		t.Fatalf("pod volume count = %d, want 2", len(pod.Volumes))
	}
	if pod.Volumes[0].Name != "seednote-data" || pod.Volumes[0].PersistentVolumeClaim == nil || pod.Volumes[0].PersistentVolumeClaim.ClaimName != "seednote-data" {
		t.Fatalf("data volume = %+v, want PVC seednote-data", pod.Volumes[0])
	}
	if pod.Volumes[1].Name != "browser-shm" || pod.Volumes[1].EmptyDir == nil || pod.Volumes[1].EmptyDir.Medium != corev1.StorageMediumMemory || pod.Volumes[1].EmptyDir.SizeLimit == nil || pod.Volumes[1].EmptyDir.SizeLimit.Cmp(resource.MustParse("1Gi")) != 0 {
		t.Fatalf("shared-memory volume = %+v, want Memory emptyDir with 1Gi size limit", pod.Volumes[1])
	}
}

func assertSeednoteEnv(t *testing.T, env []corev1.EnvVar) {
	t.Helper()
	want := map[string]string{
		"ROD_BROWSER_BIN": "/usr/local/bin/cloak-chromium",
		"COOKIES_PATH":    "/app/data/cookies.json",
		"HOME":            "/app/data/home",
		"XDG_CACHE_HOME":  "/app/data/cache",
		"XDG_CONFIG_HOME": "/app/data/config",
	}
	if len(env) != len(want) {
		t.Fatalf("environment variable count = %d, want %d", len(env), len(want))
	}
	got := make(map[string]string, len(env))
	for _, variable := range env {
		if variable.ValueFrom != nil {
			t.Fatalf("environment variable %q must use a literal value", variable.Name)
		}
		if _, exists := got[variable.Name]; exists {
			t.Fatalf("duplicate environment variable %q", variable.Name)
		}
		got[variable.Name] = variable.Value
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("environment variables = %v, want %v", got, want)
	}
}

func assertSeednoteProbe(t *testing.T, name string, probe *corev1.Probe) {
	t.Helper()
	if probe == nil || probe.HTTPGet == nil || probe.HTTPGet.Path != "/health" || probe.HTTPGet.Port != intstr.FromInt(18060) {
		t.Fatalf("%s probe = %+v, want HTTP /health on port 18060", name, probe)
	}
}

func assertResourceList(t *testing.T, name string, got, want corev1.ResourceList) {
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
