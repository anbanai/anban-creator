package agent

import claudecode "github.com/severity1/claude-agent-sdk-go"

func ManagedAgentDisallowedTools() []string {
	return []string{"Agent", "ScheduleWakeup"}
}

func WithManagedAgentRuntimePolicy() claudecode.Option {
	return claudecode.WithDisallowedTools(ManagedAgentDisallowedTools()...)
}
