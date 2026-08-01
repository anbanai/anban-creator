package agentpack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
)

type basicSchema struct {
	Schema               string                     `json:"$schema"`
	Type                 string                     `json:"type"`
	Title                string                     `json:"title"`
	Description          string                     `json:"description"`
	Format               string                     `json:"format"`
	Default              json.RawMessage            `json:"default"`
	Required             []string                   `json:"required"`
	Properties           map[string]json.RawMessage `json:"properties"`
	AdditionalProperties *bool                      `json:"additionalProperties"`
	Enum                 []any                      `json:"enum"`
	Items                json.RawMessage            `json:"items"`
	Minimum              *float64                   `json:"minimum"`
	Maximum              *float64                   `json:"maximum"`
}

func validateExtensionSchemaDocument(raw json.RawMessage, path string, allowComplex bool) error {
	schema, err := decodeBasicSchema(raw, path)
	if err != nil {
		return err
	}
	if schema.Type != "object" {
		return fmt.Errorf("%s root type must be \"object\"", path)
	}
	if !allowComplex && (schema.AdditionalProperties == nil || *schema.AdditionalProperties) {
		return fmt.Errorf("%s additionalProperties must be false for the generic form", path)
	}
	return validateSchemaContract(raw, path, 0, allowComplex, true)
}

func validateSchemaContract(raw json.RawMessage, path string, depth int, allowComplex, root bool) error {
	if depth > 16 {
		return fmt.Errorf("%s exceeds maximum Schema depth", path)
	}
	schema, err := decodeBasicSchema(raw, path)
	if err != nil {
		return err
	}
	if schema.Type == "" {
		return fmt.Errorf("%s Schema type is required", path)
	}
	if !root && !allowComplex && (schema.Type == "object" || schema.Type == "array") {
		return fmt.Errorf("%s type %q requires ui.renderer custom:<key>", path, schema.Type)
	}
	if err := validateSchemaEnum(raw, schema, path, depth); err != nil {
		return err
	}
	if len(schema.Default) > 0 {
		value, err := decodeSchemaValue(schema.Default, path+".default")
		if err != nil {
			return err
		}
		if err := validateSchemaValue(raw, value, path+".default", depth); err != nil {
			return err
		}
	}
	switch schema.Type {
	case "object":
		requiredSet := make(map[string]bool, len(schema.Required))
		for _, required := range schema.Required {
			if _, ok := schema.Properties[required]; !ok {
				return fmt.Errorf("%s required property %q is not declared", path, required)
			}
			requiredSet[required] = true
		}
		for key, child := range schema.Properties {
			childPath := path + ".properties." + key
			if err := validateSchemaContract(child, childPath, depth+1, allowComplex, false); err != nil {
				return err
			}
			if requiredSet[key] {
				childSchema, err := decodeBasicSchema(child, childPath)
				if err != nil {
					return err
				}
				if childSchema.Type == "boolean" && len(childSchema.Default) == 0 {
					if err := validateSchemaValue(child, false, childPath+".implicit_default", depth+1); err != nil {
						return fmt.Errorf("%s required boolean implicit default is invalid: %w", childPath, err)
					}
				}
			}
		}
	case "array":
		if len(schema.Items) == 0 {
			return fmt.Errorf("%s array Schema requires items", path)
		}
		return validateSchemaContract(schema.Items, path+".items", depth+1, allowComplex, false)
	case "string", "boolean", "number", "integer":
		if schema.Minimum != nil && schema.Maximum != nil && *schema.Minimum > *schema.Maximum {
			return fmt.Errorf("%s minimum must not exceed maximum", path)
		}
	default:
		return fmt.Errorf("%s uses unsupported Schema type %q", path, schema.Type)
	}
	return nil
}

func validateSchemaEnum(raw json.RawMessage, schema basicSchema, path string, depth int) error {
	seenValues := make(map[string]bool, len(schema.Enum))
	seenUIValues := make(map[string]bool, len(schema.Enum))
	for i, candidate := range schema.Enum {
		candidatePath := fmt.Sprintf("%s.enum[%d]", path, i)
		if err := validateSchemaValue(raw, candidate, candidatePath, depth); err != nil {
			return err
		}
		normalized, err := json.Marshal(normalizeJSONNumber(candidate))
		if err != nil {
			return fmt.Errorf("encode %s: %w", candidatePath, err)
		}
		key := string(normalized)
		if seenValues[key] {
			return fmt.Errorf("%s has duplicate enum value %s", path, key)
		}
		seenValues[key] = true

		if schema.Type == "string" || schema.Type == "boolean" || schema.Type == "number" || schema.Type == "integer" {
			uiKey := fmt.Sprint(normalizeJSONNumber(candidate))
			if seenUIValues[uiKey] {
				return fmt.Errorf("%s has duplicate enum UI value %q", path, uiKey)
			}
			seenUIValues[uiKey] = true
		}
	}
	return nil
}

