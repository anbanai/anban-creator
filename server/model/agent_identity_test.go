package model

import "testing"

func TestVideoAgentsHaveStableChannelIdentities(t *testing.T) {
	for _, tt := range []struct {
		name    string
		agentID string
		channel string
	}{
		{name: "montage", agentID: AgentIDMontage, channel: ChannelMontage},
		{name: "hypit", agentID: AgentIDHypit, channel: ChannelHypit},
		{name: "whiteboard", agentID: AgentIDWhiteboard, channel: ChannelWhiteboard},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if !IsAgentID(tt.agentID) {
				t.Fatalf("IsAgentID(%q) = false", tt.agentID)
			}
			if got, ok := AgentChannel(tt.agentID); !ok || got != tt.channel {
				t.Fatalf("AgentChannel(%q) = %q, %v; want %q, true", tt.agentID, got, ok, tt.channel)
			}
		})
	}
}
