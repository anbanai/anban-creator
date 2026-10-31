package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// DeviceResolver returns the transport identity for a device that the caller is
// authorized to use. Applications supporting device delivery inject this
// capability; notification does not depend on a database or device model.
type DeviceResolver func(ctx context.Context, deviceID string) (deviceVx string, err error)

type wechatChannelConfig struct {
	DeviceID  string `json:"device_id"`
	ContactID string `json:"contact_id,omitempty"`
	RoomID    string `json:"room_id,omitempty"`
}

type wechatSender struct {
	resolveDevice DeviceResolver
	enqueuer      VxActionEnqueuer
	typ           NotifyType
}

func NewWeChatSender(resolveDevice DeviceResolver, enqueuer VxActionEnqueuer, typ NotifyType) *wechatSender {
	return &wechatSender{resolveDevice: resolveDevice, enqueuer: enqueuer, typ: typ}
}
func (s *wechatSender) Type() NotifyType { return s.typ }

func (s *wechatSender) Send(ctx context.Context, config json.RawMessage, _ string, content *NotifyContent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if content == nil {
		return fmt.Errorf("notification content is required")
	}
	if s.resolveDevice == nil || s.enqueuer == nil {
		return ErrNotConfigured
	}
	var cfg wechatChannelConfig
	if err := json.Unmarshal(config, &cfg); err != nil {
		return fmt.Errorf("parse wechat config: %w", err)
	}
	var receiver string
	switch s.typ {
	case NotifyTypeWeChatContact:
		receiver = cfg.ContactID
	case NotifyTypeWeChatRoom:
		receiver = cfg.RoomID
	default:
		return fmt.Errorf("unsupported wechat notify type: %d", s.typ)
	}
	if strings.TrimSpace(cfg.DeviceID) == "" || strings.TrimSpace(receiver) == "" {
		return fmt.Errorf("invalid wechat channel config: device_id or target is empty")
	}
	deviceVx, err := s.resolveDevice(ctx, cfg.DeviceID)
	if err != nil {
		return fmt.Errorf("resolve notification device: %w", err)
	}
	if strings.TrimSpace(deviceVx) == "" {
		return fmt.Errorf("notification device identity is empty")
	}
	text := content.Content
	if content.Title != "" {
		text = content.Title + "\n" + text
	}
	if err := s.enqueuer.EnqueueVxActionText(ctx, cfg.DeviceID, deviceVx, receiver, text); err != nil {
		return fmt.Errorf("enqueue wechat action: %w", err)
	}
	return nil
}
