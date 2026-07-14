package agent

import (
	"fmt"
	"strings"

	claudecode "github.com/severity1/claude-agent-sdk-go"
)

const maxResultMessageErrorRunes = 2000

// ResultMessageError preserves the Claude Code protocol diagnostic instead of
// replacing an empty legacy result field with an untraceable generic error.
func ResultMessageError(message *claudecode.ResultMessage, toolName, toolError string) string {
	if message == nil {
		return fallbackAgentError("agent execution failed without a result message", toolName, toolError)
	}

	errors := make([]string, 0, len(message.Errors))
	for _, item := range message.Errors {
		if item = compactResultMessageError(item); item != "" {
			errors = append(errors, item)
		}
	}
	if len(errors) > 0 {
		return strings.Join(errors, "; ")
	}
	if message.Result != nil {
		if result := compactResultMessageError(*message.Result); result != "" {
			return result
		}
	}

	subtype := strings.TrimSpace(message.Subtype)
	if subtype == "error_max_turns" {
		return fmt.Sprintf("agent reached maximum turn budget (subtype=%s, num_turns=%d)", subtype, message.NumTurns)
	}
	if subtype != "" {
		diagnostic := fmt.Sprintf("agent execution failed (subtype=%s, num_turns=%d)", subtype, message.NumTurns)
		if strings.TrimSpace(toolError) != "" {
			return diagnostic + "; " + fallbackAgentError("last tool failed", toolName, toolError)
		}
		return diagnostic
	}
	return fallbackAgentError("agent execution failed without a protocol diagnostic", toolName, toolError)
}

func compactResultMessageError(value string) string {
	value = strings.TrimSpace(strings.Join(strings.Fields(value), " "))
	runes := []rune(value)
	if len(runes) > maxResultMessageErrorRunes {
		return string(runes[:maxResultMessageErrorRunes]) + "...(truncated)"
	}
	return value
}
