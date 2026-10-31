package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
)

// NotifyContent 统一通知内容
type NotifyContent struct {
	Title   string // 标题
	Content string // 正文内容（纯文本或 markdown）
}

type NotifyType int

const (
	NotifyTypeNone          NotifyType = 0
	NotifyTypeWeWork        NotifyType = 1 // 企微机器人
	NotifyTypeWeChatContact NotifyType = 2 // 微信-联系人（通过 GeWeChat 设备转发给联系人）
	NotifyTypeWeChatRoom    NotifyType = 3 // 微信-群（通过 GeWeChat 设备转发给群）
	NotifyTypeLark          NotifyType = 4 // 飞书机器人
	NotifyTypeDingTalk      NotifyType = 5 // 钉钉机器人
)

// Sender 通知渠道发送接口
type Sender interface {
	Send(ctx context.Context, config json.RawMessage, recipient string, content *NotifyContent) error
	Type() NotifyType
}

// SenderRegistry 渠道发送器注册表
type SenderRegistry struct {
	mu      sync.RWMutex
	senders map[NotifyType]Sender
}

func NewSenderRegistry() *SenderRegistry {
	return &SenderRegistry{
		senders: make(map[NotifyType]Sender),
	}
}

func (r *SenderRegistry) Register(sender Sender) {
	if sender == nil {
		return
	}
	value := reflect.ValueOf(sender)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		if value.IsNil() {
			return
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.senders == nil {
		r.senders = make(map[NotifyType]Sender)
	}
	r.senders[sender.Type()] = sender
}

func (r *SenderRegistry) Get(t NotifyType) (Sender, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sender, ok := r.senders[t]
	if !ok {
		return nil, fmt.Errorf("unsupported notify type: %d", t)
	}
	return sender, nil
}
