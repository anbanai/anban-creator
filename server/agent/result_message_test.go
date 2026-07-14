package agent

import (
	"strings"
	"testing"

	claudecode "github.com/severity1/claude-agent-sdk-go"
)

func TestResultMessageErrorPreservesProtocolDiagnostics(t *testing.T) {
	result := "legacy result"
	tests := []struct {
		name    string
		message *claudecode.ResultMessage
		tool    string
		toolErr string
		want    string
	}{
		{
			name:    "errors field has priority",
			message: &claudecode.ResultMessage{Errors: []string{" provider rejected request ", "retry later"}, Result: &result, Subtype: "error_during_execution"},
			want:    "provider rejected request; retry later",
		},
		{
			name:    "legacy result remains supported",
			message: &claudecode.ResultMessage{Result: &result, Subtype: "error_during_execution"},
			want:    "legacy result",
		},
		{
			name:    "maximum turns is actionable",
			message: &claudecode.ResultMessage{Subtype: "error_max_turns", NumTurns: 50},
			want:    "agent reached maximum turn budget (subtype=error_max_turns, num_turns=50)",
		},
		{
			name:    "subtype retains last tool diagnostic",
			message: &claudecode.ResultMessage{Subtype: "error_during_execution", NumTurns: 12},
			tool:    "Bash",
			toolErr: "command failed",
			want:    "agent execution failed (subtype=error_during_execution, num_turns=12); last tool error from Bash: command failed",
		},
		{
			name: "missing message remains explicit",
			want: "agent execution failed without a result message",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ResultMessageError(test.message, test.tool, test.toolErr); got != test.want {
				t.Fatalf("ResultMessageError() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestResultMessageErrorBoundsUpstreamDiagnostic(t *testing.T) {
	oversized := strings.Repeat("错", maxResultMessageErrorRunes+1)
	got := ResultMessageError(&claudecode.ResultMessage{Errors: []string{oversized}}, "", "")
	if len([]rune(got)) != maxResultMessageErrorRunes+len([]rune("...(truncated)")) || !strings.HasSuffix(got, "...(truncated)") {
		t.Fatalf("bounded diagnostic has %d runes, truncated=%t", len([]rune(got)), strings.HasSuffix(got, "...(truncated)"))
	}
}
