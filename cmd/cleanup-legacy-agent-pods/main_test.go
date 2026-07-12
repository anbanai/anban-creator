package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	serveragent "github.com/anbanai/anban-creator/server/agent"
)

func TestRunCleanupRejectsIncompleteRollout(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*appsv1.Deployment)
	}{
		{name: "new replica not ready", mutate: func(deployment *appsv1.Deployment) {
			deployment.Status.ReadyReplicas = 0
		}},
		{name: "old replica still exists", mutate: func(deployment *appsv1.Deployment) {
			deployment.Status.Replicas = 2
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			deployment := completeDeployment()
			tt.mutate(deployment)
			client := fake.NewSimpleClientset(deployment)

			err := runCleanup(context.Background(), client, testOptions(false), strings.NewReader(""), &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), "rollout is not complete") {
				t.Fatalf("runCleanup() error = %v, want incomplete rollout", err)
			}
			for _, action := range client.Actions() {
				if action.GetVerb() == "list" || action.GetVerb() == "delete" {
					t.Fatalf("unexpected action after incomplete rollout: %#v", action)
				}
			}
		})
	}
}

func TestRunCleanupDryRunUsesExactSelectorAndExcludesMalformedPods(t *testing.T) {
	valid := legacyPod("user-1", "project-1", "legacy-uid")
	malformed := legacyPod("user-2", "project-2", "malformed-uid")
	delete(malformed.Annotations, "anban.ai/pod-config-hash")
	client := fake.NewSimpleClientset(completeDeployment(), valid, malformed)
	var out bytes.Buffer

	if err := runCleanup(context.Background(), client, testOptions(false), strings.NewReader(""), &out); err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
	if !strings.Contains(out.String(), valid.Name+"\t"+string(valid.UID)) {
		t.Fatalf("output missing valid candidate: %q", out.String())
	}
	if strings.Contains(out.String(), malformed.Name) {
		t.Fatalf("output includes malformed candidate: %q", out.String())
	}
	if !strings.Contains(out.String(), "--execute --confirm-drained") {
		t.Fatalf("output missing next-step instruction: %q", out.String())
	}

	listCount := 0
	for _, action := range client.Actions() {
		switch action.GetVerb() {
		case "list":
			listCount++
			restrictions := action.(k8stesting.ListAction).GetListRestrictions()
			if got, want := restrictions.Labels.String(), serveragent.LegacyAgentPodLabelSelector(); got != want {
				t.Fatalf("list selector = %q, want %q", got, want)
			}
		case "delete":
			t.Fatalf("dry-run issued delete action: %#v", action)
		}
	}
	if listCount != 1 {
		t.Fatalf("list count = %d, want 1", listCount)
	}
}

func TestRunCleanupExecuteRequiresDrainedConfirmation(t *testing.T) {
	opts := testOptions(true)
	opts.confirmDrained = false
	client := fake.NewSimpleClientset()

	err := runCleanup(context.Background(), client, opts, strings.NewReader(""), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--confirm-drained") {
		t.Fatalf("runCleanup() error = %v, want confirm-drained gate", err)
	}
	if len(client.Actions()) != 0 {
		t.Fatalf("client actions = %#v, want none", client.Actions())
	}
}

func TestRunCleanupInteractiveConfirmation(t *testing.T) {
	t.Run("rejects wrong token", func(t *testing.T) {
		pod := legacyPod("user-1", "project-1", "legacy-uid")
		client := fake.NewSimpleClientset(completeDeployment(), pod)
		err := runCleanup(context.Background(), client, testOptions(true), strings.NewReader("no\n"), &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "confirmation") {
			t.Fatalf("runCleanup() error = %v, want confirmation error", err)
		}
		assertNoDeleteActions(t, client)
	})

	t.Run("accepts exact token", func(t *testing.T) {
		pod := legacyPod("user-1", "project-1", "legacy-uid")
		client := fake.NewSimpleClientset(completeDeployment(), pod)
		if err := runCleanup(context.Background(), client, testOptions(true), strings.NewReader(executeConfirmationToken+"\n"), &bytes.Buffer{}); err != nil {
			t.Fatalf("runCleanup() error = %v", err)
		}
		assertDeleted(t, client, pod.Name, pod.UID)
	})
}

func TestRunCleanupTreatsCandidateNotFoundAsSuccess(t *testing.T) {
	pod := legacyPod("user-1", "project-1", "legacy-uid")
	client := fake.NewSimpleClientset(completeDeployment(), pod)
	client.PrependReactor("get", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, pod.Name)
	})

	opts := testOptions(true)
	opts.yes = true
	if err := runCleanup(context.Background(), client, opts, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
	assertNoDeleteActions(t, client)
}

