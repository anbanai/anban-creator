package notification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/sdk/dingtalkbot"
)

type dingTalkConfig struct {
	WebhookKey string `json:"webhook_key"`
}

type DingTalkSender struct{ HTTPClient *http.Client }

func (s *DingTalkSender) Send(ctx context.Context, config json.RawMessage, _ string, content *NotifyContent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if content == nil {
		return errors.New("notification content is required")
	}
	var cfg dingTalkConfig
	if err := json.Unmarshal(config, &cfg); err != nil {
		return errors.New("invalid dingtalk notification configuration")
	}
	if strings.TrimSpace(cfg.WebhookKey) == "" {
		return errors.New("dingtalk webhook key is required")
	}
	var options []dingtalkbot.ClientOption
	if s.HTTPClient != nil {
		options = append(options, dingtalkbot.WithHTTPClient(s.HTTPClient))
	}
	options = append(options, dingtalkbot.WithTimeout(5*time.Second))
	bot := dingtalkbot.NewClient(cfg.WebhookKey, options...)
	if _, err := bot.SendMessageContext(ctx, dingtalkbot.NewMarkdownMessage(content.Title, content.Content)); err != nil {
		return safeSendError("dingtalk", err)
	}
	return nil
}
func (s *DingTalkSender) Type() NotifyType { return NotifyTypeDingTalk }

// safeSendError keeps credentials out of user-visible error strings while
// retaining errors.Is/errors.As behavior for the durable caller.
type safeSendErrorValue struct {
	provider string
	cause    error
}

func (e safeSendErrorValue) Error() string {
	return fmt.Sprintf("%s notification send failed", e.provider)
}
func (e safeSendErrorValue) Unwrap() error { return e.cause }
func safeSendError(provider string, err error) error {
	return safeSendErrorValue{provider: provider, cause: err}
}
