package agentpack

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

func validateDSHAgentSource(path string, body []byte) error {
	decoder := yaml.NewDecoder(bytes.NewReader(body))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		if err == io.EOF {
			return fmt.Errorf("%s must contain one YAML document", path)
		}
		return fmt.Errorf("decode %s: %w", path, err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
		return fmt.Errorf("%s must contain exactly one YAML document", path)
	}
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 {
		return fmt.Errorf("%s must contain one YAML document", path)
	}

	root := document.Content[0]
	if root.Kind != yaml.SequenceNode {
		return fmt.Errorf("%s root must be a sequence", path)
	}
	ids := make(map[string]bool, len(root.Content))
	for i, row := range root.Content {
		if row.Kind != yaml.MappingNode {
			return fmt.Errorf("%s row %d must be a mapping", path, i+1)
		}
		id, name := "", ""
		idSeen, nameSeen := false, false
		for j := 0; j < len(row.Content); j += 2 {
			key, value := row.Content[j], row.Content[j+1]
			if key.Kind != yaml.ScalarNode {
				continue
			}
			switch key.Value {
			case "id":
				if idSeen {
					return fmt.Errorf("%s row %d has duplicate id key", path, i+1)
				}
				idSeen = true
				if value.Kind == yaml.ScalarNode && value.Tag == "!!str" {
					id = strings.TrimSpace(value.Value)
				}
			case "name":
				if nameSeen {
					return fmt.Errorf("%s row %d has duplicate name key", path, i+1)
				}
				nameSeen = true
				if value.Kind == yaml.ScalarNode && value.Tag == "!!str" {
					name = strings.TrimSpace(value.Value)
				}
			}
		}
		if id == "" {
			return fmt.Errorf("%s row %d id is required and must be a string", path, i+1)
		}
		if name == "" {
			return fmt.Errorf("%s row %d name is required and must be a string", path, i+1)
		}
		if ids[id] {
			return fmt.Errorf("%s row %d has duplicate id %q", path, i+1, id)
		}
		ids[id] = true
	}
	return nil
}
