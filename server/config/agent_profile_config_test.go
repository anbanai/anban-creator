package config

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"gopkg.in/yaml.v3"
)

func TestAgentProfilesUseCompleteClaudeEnvs(t *testing.T) {
	wantIDs := []string{"balanced", "effective", "quality"}
	wantKeys := model.ClaudeProfileEnvKeys()
	sort.Strings(wantKeys)

	for _, name := range []string{"config.example.yaml"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", name))
			if err != nil {
				t.Fatal(err)
			}
			var configured struct {
				Claude ClaudeConfig `yaml:"claude"`
			}
			if err := yaml.Unmarshal(raw, &configured); err != nil {
				t.Fatal(err)
			}

			gotIDs := make([]string, 0, len(configured.Claude.ExecutionProfiles))
			for id := range configured.Claude.ExecutionProfiles {
				gotIDs = append(gotIDs, id)
			}
			sort.Strings(gotIDs)
			if !reflect.DeepEqual(gotIDs, wantIDs) {
				t.Fatalf("execution profile IDs = %#v, want %#v", gotIDs, wantIDs)
			}
			for id, profile := range configured.Claude.ExecutionProfiles {
				keys := make([]string, 0, len(profile.Envs))
				for key := range profile.Envs {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				if !reflect.DeepEqual(keys, wantKeys) {
					t.Fatalf("%s env keys = %#v, want %#v", id, keys, wantKeys)
				}
			}

			effective := configured.Claude.ExecutionProfiles["effective"]
			if effective.Provider != "deepseek" || effective.Envs[model.ClaudeEnvModel] != "deepseek-flash" || effective.Envs[model.ClaudeEnvAuthToken] != "${ANBAN_DEEPSEEK_API_KEY}" {
				t.Fatalf("effective profile = %#v", effective)
			}
			balanced := configured.Claude.ExecutionProfiles["balanced"]
			if balanced.Provider != "zhipu" || balanced.Envs[model.ClaudeEnvModel] != "glm-5.2" || len(balanced.ModelUsageAliases) != 0 {
				t.Fatalf("balanced profile = %#v", balanced)
			}
			quality := configured.Claude.ExecutionProfiles["quality"]
			if quality.Provider != "moonshot" || quality.Envs[model.ClaudeEnvModel] != "kimi-k3[1m]" || quality.Envs["CLAUDE_CODE_ALWAYS_ENABLE_EFFORT"] != "true" {
				t.Fatalf("quality profile = %#v", quality)
			}
		})
	}
}
