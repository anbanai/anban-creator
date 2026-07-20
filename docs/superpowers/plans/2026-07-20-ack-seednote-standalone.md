# ACK Seednote Standalone Deployment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Yunxiao-ready, internal-only ACK deployment for `xpzouying/xiaohongshu-mcp:latest` without building or mirroring the image.

**Architecture:** A parameterized Kubernetes manifest creates one PVC, one single-replica Deployment, and one ClusterIP Service named `seednote`. A companion runbook supplies the exact Yunxiao shell task that renders variables, applies the manifest, forces the mutable `latest` tag to refresh, and verifies rollout state.

**Tech Stack:** Kubernetes YAML, Alibaba Cloud ACK, Yunxiao Flow shell task, `envsubst`, `kubectl`, Go manifest contract tests.

---

### Task 1: Add The Standalone Manifest Contract Test

**Files:**
- Create: `server/k8s_seednote_standalone_test.go`
- Test: `server/k8s_seednote_standalone_test.go`

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"os"
	"strings"
	"testing"
)

func TestACKStandaloneSeednoteManifest(t *testing.T) {
	raw, err := os.ReadFile("../deploy/k8s/ack-seednote.yaml")
	if err != nil {
		t.Fatalf("read standalone ACK seednote manifest: %v", err)
	}
	text := string(raw)

	for _, want := range []string{
		"kind: PersistentVolumeClaim",
		"name: seednote-data",
		"namespace: ${namespace}",
		"storage: ${seednote_storage_size}",
		"kind: Deployment",
		"replicas: 1",
		"type: Recreate",
		"kubernetes.io/arch: amd64",
		"automountServiceAccountToken: false",
		"image: xpzouying/xiaohongshu-mcp:latest",
		"imagePullPolicy: Always",
		"value: /usr/local/bin/cloak-chromium",
		"value: /app/data/cookies.json",
		"mountPath: /app/data",
		"claimName: seednote-data",
		"mountPath: /dev/shm",
		"medium: Memory",
		"path: /health",
		"kind: Service",
		"type: ClusterIP",
		"port: 18060",
		"targetPort: 18060",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("standalone ACK seednote manifest missing %q", want)
		}
	}

	for _, forbidden := range []string{
		"imagePullSecrets:",
		"kind: Ingress",
		"type: LoadBalancer",
		"type: NodePort",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("standalone ACK seednote manifest contains forbidden public or registry configuration %q", forbidden)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails because the manifest is missing**

Run:

```bash
go test ./server -run TestACKStandaloneSeednoteManifest -count=1
```

Expected: `FAIL` containing `read standalone ACK seednote manifest`.

- [ ] **Step 3: Commit the failing contract test with the implementation in Task 2 after it turns green**

Do not commit a deliberately failing repository state.

### Task 2: Add The Standalone ACK Manifest

**Files:**
- Create: `deploy/k8s/ack-seednote.yaml`
- Test: `server/k8s_seednote_standalone_test.go`

- [ ] **Step 1: Create the manifest**

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: seednote-data
  namespace: ${namespace}
  labels:
    app: seednote
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: ${seednote_storage_size}
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: seednote
  namespace: ${namespace}
  labels:
    app: seednote
spec:
  replicas: 1
  strategy:
    type: Recreate
  selector:
    matchLabels:
      app: seednote
  template:
    metadata:
      labels:
        app: seednote
    spec:
      automountServiceAccountToken: false
      nodeSelector:
        kubernetes.io/arch: amd64
      containers:
        - name: seednote
          image: xpzouying/xiaohongshu-mcp:latest
          imagePullPolicy: Always
          ports:
            - name: http
              containerPort: 18060
              protocol: TCP
          env:
            - name: ROD_BROWSER_BIN
              value: /usr/local/bin/cloak-chromium
            - name: COOKIES_PATH
              value: /app/data/cookies.json
            - name: HOME
              value: /app/data/home
            - name: XDG_CACHE_HOME
              value: /app/data/cache
            - name: XDG_CONFIG_HOME
              value: /app/data/config
          readinessProbe:
            httpGet:
              path: /health
              port: 18060
            initialDelaySeconds: 10
            periodSeconds: 10
            timeoutSeconds: 5
            failureThreshold: 6
          livenessProbe:
            httpGet:
              path: /health
              port: 18060
            initialDelaySeconds: 30
            periodSeconds: 20
            timeoutSeconds: 5
            failureThreshold: 3
          resources:
            requests:
              cpu: 500m
              memory: 1Gi
            limits:
              cpu: "2"
              memory: 4Gi
          volumeMounts:
            - name: seednote-data
              mountPath: /app/data
            - name: browser-shm
              mountPath: /dev/shm
      volumes:
        - name: seednote-data
          persistentVolumeClaim:
            claimName: seednote-data
        - name: browser-shm
          emptyDir:
            medium: Memory
            sizeLimit: 1Gi
---
apiVersion: v1
kind: Service
metadata:
  name: seednote
  namespace: ${namespace}
  labels:
    app: seednote
spec:
  type: ClusterIP
  selector:
    app: seednote
  ports:
    - name: http
      port: 18060
      protocol: TCP
      targetPort: 18060
```

- [ ] **Step 2: Run the targeted test and verify it passes**

Run:

```bash
go test ./server -run TestACKStandaloneSeednoteManifest -count=1
```

Expected: `ok .../server`.

- [ ] **Step 3: Render and parse the YAML locally**

Run:

```bash
namespace=anbanai-prod seednote_storage_size=10Gi \
  envsubst '${namespace} ${seednote_storage_size}' \
  < deploy/k8s/ack-seednote.yaml \
  | kubectl apply --dry-run=client -f -
```

Expected: PVC, Deployment, and Service report `created (dry run)`.

- [ ] **Step 4: Commit the manifest and contract test**

```bash
git add deploy/k8s/ack-seednote.yaml server/k8s_seednote_standalone_test.go
git commit -m "feat: add standalone ACK seednote deployment"
```

### Task 3: Add The Yunxiao Runbook

**Files:**
- Create: `deploy/k8s/ack-seednote.md`

- [ ] **Step 1: Document the pipeline configuration and exact deployment script**

The runbook must name the two pipeline variables `namespace` and
`seednote_storage_size`, explain that the ACK service connection supplies the
`kubectl` context, and provide this script:

```bash
set -euo pipefail

export namespace="${namespace:?set the ACK namespace in Yunxiao}"
export seednote_storage_size="${seednote_storage_size:-10Gi}"

kubectl create namespace "$namespace" \
  --dry-run=client \
  -o yaml \
  | kubectl apply -f -

envsubst '${namespace} ${seednote_storage_size}' \
  < deploy/k8s/ack-seednote.yaml \
  | kubectl apply -f -

kubectl -n "$namespace" rollout restart deployment/seednote
kubectl -n "$namespace" rollout status deployment/seednote --timeout=10m
kubectl -n "$namespace" get deployment,pod,service,pvc -l app=seednote
kubectl -n "$namespace" get endpoints seednote
```

Also document:

```bash
kubectl -n "$namespace" port-forward service/seednote 18060:18060
curl --fail http://127.0.0.1:18060/health
npx @modelcontextprotocol/inspector
```

The MCP URL is `http://127.0.0.1:18060/mcp` through port forwarding and
`http://seednote:18060/mcp` from Anban Server in the same namespace.

- [ ] **Step 2: Check the documentation and manifest for unsafe exposure guidance**

Run:

```bash
rg -n 'Ingress|LoadBalancer|NodePort|ClusterIP|rollout restart|envsubst|port-forward' \
  deploy/k8s/ack-seednote.yaml deploy/k8s/ack-seednote.md
```

Expected: the manifest contains only `ClusterIP`; public resource names appear
only in warnings that say not to create them.

- [ ] **Step 3: Commit the runbook**

```bash
git add deploy/k8s/ack-seednote.md
git commit -m "docs: document Yunxiao seednote deployment"
```

### Task 4: Run Final Verification

**Files:**
- Verify: `deploy/k8s/ack-seednote.yaml`
- Verify: `deploy/k8s/ack-seednote.md`
- Verify: `server/k8s_seednote_standalone_test.go`

- [ ] **Step 1: Run all ACK manifest contract tests**

```bash
go test ./server -run 'TestACK|TestK8sSidecar' -count=1
```

Expected: `ok .../server`.

- [ ] **Step 2: Run the full Go suite**

```bash
go test ./...
```

Expected: all packages pass with exit code zero.

- [ ] **Step 3: Check formatting and repository diff**

```bash
gofmt -w server/k8s_seednote_standalone_test.go
git diff --check
git status --short
```

Expected: no formatting or whitespace errors; status shows only the intended
implementation files if commits were intentionally deferred.
