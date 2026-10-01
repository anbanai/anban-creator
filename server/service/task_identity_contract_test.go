package service

import (
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestResolveTaskIdentityRequiresExplicitIdentity(t *testing.T) {
	_, _, _, _, err := resolveTaskIdentity(&model.Project{Platform: model.PlatformWechat}, CreateManualParams{})
	if err == nil || !strings.Contains(err.Error(), "agent_id, channel, and task_kind are required") {
		t.Fatalf("resolveTaskIdentity error = %v, want explicit identity requirement", err)
	}
}

func TestResolveTaskIdentityRejectsAgentChannelMismatch(t *testing.T) {
	_, _, _, _, err := resolveTaskIdentity(nil, CreateManualParams{
		AgentID:  model.AgentIDArticle,
		Channel:  model.ChannelSeednote,
		TaskKind: model.TaskKindContentGeneration,
	})
	if err == nil || !strings.Contains(err.Error(), "bound to channel") {
		t.Fatalf("resolveTaskIdentity error = %v, want channel mismatch", err)
	}
}

func TestResolveTaskIdentityRejectsUnsupportedTaskKind(t *testing.T) {
	_, _, _, _, err := resolveTaskIdentity(nil, CreateManualParams{
		AgentID:  model.AgentIDArticle,
		Channel:  model.ChannelArticle,
		TaskKind: model.TaskKindViralAnalysis,
	})
	if err == nil || !strings.Contains(err.Error(), "does not support task kind") {
		t.Fatalf("resolveTaskIdentity error = %v, want unsupported task kind", err)
	}
}
