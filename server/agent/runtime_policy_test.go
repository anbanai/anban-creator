package agent

import (
	"reflect"
	"strings"
	"testing"

	claudecode "github.com/severity1/claude-agent-sdk-go"
)

func TestManagedAgentDisallowedTools(t *testing.T) {
	got := ManagedAgentDisallowedTools()
	want := []string{"Agent", "ScheduleWakeup"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ManagedAgentDisallowedTools() = %#v, want %#v", got, want)
	}

	got[0] = "Bash"
	again := ManagedAgentDisallowedTools()
	if !reflect.DeepEqual(again, want) {
		t.Fatalf("ManagedAgentDisallowedTools() did not return a copy: got %#v, want %#v", again, want)
	}
}

func TestManagedAgentRuntimePolicySetsSDKDisallowedTools(t *testing.T) {
	opts := claudecode.NewOptions(WithManagedAgentRuntimePolicy())
	want := []string{"Agent", "ScheduleWakeup"}
	if !reflect.DeepEqual(opts.DisallowedTools, want) {
		t.Fatalf("DisallowedTools = %#v, want %#v", opts.DisallowedTools, want)
	}
}

func TestManagedAgentExecutorsUseRuntimePolicy(t *testing.T) {
	for _, path := range []string{
		"executor.go",
		"../../agent/runner.go",
	} {
		t.Run(path, func(t *testing.T) {
			text := readRepoFile(t, path)
			if !strings.Contains(text, "WithManagedAgentRuntimePolicy()") {
				t.Fatalf("%s must apply WithManagedAgentRuntimePolicy()", path)
			}
		})
	}
}
