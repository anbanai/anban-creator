package main

import (
	"context"
	"time"

	serveragent "github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/config"
)

type runtimeReconcilerRunner interface {
	Run(context.Context)
}

func startRuntimeReconciler(ctx context.Context, runner runtimeReconcilerRunner) <-chan struct{} {
	done := make(chan struct{})
	if runner == nil {
		close(done)
		return done
	}
	go func() {
		defer close(done)
		runner.Run(ctx)
	}()
	return done
}

func managedRuntimeReconcilerConfig(executor string, docker config.DockerConfig, kubernetes config.KubernetesConfig) serveragent.RuntimeReconcilerConfig {
	switch executor {
	case "docker":
		activeDeadline := time.Duration(docker.TimeoutSec) * time.Second
		heartbeatTimeout := 180 * time.Second
		if activeDeadline > 0 && heartbeatTimeout >= activeDeadline {
			heartbeatTimeout = activeDeadline / 2
		}
		return serveragent.RuntimeReconcilerConfig{
			ActiveDeadline:     activeDeadline,
			HeartbeatTimeout:   heartbeatTimeout,
			CompletionGrace:    30 * time.Second,
			PreStartRetryLimit: 1,
		}
	case "kubernetes":
		return serveragent.RuntimeReconcilerConfig{
			ActiveDeadline:     time.Duration(kubernetes.ActiveDeadlineSeconds) * time.Second,
			HeartbeatTimeout:   time.Duration(kubernetes.HeartbeatTimeoutSeconds) * time.Second,
			CompletionGrace:    time.Duration(kubernetes.CompletionGraceSeconds) * time.Second,
			PreStartRetryLimit: kubernetes.PreStartRetryLimit,
		}
	default:
		return serveragent.RuntimeReconcilerConfig{}
	}
}
