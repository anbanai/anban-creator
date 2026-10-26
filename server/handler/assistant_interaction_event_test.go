package handler

import (
	"encoding/json"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestInteractionEventDataIncludesGenerationWithoutDroppingLiveFields(t *testing.T) {
	data := interactionEventData(&model.AssistantInteractionEvent{
		Generation: 7,
		Data:       json.RawMessage(`{"text":"继续写","role":"assistant"}`),
	})
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["generation"] != float64(7) || fields["text"] != "继续写" || fields["role"] != "assistant" {
		t.Fatalf("event data = %#v, want generation and live fields", fields)
	}
}