func TestRunCleanupPreservesUIDReplacement(t *testing.T) {
	pod := legacyPod("user-1", "project-1", "legacy-old-uid")
	replacement := pod.DeepCopy()
	replacement.UID = types.UID("legacy-new-uid")
	client := fake.NewSimpleClientset(completeDeployment(), pod)
	client.PrependReactor("get", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, replacement, nil
	})

	opts := testOptions(true)
	opts.yes = true
	err := runCleanup(context.Background(), client, opts, strings.NewReader(""), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "UID changed") {
		t.Fatalf("runCleanup() error = %v, want UID changed", err)
	}
	assertNoDeleteActions(t, client)
}

func TestRunCleanupPreservesCandidateWhenFingerprintChanges(t *testing.T) {
	pod := legacyPod("user-1", "project-1", "legacy-uid")
	changed := pod.DeepCopy()
	changed.Annotations["anban.ai/pod-config-hash"] = "changed-hash"
	client := fake.NewSimpleClientset(completeDeployment(), pod)
	client.PrependReactor("get", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, changed, nil
	})

	opts := testOptions(true)
	opts.yes = true
	err := runCleanup(context.Background(), client, opts, strings.NewReader(""), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "fingerprint changed") {
		t.Fatalf("runCleanup() error = %v, want fingerprint changed", err)
	}
	assertNoDeleteActions(t, client)
}

func TestRunCleanupUsesResourceVersionPreconditionAgainstLastMomentChanges(t *testing.T) {
	pod := legacyPod("user-1", "project-1", "legacy-uid")
	client := fake.NewSimpleClientset(completeDeployment(), pod)
	client.PrependReactor("delete", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		preconditions := action.(k8stesting.DeleteAction).GetDeleteOptions().Preconditions
		if preconditions == nil || preconditions.ResourceVersion == nil {
			return true, nil, nil
		}
		return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "pods"}, pod.Name, errors.New("resource version changed"))
	})

	opts := testOptions(true)
	opts.yes = true
	err := runCleanup(context.Background(), client, opts, strings.NewReader(""), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "resource version changed") {
		t.Fatalf("runCleanup() error = %v, want resource version conflict preserving changed pod", err)
	}
}

func TestRunCleanupRejectsDeploymentIdentityChanges(t *testing.T) {
	for _, field := range []string{"UID", "generation"} {
		t.Run(field, func(t *testing.T) {
			pod := legacyPod("user-1", "project-1", "legacy-uid")
			deployment := completeDeployment()
			client := fake.NewSimpleClientset(deployment, pod)
			gets := 0
			client.PrependReactor("get", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
				gets++
				observed := deployment.DeepCopy()
				if gets >= 2 {
					if field == "UID" {
						observed.UID = types.UID("replacement-deployment")
					} else {
						observed.Generation++
						observed.Status.ObservedGeneration = observed.Generation
					}
				}
				return true, observed, nil
			})

			opts := testOptions(true)
			opts.yes = true
			err := runCleanup(context.Background(), client, opts, strings.NewReader(""), &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), "deployment changed") {
				t.Fatalf("runCleanup() error = %v, want deployment changed", err)
			}
			assertNoDeleteActions(t, client)
		})
	}
}

func TestRunCleanupRejectsRolloutBecomingIncomplete(t *testing.T) {
	pod := legacyPod("user-1", "project-1", "legacy-uid")
	deployment := completeDeployment()
	client := fake.NewSimpleClientset(deployment, pod)
	gets := 0
	client.PrependReactor("get", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		gets++
		observed := deployment.DeepCopy()
		if gets >= 2 {
			observed.Status.UpdatedReplicas = 0
		}
		return true, observed, nil
	})

	opts := testOptions(true)
	opts.yes = true
	err := runCleanup(context.Background(), client, opts, strings.NewReader(""), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "rollout is not complete") {
		t.Fatalf("runCleanup() error = %v, want rollout completeness error", err)
	}
	assertNoDeleteActions(t, client)
}

