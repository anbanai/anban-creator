package handler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
)

type fileStreamStorage struct {
	fakeStorageProvider
	opened, buffered, closed bool
}

func (s *fileStreamStorage) Read(context.Context, string) ([]byte, error) {
	s.buffered = true
	return nil, fmt.Errorf("unbounded object reads are forbidden")
}

func (s *fileStreamStorage) OpenObject(context.Context, string) (io.ReadCloser, error) {
	s.opened = true
	return &fileStreamBody{Reader: bytes.NewBufferString("project-archive"), close: func() { s.closed = true }}, nil
}

type fileStreamBody struct {
	io.Reader
	close func()
}

func (b *fileStreamBody) Close() error { b.close(); return nil }

func TestServeFileUsesAndClosesStorageStream(t *testing.T) {
	store := &fileStreamStorage{}
	logger := zerolog.New(io.Discard)
	handler := NewFileHandler(store, &logger)
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error { c.Locals("user_id", "user-1"); return c.Next() })
	app.Get("/files/*", handler.ServeFile)
	resp, err := app.Test(httptest.NewRequest("GET", "/files/uploads/references/user-1/project.zip", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK || string(body) != "project-archive" || !store.opened || store.buffered || !store.closed {
		t.Fatalf("status=%d body=%q opened=%t buffered=%t closed=%t", resp.StatusCode, body, store.opened, store.buffered, store.closed)
	}
}
