package agentpack

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const nullableProfileInputSchema = `{
	"type":"object",
	"additionalProperties":false,
	"required":["current_profile"],
	"properties":{
		"current_profile":{
			"oneOf":[
				{
					"type":"object",
					"additionalProperties":false,
					"required":["version","positioning"],
					"properties":{
						"version":{"type":"integer","minimum":1},
						"positioning":{"type":"string"},
						"topics":{"type":"array","items":{"type":"string"}}
					}
				},
				{"type":"null"}
			]
		}
	}
}`

func TestValidateTaskInputNullableFrozenProfile(t *testing.T) {
	pack := Manifest{ID: "profile-analysis", Schemas: &SchemaDocuments{TaskInput: json.RawMessage(nullableProfileInputSchema)}}
	if err := validateExtensionSchemaDocument(pack.Schemas.TaskInput, "task-input.schema.json", true); err != nil {
		t.Fatalf("nullable frozen profile schema: %v", err)
	}
	tests := []struct {
		name  string
		input map[string]any
		valid bool
	}{
		{"initial profile", map[string]any{"current_profile": nil}, true},
		{"frozen profile", map[string]any{"current_profile": map[string]any{
			"version": float64(2), "positioning": "Studio users", "topics": []any{"writing"},
		}}, true},
		{"missing required profile", map[string]any{}, false},
		{"profile string", map[string]any{"current_profile": "unknown"}, false},
		{"profile array", map[string]any{"current_profile": []any{}}, false},
		{"missing profile field", map[string]any{"current_profile": map[string]any{"version": 1}}, false},
		{"wrong profile field type", map[string]any{"current_profile": map[string]any{"version": "1", "positioning": "Studio users"}}, false},
		{"invalid profile version", map[string]any{"current_profile": map[string]any{"version": 0, "positioning": "Studio users"}}, false},
		{"unknown profile field", map[string]any{"current_profile": map[string]any{"version": 1, "positioning": "Studio users", "unknown": true}}, false},
		{"wrong topic item type", map[string]any{"current_profile": map[string]any{"version": 1, "positioning": "Studio users", "topics": []any{1}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTaskInput(pack, tt.input)
			if (err == nil) != tt.valid {
				t.Fatalf("ValidateTaskInput(%#v) = %v, valid = %v", tt.input, err, tt.valid)
			}
		})
	}
}

func TestSchemaOneOfRequiresExactlyOneMatchingBranch(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		value  any
		valid  bool
	}{
		{"one integer branch", `{"oneOf":[{"type":"integer"},{"type":"string"}]}`, 2, true},
		{"no matching branch", `{"oneOf":[{"type":"integer"},{"type":"string"}]}`, false, false},
		{"overlapping numeric branches", `{"oneOf":[{"type":"integer"},{"type":"number"}]}`, 2, false},
		{"only number branch matches", `{"oneOf":[{"type":"integer"},{"type":"number"}]}`, 2.5, true},
		{"duplicate null branches", `{"oneOf":[{"type":"null"},{"type":"null"}]}`, nil, false},
		{"null only matches null", `{"type":"null"}`, false, false},
		{"single branch", `{"oneOf":[{"type":"null"}]}`, nil, true},
		{"sibling type restricts union", `{"type":"string","oneOf":[{"type":"string"},{"type":"null"}]}`, nil, false},
		{"sibling type permits string", `{"type":"string","oneOf":[{"type":"string"},{"type":"null"}]}`, "editor", true},
		{"sibling enum restricts union", `{"enum":["editor"],"oneOf":[{"type":"string"},{"type":"null"}]}`, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := json.RawMessage(tt.schema)
			if err := validateSchemaContract(raw, "schema", 0, true, false); err != nil {
				t.Fatalf("valid contract: %v", err)
			}
			err := validateSchemaValue(raw, tt.value, "input", 0)
			if (err == nil) != tt.valid {
				t.Fatalf("validateSchemaValue(%#v) = %v, valid = %v", tt.value, err, tt.valid)
			}
		})
	}
}

func TestSchemaOneOfRejectsMalformedContracts(t *testing.T) {
	for _, schema := range []string{
		`{"oneOf":[]}`,
		`{"oneOf":null}`,
		`{"oneOf":{}}`,
		`{"oneOf":[null,{"type":"null"}]}`,
		`{"oneOf":[true,{"type":"null"}]}`,
		`{"oneOf":[{}, {"type":"null"}]}`,
		`{"oneOf":[{"type":"missing"},{"type":"null"}]}`,
		`{"oneOf":[{"type":["string","null"]},{"type":"null"}]}`,
		`{"oneOf":[{"type":"array"},{"type":"null"}]}`,
		`{"oneOf":[{"type":"object","additionalProperties":false,"required":["missing"]},{"type":"null"}]}`,
		`{"oneOf":[{"type":"string","minLength":1},{"type":"null"}]}`,
		`{"oneOf":[{"$ref":"#/$defs/profile"},{"type":"null"}]}`,
		`{"oneOf":[{"type":"string"},{"type":"null"}],"$defs":{}}`,
		`{"oneOf":[{"type":"array","items":{"type":"string"},"uniqueItems":true},{"type":"null"}]}`,
	} {
		t.Run(schema, func(t *testing.T) {
			if err := validateSchemaContract(json.RawMessage(schema), "schema", 0, true, false); err == nil {
				t.Fatal("malformed oneOf contract accepted")
			}
			if err := validateSchemaValue(json.RawMessage(schema), nil, "input", 0); err == nil {
				t.Fatal("null branch hid a malformed oneOf contract during value validation")
			}
		})
	}
}

