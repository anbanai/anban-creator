package notification

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	wework "github.com/anbanai/anban-creator/server/sdk/wework/robot"
)

type weWorkConfig struct {
	WebhookKey string `json:"webhook_key"`
}

type WeWorkSender struct{ HTTPClient *http.Client }

func (s *WeWorkSender) Send(ctx context.Context, config json.RawMessage, _ string, content *NotifyContent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if content == nil {
		return errors.New("notification content is required")
	}
	var cfg weWorkConfig
	if err := json.Unmarshal(config, &cfg); err != nil {
		return errors.New("invalid wecom notification configuration")
	}
	if strings.TrimSpace(cfg.WebhookKey) == "" {
		return errors.New("wecom webhook key is required")
	}
	var options []wework.RobotOption
	if s.HTTPClient != nil {
		options = append(options, wework.WithHTTPClient(s.HTTPClient))
	}
	bot := wework.NewRobot(cfg.WebhookKey, options...)
	markdown := newMarkdownBuilder()
	if content.Title != "" {
		markdown.h2(content.Title)
	}
	markdown.text(content.Content)
	if err := bot.SendMarkdownContext(ctx, markdown.string()); err != nil {
		return safeSendError("wecom", err)
	}
	return nil
}
func (s *WeWorkSender) Type() NotifyType { return NotifyTypeWeWork }
