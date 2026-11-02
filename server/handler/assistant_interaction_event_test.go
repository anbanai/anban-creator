package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"net"
	"net/http"
	"testing"
	"time"

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

func TestPreparedInteractionSSEConnectsBeforeRuntimeStarts(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AssistantInteraction{}, &model.AssistantInteractionRuntime{}, &model.AssistantInteractionDraft{}, &model.AssistantInteractionEvent{}, &model.AssistantInteractionAttempt{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	factory := &assistantHandlerRuntimeFactory{}
	svc := service.NewAssistantInteractionService(repository.New(db), factory, nil)
	prepared, err := svc.CreateOrResume(context.Background(), "user-1", service.AssistantInteractionInput{ModeID: service.AssistantModeCreation, PrepareOnly: true}, "prepare")
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	app.Get("/events/:id", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return (&AssistantHandler{interactionSvc: svc}).InteractionEvents(c)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serverDone := make(chan error, 1)
	go func() { serverDone <- app.Listener(listener) }()
	t.Cleanup(func() {
		// Closing the test DB ends the polling stream before server shutdown.
		_ = sqlDB.Close()
		_ = app.ShutdownWithTimeout(2 * time.Second)
		<-serverDone
	})
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get("http://" + listener.Addr().String() + "/events/" + prepared.ID)
	if err != nil {
		t.Fatalf("SSE headers were not flushed before first message: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("SSE response = %d %q", response.StatusCode, response.Header.Get("Content-Type"))
	}
	comment, err := bufio.NewReader(response.Body).ReadString('\n')
	if err != nil || comment != ": connected\n" {
		t.Fatalf("initial SSE comment = %q, %v", comment, err)
	}
	if factory.starts.Load() != 0 {
		t.Fatal("SSE initialized a runtime")
	}
}