func TestRunCleanupContinuesAfterDeleteErrorsAndUsesUIDPreconditions(t *testing.T) {
	first := legacyPod("user-1", "project-1", "first-uid")
	second := legacyPod("user-2", "project-2", "second-uid")
	client := fake.NewSimpleClientset(completeDeployment(), first, second)
	client.PrependReactor("delete", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		deleteAction := action.(k8stesting.DeleteAction)
		return true, nil, fmt.Errorf("injected delete failure for %s", deleteAction.GetName())
	})

	opts := testOptions(true)
	opts.yes = true
	err := runCleanup(context.Background(), client, opts, strings.NewReader(""), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), first.Name) || !strings.Contains(err.Error(), second.Name) || strings.Count(err.Error(), "injected delete failure") != 2 {
		t.Fatalf("runCleanup() error = %v, want both delete errors aggregated", err)
	}
	assertDeleted(t, client, first.Name, first.UID)
	assertDeleted(t, client, second.Name, second.UID)
	deploymentGets := 0
	for _, action := range client.Actions() {
		if action.GetVerb() == "get" && action.GetResource().Resource == "deployments" {
			deploymentGets++
		}
	}
	if deploymentGets != 4 {
		t.Fatalf("deployment GET count = %d, want initial snapshot, pre-delete check, and one check per candidate", deploymentGets)
	}
}

func TestRunCleanupTreatsDeleteNotFoundAsSuccess(t *testing.T) {
	pod := legacyPod("user-1", "project-1", "legacy-uid")
	client := fake.NewSimpleClientset(completeDeployment(), pod)
	client.PrependReactor("delete", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, pod.Name)
	})
	opts := testOptions(true)
	opts.yes = true
	if err := runCleanup(context.Background(), client, opts, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
	assertDeleted(t, client, pod.Name, pod.UID)
}

func TestRunCleanupRequiresNamespaceAndDeployment(t *testing.T) {
	for _, mutate := range []func(*options){
		func(opts *options) { opts.namespace = "" },
		func(opts *options) { opts.deploymentName = "" },
	} {
		opts := testOptions(false)
		mutate(&opts)
		client := fake.NewSimpleClientset()
		if err := runCleanup(context.Background(), client, opts, strings.NewReader(""), &bytes.Buffer{}); err == nil {
			t.Fatal("runCleanup() error = nil, want required option error")
		}
		if len(client.Actions()) != 0 {
			t.Fatalf("client actions = %#v, want none", client.Actions())
		}
	}
}

func completeDeployment() *appsv1.Deployment {
	replicas := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "creator-server", Namespace: "anban", UID: types.UID("deployment-uid"), Generation: 7},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration:  7,
			Replicas:            1,
			UpdatedReplicas:     1,
			ReadyReplicas:       1,
			AvailableReplicas:   1,
			UnavailableReplicas: 0,
		},
	}
}

func legacyPod(userID, projectID, uid string) *corev1.Pod {
	knownNames := map[string]string{
		"user-1/project-1": "anban-agent-user-1-project-1-94df591a66",
		"user-2/project-2": "anban-agent-user-2-project-2-8067f3da20",
	}
	name, ok := knownNames[userID+"/"+projectID]
	if !ok {
		panic(fmt.Sprintf("missing deterministic test name for %s/%s", userID, projectID))
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       "anban",
			UID:             types.UID(uid),
			ResourceVersion: "1",
			Labels: map[string]string{
				"app.kubernetes.io/name": "anban-agent",
				"anban.ai/user-id":       userID,
				"anban.ai/project-id":    projectID,
			},
			Annotations: map[string]string{"anban.ai/pod-config-hash": "hash-" + projectID},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "agent"}}},
	}
}

func testOptions(execute bool) options {
	return options{
		namespace:      "anban",
		deploymentName: "creator-server",
		execute:        execute,
		confirmDrained: execute,
	}
}

func assertNoDeleteActions(t *testing.T, client *fake.Clientset) {
	t.Helper()
	for _, action := range client.Actions() {
		if action.GetVerb() == "delete" {
			t.Fatalf("unexpected delete action: %#v", action)
		}
	}
}

func assertDeleted(t *testing.T, client *fake.Clientset, name string, uid types.UID) {
	t.Helper()
	for _, action := range client.Actions() {
		if action.GetVerb() != "delete" || action.GetResource().Resource != "pods" || action.(k8stesting.DeleteAction).GetName() != name {
			continue
		}
		options := action.(k8stesting.DeleteAction).GetDeleteOptions()
		if options.Preconditions == nil || options.Preconditions.UID == nil || *options.Preconditions.UID != uid {
			t.Fatalf("delete %s preconditions = %#v, want UID %q", name, options.Preconditions, uid)
		}
		return
	}
	t.Fatalf("delete action for %s not found", name)
}
