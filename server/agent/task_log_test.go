package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskLogWriterResultExcludesClaudeBillingFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task.log")
	writer, err := NewTaskLogWriter(path, "task-1")
	if err != nil {
		t.Fatalf("NewTaskLogWriter: %v", err)
	}
	writer.WriteResult(true, 1234, 7)
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	for _, forbidden := range []string{"Cost:", "tokens:", "usage:"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("task result log retained non-authoritative Claude field %q: %s", forbidden, raw)
		}
	}
	if !strings.Contains(string(raw), "Duration: 1234ms | Turns: 7") {
		t.Fatalf("task result log lost execution metadata: %s", raw)
	}
}
