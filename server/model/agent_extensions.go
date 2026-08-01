package model

import "gorm.io/datatypes"

func (p *Project) SetAgentConfig(config map[string]any) {
	p.AgentConfig = datatypes.NewJSONType(config)
	p.AgentConfigSet = true
}

func (t *Task) SetAgentInput(input map[string]any) {
	t.AgentInput = datatypes.NewJSONType(input)
}

func (p *Plan) SetAgentInput(input map[string]any) {
	p.AgentInput = datatypes.NewJSONType(input)
}

func cloneAgentExtensionMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = cloneAgentExtensionValue(value)
	}
	return cloned
}

func cloneAgentExtensionValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneAgentExtensionMap(typed)
	case []any:
		cloned := make([]any, len(typed))
		for i, item := range typed {
			cloned[i] = cloneAgentExtensionValue(item)
		}
		return cloned
	default:
		return value
	}
}
