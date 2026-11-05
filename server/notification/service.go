package notification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Channel is a delivery configuration that the calling application has already
// resolved and authorized. Database identifiers and ownership remain in the
// application service; this package does not load channels or grant access.
type Channel struct {
	Type    NotifyType
	Config  json.RawMessage
	Enabled bool
}

// Service dispatches a single notification. Persistence, authorization and
// durable retries belong to the application service, not the transport SDK.
type Service struct{ registry *SenderRegistry }

func NewService(registry *SenderRegistry) *Service {
	if registry == nil {
		registry = NewSenderRegistry()
	}
	return &Service{registry: registry}
}

var ErrNotConfigured = errors.New("notification channel not configured")

func (s *Service) Send(ctx context.Context, channel *Channel, content *NotifyContent) error {
	return s.SendByChannel(ctx, channel, "", content)
}

func (s *Service) SendByChannel(ctx context.Context, channel *Channel, recipient string, content *NotifyContent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if channel == nil || channel.Type == NotifyTypeNone {
		return ErrNotConfigured
	}
	if !channel.Enabled {
		return errors.New("notification channel is disabled")
	}
	if content == nil {
		return errors.New("notification content is required")
	}
	sender, err := s.registry.Get(channel.Type)
	if err != nil {
		return fmt.Errorf("get sender: %w", err)
	}
	return sender.Send(ctx, channel.Config, recipient, content)
}

func (s *Service) TestChannel(ctx context.Context, channel *Channel, recipient string) error {
	return s.SendByChannel(ctx, channel, recipient, &NotifyContent{
		Title:   "测试通知",
		Content: "这是一条测试通知，如果您收到此消息，说明通知渠道配置正确。",
	})
}
