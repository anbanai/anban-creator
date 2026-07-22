package config

import (
	"strings"
	"testing"
)

func TestConfigRejectsLegacyManagedExecutorFields(t *testing.T) {
	tests := []struct {
		name  string
		field string
		value string
		want  string
	}{
		{name: "empty executor has no default fallback", field: "executor", value: "", want: "claude.executor must be 'docker' or 'kubernetes'"},
		{name: "local executor", field: "executor", value: "local", want: "claude.executor must be 'docker' or 'kubernetes'"},
		{name: "Docker article image", field: "docker", value: "\n    article_image: legacy", want: `unknown claude.docker config field "article_image"`},
		{name: "Docker image profiles", field: "docker", value: "\n    image_profiles: {}", want: `unknown claude.docker config field "image_profiles"`},
		{name: "Docker container name", field: "docker", value: "\n    container_name: legacy", want: `unknown claude.docker config field "container_name"`},
		{name: "Docker workspace dir", field: "docker", value: "\n    workspace_dir: /tmp/legacy", want: `unknown claude.docker config field "workspace_dir"`},
		{name: "Kubernetes article image", field: "kubernetes", value: "\n    article_image: legacy", want: `unknown claude.kubernetes config field "article_image"`},
		{name: "Kubernetes image profiles", field: "kubernetes", value: "\n    image_profiles: {}", want: `unknown claude.kubernetes config field "image_profiles"`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := validClaudeConfigYAML
			if test.field == "executor" {
				body = strings.Replace(body, "  executor: docker", "  executor: "+test.value, 1)
			} else {
				body = strings.Replace(body, "  env:\n", "  "+test.field+":"+test.value+"\n  env:\n", 1)
			}
			_, err := loadClaudeConfigYAML(t, body)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("NewConfig() error = %v, want containing %q", err, test.want)
			}
		})
	}
}
