package router

import (
	"io"
	"testing"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/handler"
	"github.com/rs/zerolog"
)

func TestAnalyticsImportsExposeSelectionAndRevocationWithoutLegacyResolve(t *testing.T) {
	logger := zerolog.New(io.Discard)
	app := NewRouter(&Services{
		Config: &config.Config{}, Logger: &logger,
		WechatAnalyticsImportHandler: handler.NewWechatAnalyticsImportHandler(nil, &logger),
		SeednoteImportHandler:        handler.NewSeednoteImportHandler(nil, &logger),
	})
	routes := make(map[string]bool)
	for _, route := range app.GetRoutes() {
		routes[route.Method+" "+route.Path] = true
	}
	for _, platform := range []string{"wechat", "seednote"} {
		prefix := "POST /api/v1/projects/:id/" + platform + "-analytics/imports"
		for _, suffix := range []string{"", "/preview", "/:batchId/revoke"} {
			if !routes[prefix+suffix] {
				t.Errorf("missing import route: %s", prefix+suffix)
			}
		}
		if routes[prefix+"/:batchId/resolve"] {
			t.Errorf("legacy resolve route still registered for %s", platform)
		}
	}
	if routes["POST /api/v1/wechat-analytics/imports/:batchId/resolve"] {
		t.Error("legacy global resolve route still registered")
	}
}
