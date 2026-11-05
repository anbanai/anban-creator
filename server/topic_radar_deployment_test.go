package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTopicRadarIndependentDeploymentContract(t *testing.T) {
	contracts := map[string][]string{
		"deploy/docker/Dockerfile.agent-topic-radar": {"COPY harness/ /anbanai/", "COPY --from=builder /app/runtime-claude/dist", "USER 1000:1000", `CMD ["job"]`},
		"Makefile":                                {"TOPIC_RADAR_AGENT_IMAGE ?= creator-agent-topic-radar:latest", "docker-topic-radar-agent-image: docker-agent-build-check", "docker build -f deploy/docker/Dockerfile.agent-topic-radar -t $(TOPIC_RADAR_AGENT_IMAGE) ."},
		"scripts/agent-runtime-release.sh":        {`"topic-radar=${AGENT_IMAGE_TOPIC_RADAR:?AGENT_IMAGE_TOPIC_RADAR must be set}"`},
		"scripts/render-ack-server-deployment.sh": {`topic_radar_agent_image_repo=${topic_radar_agent_image_repo:-$(jq -r '.images["topic-radar"] // empty' "$manifest_path")}`, "export topic_radar_agent_image_repo"},
		"server/Deployment.yaml":                  {"ANBAN_AGENT_IMAGE_TOPIC_RADAR", "${topic_radar_agent_image_repo}"},
		"server/config.example.yaml":              {"ANBAN_AGENT_IMAGE_TOPIC_RADAR", "creator-agent-topic-radar:latest"},
	}
	for file, tokens := range contracts {
		t.Run(file, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("..", file))
			if err != nil {
				t.Fatal(err)
			}
			for _, token := range tokens {
				if !strings.Contains(string(body), token) {
					t.Errorf("missing contract %q", token)
				}
			}
		})
	}
}
