package main

import (
	"testing"

	"github.com/anbanai/anban-creator/server/storage"
)

func TestUploadSessionCleanupSupportsLocalProvider(t *testing.T) {
	local, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := uploadSessionCleanupStorage(local); !ok {
		t.Fatal("local stream artifacts require the upload-session cleanup worker")
	}
}
