package claudecode

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestVendoredModuleExcludesClaudeWorkspaceConfig(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate vendored module root")
	}
	workspaceConfig := filepath.Join(filepath.Dir(testFile), ".claude")
	if _, err := os.Stat(workspaceConfig); !os.IsNotExist(err) {
		t.Fatalf("vendored module must not contain .claude workspace config: %v", err)
	}
}
