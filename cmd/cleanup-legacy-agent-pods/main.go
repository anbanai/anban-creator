package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	serveragent "github.com/anbanai/anban-creator/server/agent"
)

const executeConfirmationToken = "delete legacy agent pods"

type options struct {
	namespace      string
	deploymentName string
	execute        bool
	confirmDrained bool
	yes            bool
}

type deploymentSnapshot struct {
	uid        types.UID
	generation int64
}

type podCandidate struct {
	name        string
	uid         types.UID
	fingerprint string
}

func main() {
	var opts options
	var kubeconfig, kubeContext string
	flag.StringVar(&opts.namespace, "namespace", "", "Kubernetes namespace containing the Server Deployment and legacy pods")
	flag.StringVar(&opts.deploymentName, "deployment", "", "Server Deployment name")
	flag.BoolVar(&opts.execute, "execute", false, "delete validated legacy pods (default is dry-run)")
	flag.BoolVar(&opts.confirmDrained, "confirm-drained", false, "attest that task intake is quiesced and all running tasks are drained")
	flag.BoolVar(&opts.yes, "yes", false, "skip the interactive execution confirmation")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "path to kubeconfig (defaults to standard clientcmd loading rules)")
	flag.StringVar(&kubeContext, "context", "", "kubeconfig context override")
	flag.Parse()

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		loadingRules.ExplicitPath = kubeconfig
	}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: kubeContext}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load Kubernetes config: %v\n", err)
		os.Exit(1)
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create Kubernetes client: %v\n", err)
		os.Exit(1)
	}
	if err := runCleanup(context.Background(), client, opts, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "cleanup legacy agent pods: %v\n", err)
		os.Exit(1)
	}
}

func runCleanup(ctx context.Context, client kubernetes.Interface, opts options, in io.Reader, out io.Writer) error {
	if strings.TrimSpace(opts.namespace) == "" {
		return errors.New("--namespace is required")
	}
	if strings.TrimSpace(opts.deploymentName) == "" {
		return errors.New("--deployment is required")
	}
	if opts.execute && !opts.confirmDrained {
		return errors.New("--execute requires --confirm-drained after task intake is quiesced and running tasks are drained")
	}

	deployment, err := client.AppsV1().Deployments(opts.namespace).Get(ctx, opts.deploymentName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get deployment %s/%s: %w", opts.namespace, opts.deploymentName, err)
	}
	if err := requireCompleteRollout(deployment); err != nil {
		return err
	}
	snapshot := deploymentSnapshot{uid: deployment.UID, generation: deployment.Generation}
	if snapshot.uid == "" {
		return fmt.Errorf("deployment %s/%s has an empty UID", opts.namespace, opts.deploymentName)
	}

	pods, err := client.CoreV1().Pods(opts.namespace).List(ctx, metav1.ListOptions{LabelSelector: serveragent.LegacyAgentPodLabelSelector()})
	if err != nil {
		return fmt.Errorf("list legacy agent pods: %w", err)
	}
	candidates := make([]podCandidate, 0, len(pods.Items))
	for i := range pods.Items {
		pod := &pods.Items[i]
		if !serveragent.IsValidatedLegacyAgentPod(pod) {
			continue
		}
		candidate := podCandidate{name: pod.Name, uid: pod.UID, fingerprint: legacyPodFingerprint(pod)}
		candidates = append(candidates, candidate)
		fmt.Fprintf(out, "%s\t%s\n", candidate.name, candidate.uid)
	}

	if !opts.execute {
		fmt.Fprintln(out, "Dry-run only. After quiescing task intake, draining all running tasks, and completing the Server rollout, rerun with --execute --confirm-drained (optionally --yes).")
		return nil
	}
	if !opts.yes {
		fmt.Fprintf(out, "Type %q to delete the listed pods: ", executeConfirmationToken)
		scanner := bufio.NewScanner(in)
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("read confirmation: %w", err)
			}
			return errors.New("confirmation not provided")
		}
		if scanner.Text() != executeConfirmationToken {
			return errors.New("confirmation token did not match; no pods were deleted")
		}
	}

	if err := revalidateDeployment(ctx, client, opts, snapshot); err != nil {
		return err
	}
	var cleanupErrors []error
	for _, candidate := range candidates {
		if err := revalidateDeployment(ctx, client, opts, snapshot); err != nil {
			return errors.Join(append(cleanupErrors, err)...)
		}
		pod, err := client.CoreV1().Pods(opts.namespace).Get(ctx, candidate.name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("re-get pod %s: %w", candidate.name, err))
			continue
		}
		if pod.UID != candidate.uid {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("pod %s UID changed from %q to %q; replacement preserved", candidate.name, candidate.uid, pod.UID))
			continue
		}
		if !serveragent.IsValidatedLegacyAgentPod(pod) || legacyPodFingerprint(pod) != candidate.fingerprint {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("pod %s fingerprint changed; pod preserved", candidate.name))
			continue
		}
		if pod.ResourceVersion == "" {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("pod %s resource version is empty; pod preserved", candidate.name))
			continue
		}
		uid := candidate.uid
		resourceVersion := pod.ResourceVersion
		err = client.CoreV1().Pods(opts.namespace).Delete(ctx, candidate.name, metav1.DeleteOptions{
			Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &resourceVersion},
		})
		if err != nil && !apierrors.IsNotFound(err) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("delete pod %s: %w", candidate.name, err))
		}
	}
	return errors.Join(cleanupErrors...)
}

func requireCompleteRollout(deployment *appsv1.Deployment) error {
	if deployment == nil {
		return errors.New("deployment is nil")
	}
	desired := int32(1)
	if deployment.Spec.Replicas != nil {
		desired = *deployment.Spec.Replicas
	}
	status := deployment.Status
	if status.ObservedGeneration < deployment.Generation || status.Replicas != desired || status.UpdatedReplicas != desired || status.ReadyReplicas != desired || status.AvailableReplicas != desired || status.UnavailableReplicas != 0 {
		return fmt.Errorf("deployment %s/%s rollout is not complete: generation=%d observed=%d desired=%d replicas=%d updated=%d ready=%d available=%d unavailable=%d",
			deployment.Namespace, deployment.Name, deployment.Generation, status.ObservedGeneration, desired,
			status.Replicas, status.UpdatedReplicas, status.ReadyReplicas, status.AvailableReplicas, status.UnavailableReplicas)
	}
	return nil
}

func revalidateDeployment(ctx context.Context, client kubernetes.Interface, opts options, snapshot deploymentSnapshot) error {
	deployment, err := client.AppsV1().Deployments(opts.namespace).Get(ctx, opts.deploymentName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("re-get deployment %s/%s: %w", opts.namespace, opts.deploymentName, err)
	}
	if deployment.UID != snapshot.uid || deployment.Generation != snapshot.generation {
		return fmt.Errorf("deployment changed during cleanup: UID %q to %q, generation %d to %d", snapshot.uid, deployment.UID, snapshot.generation, deployment.Generation)
	}
	if err := requireCompleteRollout(deployment); err != nil {
		return fmt.Errorf("deployment changed during cleanup: %w", err)
	}
	return nil
}

func legacyPodFingerprint(pod *corev1.Pod) string {
	return strings.Join([]string{
		pod.Name,
		pod.Labels["app.kubernetes.io/name"],
		pod.Labels["anban.ai/user-id"],
		pod.Labels["anban.ai/project-id"],
		pod.Annotations["anban.ai/pod-config-hash"],
		pod.Spec.Containers[0].Name,
	}, "\x00")
}
