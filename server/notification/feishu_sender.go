package notification

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/sdk/feishubot"
)

type feishuConfig struct {
	WebhookKey string `json:"webhook_key"`
}

type FeishuSender struct{ HTTPClient *http.Client }

func (s *FeishuSender) Send(ctx context.Context, config json.RawMessage, _ string, content *NotifyContent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if content == nil {
		return errors.New("notification content is required")
	}
	var cfg feishuConfig
	if err := json.Unmarshal(config, &cfg); err != nil {
		return errors.New("invalid feishu notification configuration")
	}
	if strings.TrimSpace(cfg.WebhookKey) == "" {
		return errors.New("feishu webhook key is required")
	}
	var options []feishubot.ClientOption
	if s.HTTPClient != nil {
		options = append(options, feishubot.WithHTTPClient(s.HTTPClient))
	}
	options = append(options, feishubot.WithTimeout(5*time.Second))
	bot := feishubot.NewClient(cfg.WebhookKey, options...)
	message := feishubot.NewInteractiveMessage().SetHeader(content.Title, "blue").AddText(content.Content)
	if _, err := bot.SendMessageContext(ctx, message); err != nil {
		return safeSendError("feishu", err)
	}
	return nil
}
func (s *FeishuSender) Type() NotifyType { return NotifyTypeLark }
