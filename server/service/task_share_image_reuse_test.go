package service

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestTaskShareReusedArticleImage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(current, previous *model.TaskFile)
		wantOK bool
	}{
		{"retained identical image", func(_, _ *model.TaskFile) {}, true},
		{"superseded identical image", func(_, p *model.TaskFile) { p.State = model.TaskFileStateSuperseded }, true},
		{"different task", func(_, p *model.TaskFile) { p.TaskID = "another-task" }, false},
		{"missing execution", func(_, p *model.TaskFile) { p.ExecutionID = "" }, false},
		{"different platform URL", func(_, p *model.TaskFile) { p.PlatformImageURL += "-other" }, false},
		{"different path", func(_, p *model.TaskFile) { p.FilePath = "output/another.png" }, false},
		{"different content", func(_, p *model.TaskFile) { p.ContentHash = strings.Repeat("a", 64) }, false},
		{"missing current hash", func(c, _ *model.TaskFile) { c.ContentHash = "" }, false},
		{"missing previous hash", func(_, p *model.TaskFile) { p.ContentHash = "" }, false},
		{"invalid hashes", func(c, p *model.TaskFile) { c.ContentHash, p.ContentHash = "invalid", "invalid" }, false},
		{"different size", func(_, p *model.TaskFile) { p.FileSize++ }, false},
		{"different MIME", func(_, p *model.TaskFile) { p.MimeType = "image/jpeg" }, false},
		{"pending image", func(_, p *model.TaskFile) { p.State = model.TaskFileStatePending }, false},
		{"image no longer delivered", func(c, _ *model.TaskFile) { c.State = model.TaskFileStateRetained }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, store, task := shareFixture(t, model.ChannelArticle)
			const source = "https://creator.example/api/v1/article-assets/reused-image"
			files := shareDelivery(t, repo, store, task, "current", `<p>Article</p><img src="`+source+`">`)
			current := files[3] // output/image_2.png
			info, err := store.StatObject(t.Context(), current.OSSKey)
			if err != nil {
				t.Fatal(err)
			}
			current.PlatformImageURL, current.ContentHash = "", info.SHA256
			previous := *current
			previous.ID, previous.ExecutionID, previous.State = "previous-image", "previous", model.TaskFileStateRetained
			previous.PlatformImageURL = source
			previous.OSSKey = "old-object-must-not-be-read"
			tc.mutate(current, &previous)
			if err := repo.TaskFiles().DeleteByTaskID(t.Context(), task.ID); err != nil {
				t.Fatal(err)
			}
			for _, file := range append(files, &previous) {
				if err := repo.TaskFiles().Create(t.Context(), file); err != nil {
					t.Fatal(err)
				}
			}

			status, err := svc.Put(t.Context(), "owner", task.ID, 7)
			if !tc.wantOK {
				if !errors.Is(err, ErrTaskShareUnavailable) {
					t.Fatalf("unmatched image created a share: status=%#v, err=%v", status, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("share reused delivered image: %v", err)
			}
			token := shareToken(status)
			content, err := svc.Public(t.Context(), token)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(content.Body, `/images/current-image_2.png`) || strings.Contains(content.Body, source) {
				t.Fatalf("body did not use the current delivered image: %s", content.Body)
			}
			stream, err := svc.Image(t.Context(), token, current.ID)
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(stream.Body)
			_ = stream.Body.Close()
			if err != nil || !bytes.HasPrefix(data, []byte("\x89PNG")) || int64(len(data)) != current.FileSize {
				t.Fatalf("current image bytes unavailable: len=%d, err=%v", len(data), err)
			}
			if _, err := svc.Image(t.Context(), token, previous.ID); !errors.Is(err, ErrTaskShareNotFound) {
				t.Fatalf("previous image became publicly readable: %v", err)
			}
		})
	}
}
