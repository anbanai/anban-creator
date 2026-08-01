package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/anbanai/anban-creator/server/agentpack"
)

var ErrInvalidAgentInput = errors.New("invalid agent input")

func validateAndCloneAgentInput(taskType string, input map[string]any) (map[string]any, error) {
	pack, ok := agentpack.Default().ForTaskType(taskType)
	if !ok {
		return nil, fmt.Errorf("%w: no Agent Pack is bound to task type %q", ErrInvalidAgentInput, taskType)
	}
	if err := agentpack.ValidateTaskInput(pack, input); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidAgentInput, err)
	}
	if input == nil {
		return nil, nil
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("%w: encode agent_input: %v", ErrInvalidAgentInput, err)
	}
	var cloned map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&cloned); err != nil {
		return nil, fmt.Errorf("%w: decode agent_input: %v", ErrInvalidAgentInput, err)
	}
	return cloned, nil
}
