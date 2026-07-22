package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

func TestNewDockerExecutorValidatesPersistentArticleContainer(t *testing.T) {
	tests := []struct {
		name            string
		containerStatus int
		container       map[string]any
		wantErr         string
	}{
		{
			name:            "matching running container",
			containerStatus: http.StatusOK,
			container: persistentArticleContainerFixture(
				"sha256:configured-article",
				true,
				"running",
				[]map[string]any{{"Type": "bind", "Source": "/host/workspace", "Destination": "/workspace", "RW": true}},
			),
		},
		{
			name:            "missing container",
			containerStatus: http.StatusNotFound,
			wantErr:         `persistent Article container "creator-agent-article" is unavailable`,
		},
		{
			name:            "stopped container",
			containerStatus: http.StatusOK,
			container: persistentArticleContainerFixture(
				"sha256:configured-article",
				false,
				"exited",
				[]map[string]any{{"Type": "bind", "Source": "/host/workspace", "Destination": "/workspace", "RW": true}},
			),
			wantErr: `persistent Article container "creator-agent-article" is not running (status "exited")`,
		},
		{
			name:            "image ID mismatch",
			containerStatus: http.StatusOK,
			container: persistentArticleContainerFixture(
				"sha256:stale-article",
				true,
				"running",
				[]map[string]any{{"Type": "bind", "Source": "/host/workspace", "Destination": "/workspace", "RW": true}},
			),
			wantErr: `persistent Article container "creator-agent-article" uses image ID "sha256:stale-article", but configured Article image "configured-article:latest" resolves to "sha256:configured-article"`,
		},
		{
			name:            "missing workspace mount",
			containerStatus: http.StatusOK,
			container: persistentArticleContainerFixture(
				"sha256:configured-article",
				true,
				"running",
				nil,
			),
			wantErr: `persistent Article container "creator-agent-article" must have a writable mount at "/workspace"`,
		},
		{
			name:            "wrong workspace mount",
			containerStatus: http.StatusOK,
			container: persistentArticleContainerFixture(
				"sha256:configured-article",
				true,
				"running",
				[]map[string]any{{"Type": "bind", "Source": "/host/workspace", "Destination": "/workspace-old", "RW": true}},
			),
			wantErr: `persistent Article container "creator-agent-article" must have a writable mount at "/workspace"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var inspectedImage, inspectedContainer bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.Contains(r.URL.Path, "/images/configured-article:latest/json"):
					inspectedImage = true
					_ = json.NewEncoder(w).Encode(map[string]any{"Id": "sha256:configured-article"})
				case strings.Contains(r.URL.Path, "/containers/creator-agent-article/json"):
					inspectedContainer = true
					w.WriteHeader(test.containerStatus)
					if test.containerStatus == http.StatusNotFound {
						_ = json.NewEncoder(w).Encode(map[string]string{"message": "No such container: creator-agent-article"})
						return
					}
					_ = json.NewEncoder(w).Encode(test.container)
				default:
					t.Fatalf("unexpected Docker request: %s %s", r.Method, r.URL.Path)
				}
			}))
			defer server.Close()

			t.Setenv("DOCKER_HOST", server.URL)
			t.Setenv("DOCKER_API_VERSION", "1.44")
			logger := zerolog.Nop()
			executor, err := NewDockerExecutor(
				&logger,
				nil,
				nil,
				config.DockerConfig{},
				config.RuntimeImages{model.PlatformArticle: "configured-article:latest"},
				"",
				"",
				nil,
				nil,
				nil,
				nil,
				nil,
			)
			if executor != nil {
				defer executor.Close()
			}

			if !inspectedImage {
				t.Fatal("configured Article image was not inspected")
			}
			if !inspectedContainer {
				t.Fatal("persistent Article container was not inspected")
			}
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("NewDockerExecutor() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("NewDockerExecutor() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func persistentArticleContainerFixture(imageID string, running bool, status string, mounts []map[string]any) map[string]any {
	return map[string]any{
		"Id":    "persistent-article-container",
		"Image": imageID,
		"State": map[string]any{"Running": running, "Status": status},
		"Config": map[string]any{
			"Image": "stale-mutable-tag:latest",
		},
		"Mounts": mounts,
	}
}
