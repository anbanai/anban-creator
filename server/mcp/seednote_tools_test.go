package mcp

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
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

func TestSeednoteLoginResultsContainStateWithoutCrossToolSequencing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/login/status":
			_, _ = w.Write([]byte(`{"success":true,"logged_in":false}`))
		case "/api/v1/login/qrcode":
			png := base64.StdEncoding.EncodeToString([]byte("png"))
			_, _ = w.Write([]byte(`{"success":true,"data":{"qrcode_image":"` + png + `"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	old := svcs
	svcs = &Services{SeednoteCapabilitySvc: service.NewSeednoteCapabilityService(
		seednote.NewClient(server.URL, time.Second), seednoteReadiness(true),
	)}
	t.Cleanup(func() { svcs = old })

	status, err := checkSeednoteLoginStatusHandler(context.Background(), &mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	statusText := strings.ToLower(callToolText(status))
	if strings.Contains(statusText, "get_seednote_login_qrcode") || strings.Contains(statusText, "二维码") {
		t.Fatalf("login status contains QR workflow sequencing: %s", statusText)
	}

	qr, err := getSeednoteLoginQRCodeHandler(context.Background(), &mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(qr.Content) != 1 {
		t.Fatalf("QR result content count = %d, want image state only", len(qr.Content))
	}
	if _, ok := qr.Content[0].(*mcp.ImageContent); !ok {
		t.Fatalf("QR result content = %T, want image", qr.Content[0])
	}
}
