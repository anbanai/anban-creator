package service

import (
	"context"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/app/draft"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
)

// fakeDraftClient records whether CreateDraft was invoked so the pre-publish
// image-diversity gate can be asserted to short-circuit before the WeChat API.
type fakeDraftClient struct {
	createCalled bool
	createErr    error
}

func (f *fakeDraftClient) CreateDraft([]draft.Article) (*draft.DraftResult, error) {
	f.createCalled = true
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &draft.DraftResult{MediaID: "media-123", DraftURL: "https://draft.example/123"}, nil
}

func (f *fakeDraftClient) ListDrafts(int64, int64) (*draft.ListDraftsResult, error) {
	return nil, nil
}

func (f *fakeDraftClient) ListPublished(int64, int64) (*draft.ListPublishedResult, error) {
	return nil, nil
}

func setupPublishingTest(t *testing.T) (*PublishingService, repository.Repository) {
	t.Helper()
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	svc := NewPublishingService(repo, &logger)
	return svc, repo
}

func TestExtractImageSrcs(t *testing.T) {
	cases := []struct {
		name string
		html string
		want []string
	}{
		{"empty", ``, nil},
		{"no img", `<p>text</p>`, nil},
		{"single double-quote", `<img src="http://a/1.png">`, []string{"http://a/1.png"}},
		{"single single-quote", `<img src='http://a/2.png'/>`, []string{"http://a/2.png"}},
		{"case insensitive", `<IMG SRC="http://a/3.png">`, []string{"http://a/3.png"}},
		{"src not first attr", `<img class="x" style="width:100%" src="http://a/4.png">`, []string{"http://a/4.png"}},
		{"two distinct", `<img src="http://a/1.png"><img src="http://a/2.png">`, []string{"http://a/1.png", "http://a/2.png"}},
		{"two same keeps dupes", `<img src="http://a/1.png"><img src="http://a/1.png">`, []string{"http://a/1.png", "http://a/1.png"}},
		// Edge cases the regex must handle for machine-generated WeChat HTML.
		// These lock the contract so a future "simplification" of \bsrc cannot
		// silently regress the gate.
		{"srcset without src yields none", `<img srcset="http://a/1.png 2x">`, nil},
		{"src and srcset picks src", `<img src="http://a/1.png" srcset="http://a/2.png 2x">`, []string{"http://a/1.png"}},
		{"data url captured", `<img src="data:image/png;base64,iVBORw0KGgo=">`, []string{"data:image/png;base64,iVBORw0KGgo="}},
		{"multiline tags", "<img\n  src=\"http://a/1.png\">\n<img src=\"http://a/2.png\">", []string{"http://a/1.png", "http://a/2.png"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractImageSrcs(tc.html)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d srcs %v, want %d %v", len(got), got, len(tc.want), tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("src[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestValidateContentImageDiversity(t *testing.T) {
	cases := []struct {
		name    string
		html    string
		wantErr bool
	}{
		{"no image", `<p>text only</p>`, false},
		{"single image", `<img src="http://a/1.png">`, false},
		{"two same url rejected", `<img src="http://a/1.png"><img src="http://a/1.png">`, true},
		{"three same url rejected", `<img src="http://a/1.png"><img src="http://a/1.png"><img src="http://a/1.png">`, true},
		{"two distinct allowed", `<img src="http://a/1.png"><img src="http://a/2.png">`, false},
		{"three two-distinct allowed", `<img src="http://a/1.png"><img src="http://a/2.png"><img src="http://a/1.png">`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateContentImageDiversity(tc.html)
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

func TestPublishDraft_RejectsDuplicateContentImages(t *testing.T) {
	svc, repo := setupPublishingTest(t)
	ctx := context.Background()
	userID := uuid.New().String()
	channelID := createTestWechatChannel(t, repo, userID)

	fake := &fakeDraftClient{}
	svc.createDraftServiceFn = func(*model.Channel) (draftClient, error) { return fake, nil }

	_, err := svc.PublishDraft(ctx, userID, channelID, []DraftArticleInput{
		{Title: "dup", Content: `<p>intro</p><img src="https://cdn/same.png"/><p>mid</p><img src="https://cdn/same.png"/>`},
	})
	if err == nil {
		t.Fatal("expected publish to be rejected for duplicate content images")
	}
	if fake.createCalled {
		t.Fatal("CreateDraft must not be called when content images are duplicates")
	}
}

func TestPublishDraft_AllowsDistinctContentImages(t *testing.T) {
	svc, repo := setupPublishingTest(t)
	ctx := context.Background()
	userID := uuid.New().String()
	channelID := createTestWechatChannel(t, repo, userID)

	fake := &fakeDraftClient{}
	svc.createDraftServiceFn = func(*model.Channel) (draftClient, error) { return fake, nil }

	res, err := svc.PublishDraft(ctx, userID, channelID, []DraftArticleInput{
		{Title: "ok", Content: `<img src="https://cdn/a.png"/><img src="https://cdn/b.png"/>`},
	})
	if err != nil {
		t.Fatalf("expected publish to succeed, got %v", err)
	}
	if !fake.createCalled {
		t.Fatal("CreateDraft should be called for distinct images")
	}
	if res.MediaID != "media-123" {
		t.Fatalf("media_id = %q", res.MediaID)
	}
}

func TestPublishDraft_AllowsNoAndSingleImage(t *testing.T) {
	svc, repo := setupPublishingTest(t)
	ctx := context.Background()
	userID := uuid.New().String()
	channelID := createTestWechatChannel(t, repo, userID)

	fake := &fakeDraftClient{}
	svc.createDraftServiceFn = func(*model.Channel) (draftClient, error) { return fake, nil }

	for _, content := range []string{
		`<p>text only, no images</p>`,
		`<p>one</p><img src="https://cdn/only.png"/>`,
	} {
		fake.createCalled = false
		if _, err := svc.PublishDraft(ctx, userID, channelID, []DraftArticleInput{
			{Title: "t", Content: content},
		}); err != nil {
			t.Fatalf("expected publish to succeed for content %q, got %v", content, err)
		}
		if !fake.createCalled {
			t.Fatalf("CreateDraft should be called for content %q", content)
		}
	}
}
