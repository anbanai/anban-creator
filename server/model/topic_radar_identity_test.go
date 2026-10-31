package model

import "testing"

func TestTopicRadarIdentityIsRoutable(t *testing.T) {
	if !IsAgentID(AgentIDTopicRadar) {
		t.Fatal("topic radar agent should be recognized")
	}
	if !IsChannel(ChannelTopicRadar) {
		t.Fatal("topic radar channel should be recognized")
	}
	if !IsTaskKind(TaskKindTopicDigest) || !IsTaskKind(TaskKindTopicScript) {
		t.Fatal("topic radar task kinds should be recognized")
	}
	if channel, ok := AgentChannel(AgentIDTopicRadar); !ok || channel != ChannelTopicRadar {
		t.Fatalf("agent channel = %q, %v", channel, ok)
	}
	if got := TaskKindForTaskType(TaskTypeTopicDigest); got != TaskKindTopicDigest {
		t.Fatalf("digest task kind = %q", got)
	}
	if got := TaskTypeForTaskKind(TaskKindTopicScript); got != TaskTypeTopicScript {
		t.Fatalf("script task type = %q", got)
	}
}
