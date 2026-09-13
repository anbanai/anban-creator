package main

import (
	"context"
	"testing"
	"time"

	serveragent "github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/config"
)

type blockingRuntimeReconciler struct {
	started chan struct{}
	release chan struct{}
}

func (r *blockingRuntimeReconciler) Run(ctx context.Context) {
	close(r.started)
	<-ctx.Done()
	<-r.release
}

func TestRuntimeReconcilerLifecycleJoinsBeforeProviderClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reconciler := &blockingRuntimeReconciler{started: make(chan struct{}), release: make(chan struct{})}
	done := startRuntimeReconciler(ctx, reconciler)
	<-reconciler.started

	cancel()
	providerClosed := make(chan struct{})
	go func() {
		<-done
		close(providerClosed)
	}()
	select {
	case <-providerClosed:
		t.Fatal("provider close passed reconciler join before Run returned")
	case <-time.After(20 * time.Millisecond):
	}
	close(reconciler.release)
	select {
	case <-providerClosed:
	case <-time.After(time.Second):
		t.Fatal("reconciler join did not unblock provider close")
	}
}

func TestManagedRuntimeReconcilerConfigCoversDockerAndKubernetes(t *testing.T) {
	docker := managedRuntimeReconcilerConfig("docker", config.DockerConfig{TimeoutSec: 600}, config.KubernetesConfig{}, 42)
	if docker.ActiveDeadline != 10*time.Minute || docker.HeartbeatTimeout <= 0 || docker.HeartbeatTimeout >= docker.ActiveDeadline || docker.CompletionGrace != 30*time.Second || docker.DiagnosticRetention != 42*time.Second {
		t.Fatalf("Docker reconciler config = %+v", docker)
	}

	kubernetes := managedRuntimeReconcilerConfig("kubernetes", config.DockerConfig{}, config.KubernetesConfig{
		ActiveDeadlineSeconds: 900, HeartbeatTimeoutSeconds: 120, CompletionGraceSeconds: 45,
	}, 42)
	want := serveragent.RuntimeReconcilerConfig{
		ActiveDeadline: 15 * time.Minute, HeartbeatTimeout: 2 * time.Minute, CompletionGrace: 45 * time.Second, DiagnosticRetention: 42 * time.Second,
	}
	if kubernetes.ActiveDeadline != want.ActiveDeadline || kubernetes.HeartbeatTimeout != want.HeartbeatTimeout || kubernetes.CompletionGrace != want.CompletionGrace {
		t.Fatalf("Kubernetes reconciler config = %+v, want %+v", kubernetes, want)
	}
}
