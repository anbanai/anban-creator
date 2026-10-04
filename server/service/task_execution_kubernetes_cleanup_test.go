package service

import (
	"context"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/agent"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"k8s.io/client-go/kubernetes/fake"
)

type cleanupOnlyProjectMemory struct{ t *testing.T }

func (m cleanupOnlyProjectMemory) EnsureProject(context.Context, string) error {
	m.t.Fatal("cleanup must not prepare project memory")
	return nil
}

func (m cleanupOnlyProjectMemory) RequireProject(context.Context, string) error {
	m.t.Fatal("cleanup must not prepare project memory")
	return nil
}

func TestKubernetesCleanupBlocksMissingJobWithoutFrozenNamespace(t *testing.T) {
	ctx := context.Background()
	svc, repo, db, _, execution := setupCloudCompletionTestWithDB(t, false, false)
	if err := db.Model(&model.TaskExecution{}).Where("id = ?", execution.ID).Update("target", "kubernetes").Error; err != nil {
		t.Fatal(err)
	}
	if won, err := repo.TaskExecutions().Transition(ctx, execution.ID, []string{model.TaskExecutionStarting}, model.TaskExecutionCancelled, model.ExecutionTransition{}); err != nil || !won {
		t.Fatalf("terminalize: won=%v err=%v", won, err)
	}
	// Only the current namespace is available. A prebind execution may have
	// created its Job in another namespace before a configuration change.
	client := fake.NewSimpleClientset()
	dispatcher, err := agent.NewKubernetesDispatcherWithClient(srvconfig.KubernetesConfig{Namespace: "new-namespace"}, nil, "", client, cleanupOnlyProjectMemory{t})
	if err != nil {
		t.Fatal(err)
	}
	svc.SetRuntimeDispatcher(dispatcher)
	if err := svc.cleanupCancelledExecution(ctx, execution); !agent.IsPermanentDispatchError(err) {
		t.Fatalf("cleanup = %v, want permanent unresolved identity", err)
	}
	found, err := repo.TaskExecutions().FindByID(ctx, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.CleanupStatus != model.TaskExecutionCleanupBlocked || found.CleanupDiagnostic != "runtime_identity_conflict" || found.Status != model.TaskExecutionCancelled || found.RuntimeScope != "" {
		t.Fatalf("cleanup did not preserve blocked identity/outcome: %+v", found)
	}
	if won, err := repo.TaskExecutions().ClaimCleanup(ctx, execution.ID, "retry", time.Minute); err != nil || won {
		t.Fatalf("blocked cleanup was reclaimable: won=%v err=%v", won, err)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "get" || action.GetNamespace() != "new-namespace" {
			t.Fatalf("unexpected cleanup provider action: %v", action)
		}
	}
}