func decodeSchemaValue(raw json.RawMessage, path string) (any, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode Schema value at %s: %w", path, err)
	}
	return value, nil
}

func decodeBasicSchema(raw json.RawMessage, path string) (basicSchema, error) {
	var schema basicSchema
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&schema); err != nil {
		return basicSchema{}, fmt.Errorf("decode Schema at %s: %w", path, err)
	}
	return schema, nil
}

func ValidateTaskInput(pack Manifest, input map[string]any) error {
	if pack.Schemas == nil || len(pack.Schemas.TaskInput) == 0 {
		if len(input) == 0 {
			return nil
		}
		return fmt.Errorf("Agent Pack %q does not declare task input", pack.ID)
	}
	return validateSchemaValue(pack.Schemas.TaskInput, input, "agent_input", 0)
}

func ValidateProjectConfig(pack Manifest, config map[string]any) error {
	if pack.Schemas == nil || len(pack.Schemas.ProjectConfig) == 0 {
		if len(config) == 0 {
			return nil
		}
		return fmt.Errorf("Agent Pack %q does not declare project config", pack.ID)
	}
	return validateSchemaValue(pack.Schemas.ProjectConfig, config, "agent_config", 0)
}

func validateSchemaValue(raw json.RawMessage, value any, path string, depth int) error {
	if depth > 16 {
		return fmt.Errorf("%s exceeds maximum Schema depth", path)
	}
	schema, err := decodeBasicSchema(raw, path)
	if err != nil {
		return err
	}
	if len(schema.Enum) > 0 {
		matched := false
		for _, candidate := range schema.Enum {
			if reflect.DeepEqual(normalizeJSONNumber(candidate), normalizeJSONNumber(value)) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%s is not an allowed value", path)
		}
	}
	switch schema.Type {
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", path)
		}
		for _, required := range schema.Required {
			if _, ok := object[required]; !ok {
				return fmt.Errorf("%s.%s is required", path, required)
			}
		}
		for key, child := range object {
			childSchema, declared := schema.Properties[key]
			if !declared {
				if schema.AdditionalProperties != nil && !*schema.AdditionalProperties {
					return fmt.Errorf("%s.%s is not allowed", path, key)
				}
				continue
			}
			if err := validateSchemaValue(childSchema, child, path+"."+key, depth+1); err != nil {
				return err
			}
		}
	case "array":
		array, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", path)
		}
		if len(schema.Items) > 0 {
			for i, child := range array {
				if err := validateSchemaValue(schema.Items, child, fmt.Sprintf("%s[%d]", path, i), depth+1); err != nil {
					return err
				}
			}
		}
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s must be a string", path)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be a boolean", path)
		}
	case "number", "integer":
		number, ok := jsonNumber(value)
		if !ok || (schema.Type == "integer" && math.Trunc(number) != number) {
			return fmt.Errorf("%s must be an %s", path, schema.Type)
		}
		if schema.Minimum != nil && number < *schema.Minimum {
			return fmt.Errorf("%s must be at least %v", path, *schema.Minimum)
		}
		if schema.Maximum != nil && number > *schema.Maximum {
			return fmt.Errorf("%s must be at most %v", path, *schema.Maximum)
		}
	case "":
		return fmt.Errorf("%s Schema type is required", path)
	default:
		return fmt.Errorf("%s uses unsupported Schema type %q", path, schema.Type)
	}
	return nil
}

func jsonNumber(value any) (float64, bool) {
	switch number := value.(type) {
	case float64:
		return number, true
	case float32:
		return float64(number), true
	case int:
		return float64(number), true
	case int64:
		return float64(number), true
	case json.Number:
		parsed, err := number.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func normalizeJSONNumber(value any) any {
	if number, ok := jsonNumber(value); ok {
		return number
	}
	return value
}
