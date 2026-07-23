package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anbanai/anban-creator/server/seednote"
	"github.com/anbanai/anban-creator/server/service"
)

type seednoteReadiness bool

func (r seednoteReadiness) Ready() bool { return bool(r) }

func TestCheckSeednoteLoginStatusReportsUnavailableWhenSidecarNotReady(t *testing.T) {
	old := svcs
	svcs = &Services{
		SeednoteCapabilitySvc: service.NewSeednoteCapabilityService(
			seednote.NewClient("http://127.0.0.1:1", time.Second), seednoteReadiness(false),
		),
	}
	t.Cleanup(func() { svcs = old })

	result, err := checkSeednoteLoginStatusHandler(context.Background(), &mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	payload := decodeMCPMap(t, result)
	if payload["available"] != false || payload["logged_in"] != false {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	if msg, _ := payload["message"].(string); !strings.Contains(msg, "后台连接") {
		t.Fatalf("message = %q, want background connection hint", msg)
	}
}
