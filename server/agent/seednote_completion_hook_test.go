package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSeednoteNativeCompletionHookSkipsFailedDelivery(t *testing.T) {
	workspace := t.TempDir()
	output := filepath.Join(workspace, "output")
	if err := os.Mkdir(output, 0o755); err != nil {
		t.Fatal(err)
	}
	failure := filepath.Join(output, "failure-state.json")
	if err := os.WriteFile(failure, []byte(`{"version":"1.0","status":"recoverable_failure","stage":"image_generation","error_code":"image_review_unavailable","message":"review unavailable","resume_from":"image_generation"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	metadata := filepath.Join(output, "completion-metadata.json")
	run := func() {
		t.Helper()
		cmd := exec.Command("bash", filepath.Join(repoRoot(t), "harness", "hooks", "completion-metadata.sh"))
		cmd.Env = append(os.Environ(), "CLAUDE_PROJECT_DIR="+workspace, "ANBAN_TASK_TYPE=seednote")
		if data, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("completion hook: %v\n%s", err, data)
		}
	}
	run()
	if _, err := os.Stat(metadata); !os.IsNotExist(err) {
		t.Fatalf("failed delivery produced success metadata: %v", err)
	}
	if err := os.Remove(failure); err != nil {
		t.Fatal(err)
	}
	run()
	if _, err := os.Stat(metadata); err != nil {
		t.Fatalf("recovered delivery did not produce completion metadata: %v", err)
	}
}
