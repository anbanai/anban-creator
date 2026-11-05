package notification

import (
	"encoding/json"
	"fmt"

	"github.com/anbanai/anban-creator/server/sdk/dingtalkbot"
	"github.com/anbanai/anban-creator/server/sdk/feishubot"
	wework "github.com/anbanai/anban-creator/server/sdk/wework/robot"
)

type markdownBuilder struct{ content string }

func newMarkdownBuilder() *markdownBuilder { return &markdownBuilder{} }
func (b *markdownBuilder) h2(text string) *markdownBuilder {
	b.content += fmt.Sprintf("## %s\n", text)
	return b
}
func (b *markdownBuilder) text(text string) *markdownBuilder { b.content += text + "\n"; return b }
func (b *markdownBuilder) string() string                    { return b.content }

// MarkdownBody builds the provider-specific webhook envelope without sending
// it. The SDK message builders are the single source of truth for payload
// shape; caller-owned durable transport decides when and how to deliver it.
func MarkdownBody(typ NotifyType, title, text string) ([]byte, error) {
	var payload any
	switch typ {
	case NotifyTypeLark:
		payload = feishubot.NewInteractiveMessage().SetHeader(title, "blue").AddText(text).ToMap()
	case NotifyTypeDingTalk:
		payload = dingtalkbot.NewMarkdownMessage(title, text)
	case NotifyTypeWeWork:
		builder := newMarkdownBuilder()
		if title != "" {
			builder.h2(title)
		}
		payload = wework.NewMarkdownMessage(builder.text(text).string())
	default:
		return nil, fmt.Errorf("unsupported notification type: %d", typ)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal notification payload: %w", err)
	}
	return body, nil
}