func TestSchemaOneOfValidatesDefaultsAndEnumMembers(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		valid  bool
	}{
		{"nullable default", `{"oneOf":[{"type":"string"},{"type":"null"}],"default":null}`, true},
		{"nullable enum", `{"oneOf":[{"type":"string"},{"type":"null"}],"enum":["editor",null]}`, true},
		{"wrong default", `{"oneOf":[{"type":"string"},{"type":"null"}],"default":2}`, false},
		{"ambiguous default", `{"oneOf":[{"type":"number"},{"type":"integer"}],"default":2}`, false},
		{"wrong enum member", `{"oneOf":[{"type":"string"},{"type":"null"}],"enum":[2]}`, false},
		{"duplicate null enum", `{"oneOf":[{"type":"string"},{"type":"null"}],"enum":[null,null]}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSchemaContract(json.RawMessage(tt.schema), "schema", 0, true, false)
			if (err == nil) != tt.valid {
				t.Fatalf("validateSchemaContract = %v, valid = %v", err, tt.valid)
			}
		})
	}
}

func TestSchemaOneOfPreservesExtensionRequirements(t *testing.T) {
	tests := []struct {
		name         string
		schema       string
		allowComplex bool
		want         string
	}{
		{"object root", `{"oneOf":[{"type":"null"}]}`, true, "root type must be"},
		{"closed root", `{"type":"object","properties":{"profile":{"oneOf":[{"type":"null"}]}}}`, true, "additionalProperties must be false"},
		{"closed object branch", `{"type":"object","additionalProperties":false,"properties":{"profile":{"oneOf":[{"type":"object"},{"type":"null"}]}}}`, true, "additionalProperties must be false"},
		{"generic form union", nullableProfileInputSchema, false, "requires ui.renderer"},
		{"generic form null", `{"type":"object","additionalProperties":false,"properties":{"profile":{"type":"null"}}}`, false, "requires ui.renderer"},
		{"required boolean implicit default", `{"type":"object","additionalProperties":false,"required":["enabled"],"properties":{"enabled":{"type":"boolean","enum":[true]}}}`, true, "required boolean implicit default"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExtensionSchemaDocument(json.RawMessage(tt.schema), "schema", tt.allowComplex)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateExtensionSchemaDocument = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestSchemaCustomRendererAllowsExplicitOpenNestedObjects(t *testing.T) {
	const schema = `{
		"type":"object",
		"additionalProperties":false,
		"properties":{
			"profile":{
				"oneOf":[
					{"type":"object","additionalProperties":false,"properties":{
						"content":{"type":"object","additionalProperties":true}
					}},
					{"type":"null"}
				]
			}
		}
	}`
	if err := validateExtensionSchemaDocument(json.RawMessage(schema), "schema", true); err != nil {
		t.Fatalf("custom renderer's explicitly open nested content: %v", err)
	}
	pack := Manifest{ID: "profile-analysis", Schemas: &SchemaDocuments{TaskInput: json.RawMessage(schema)}}
	if err := ValidateTaskInput(pack, map[string]any{"profile": map[string]any{
		"content": map[string]any{"positioning": "Studio", "format": []any{"notes"}, "details": map[string]any{"editor": true}},
	}}); err != nil {
		t.Fatalf("arbitrary profile content rejected: %v", err)
	}
	for _, tt := range []struct {
		name         string
		schema       string
		allowComplex bool
	}{
		{"open root", `{"type":"object","additionalProperties":true}`, true},
		{"undeclared nested openness", `{"type":"object","additionalProperties":false,"properties":{"content":{"type":"object"}}}`, true},
		{"generic nested open object", `{"type":"object","additionalProperties":false,"properties":{"content":{"type":"object","additionalProperties":true}}}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateExtensionSchemaDocument(json.RawMessage(tt.schema), "schema", tt.allowComplex); err == nil {
				t.Fatal("unsupported open object contract accepted")
			}
		})
	}
	if err := ValidateTaskInput(pack, map[string]any{"profile": map[string]any{"content": "invalid"}}); err == nil {
		t.Fatal("open content object accepted a string")
	}
}

func TestSchemaOneOfDepthIsBounded(t *testing.T) {
	for _, levels := range []int{16, 17} {
		t.Run(fmt.Sprint(levels), func(t *testing.T) {
			schema := `{"type":"null"}`
			for range levels {
				schema = `{"oneOf":[` + schema + `]}`
			}
			valid := levels == 16
			for _, check := range []struct {
				name string
				err  error
			}{
				{"contract", validateSchemaContract(json.RawMessage(schema), "schema", 0, true, false)},
				{"value", validateSchemaValue(json.RawMessage(schema), nil, "input", 0)},
			} {
				if (check.err == nil) != valid {
					t.Errorf("%s with %d levels = %v, valid = %v", check.name, levels, check.err, valid)
				}
			}
		})
	}
}
