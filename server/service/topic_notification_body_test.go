package service

import (
	"encoding/json"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestNotificationRequestBodyUsesProviderSDKContract(t *testing.T) {
	for _, tc := range []struct{ provider, msgType string }{
		{model.TopicNotificationProviderFeishu, "interactive"},
		{model.TopicNotificationProviderWeCom, "markdown"},
		{model.TopicNotificationProviderDingTalk, "markdown"},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			body, err := notificationRequestBody(tc.provider, "hello")
			if err != nil {
				t.Fatal(err)
			}
			var envelope map[string]any
			if err := json.Unmarshal(body, &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope["msg_type"] != tc.msgType && envelope["msgtype"] != tc.msgType {
				t.Fatalf("invalid message envelope: %s", body)
			}
			if tc.provider == model.TopicNotificationProviderFeishu {
				card, ok := envelope["card"].(map[string]any)
				if !ok {
					t.Fatalf("invalid Feishu card: %s", body)
				}
				elements, ok := card["elements"].([]any)
				if !ok || len(elements) != 1 {
					t.Fatalf("invalid Feishu elements: %s", body)
				}
				el := elements[0].(map[string]any)
				text, ok := el["text"].(map[string]any)
				if el["tag"] != "div" || !ok || text["tag"] != "lark_md" || text["content"] != "hello" {
					t.Fatalf("invalid Feishu markdown element: %s", body)
				}
			}
		})
	}
}
