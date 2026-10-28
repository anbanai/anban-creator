package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func shareFixture(t *testing.T, channel string) (*TaskShareService, repository.Repository, *storage.LocalProvider, *model.Task) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.TaskExecution{}, &model.TaskFile{}, &model.TaskShare{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	repo := repository.New(db)
	store, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: "task", UserID: "owner", ProjectID: "project", AgentID: channel, Channel: channel, TaskKind: model.TaskKindContentGeneration, Status: model.TaskStatusCompleted, Title: "Public title", Prompt: "private prompt"}
	if err := repo.Tasks().Create(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	svc := NewTaskShareService(repo, store)
	svc.now = func() time.Time { return time.Date(2026, 10, 28, 1, 2, 3, 0, time.UTC) }
	return svc, repo, store, task
}

func shareDelivery(t *testing.T, repo repository.Repository, store storage.Provider, task *model.Task, executionID, body string) []*model.TaskFile {
	t.Helper()
	contract := `[{"role":"final_markdown","path":"output/content.md","mime_type":"text/markdown"},{"role":"cover","path":"output/cover.png","mime_type":"image/png"},{"role":"image","path":"output/image_*.png","mime_type":"image/png"},{"role":"tail","path":"output/tail.png","mime_type":"image/png"},{"role":"image","path":"output/private.png","mime_type":"image/png","internal_only":true}]`
	bodyPath, bodyMIME := "output/content.md", "text/markdown"
	if task.Channel == model.ChannelArticle {
		bodyPath, bodyMIME = "output/05-article.html", "text/html"
		contract = strings.Replace(contract, `"role":"final_markdown","path":"output/content.md","mime_type":"text/markdown"`, `"role":"html","path":"output/05-article.html","mime_type":"text/html"`, 1)
	}
	execution := &model.TaskExecution{ID: executionID, TaskID: task.ID, Attempt: len(executionID), Status: model.TaskExecutionSucceeded, ManifestStatus: model.TaskExecutionManifestDelivered, AgentPackID: task.AgentID, AgentPackVersion: "1.0.0", AgentPackDigest: "frozen-digest", AgentPackDeliveryContract: datatypes.JSON(contract)}
	if err := repo.TaskExecutions().Create(t.Context(), execution); err != nil {
		t.Fatal(err)
	}
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	files := []*model.TaskFile{{ID: executionID + "-body", FilePath: bodyPath, MimeType: bodyMIME}}
	for _, name := range []string{"cover.png", "image_10.png", "image_2.png", "tail.png", "private.png"} {
		files = append(files, &model.TaskFile{ID: executionID + "-" + name, FilePath: "output/" + name, MimeType: "image/png"})
	}
	for _, file := range files {
		file.TaskID, file.ExecutionID, file.State = task.ID, executionID, model.TaskFileStateDelivered
		file.FileName = strings.TrimPrefix(file.FilePath, "output/")
		file.Role = model.FileRoleImage
		data := pngData.Bytes()
		if file == files[0] {
			data = []byte(body)
			file.Role = model.FileRoleMarkdown
		}
		file.OSSKey = buildTaskMCPArtifactStoragePrefix(task, executionID) + file.FilePath
		file.StorageProvider, file.FileSize = store.Name(), int64(len(data))
		file.PlatformImageURL = "https://platform.example/" + file.ID
		if _, err := store.Upload(t.Context(), file.OSSKey, bytes.NewReader(data), file.MimeType); err != nil {
			t.Fatal(err)
		}
		if err := repo.TaskFiles().Create(t.Context(), file); err != nil {
			t.Fatal(err)
		}
	}
	return files
}

func shareToken(status *TaskShareStatus) string {
	return strings.TrimPrefix(status.SharePath, "/share/")
}

func TestTaskShareLifecycle(t *testing.T) {
	svc, repo, store, task := shareFixture(t, model.ChannelSeednote)
	ctx := t.Context()
	if got, err := svc.Get(ctx, "owner", task.ID); err != nil || got.Status != "none" {
		t.Fatalf("initial = %#v, %v", got, err)
	}
	for _, days := range []int{0, -1, 366} {
		if _, err := svc.Put(ctx, "owner", task.ID, days); !errors.Is(err, ErrTaskShareInvalidExpiry) {
			t.Fatalf("days %d = %v", days, err)
		}
	}
	for _, method := range []func() error{
		func() error { _, err := svc.Get(ctx, "foreign", task.ID); return err },
		func() error { _, err := svc.Put(ctx, "foreign", task.ID, 7); return err },
		func() error { return svc.Revoke(ctx, "foreign", task.ID) },
	} {
		if err := method(); !errors.Is(err, ErrTaskShareNotFound) {
			t.Fatalf("ownership = %v", err)
		}
	}
	shareDelivery(t, repo, store, task, "one", "# Delivered body")
	first, err := svc.Put(ctx, "owner", task.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != "active" || len(shareToken(first)) < 43 || !first.ExpiresAt.Equal(svc.now().Add(24*time.Hour)) {
		t.Fatalf("created = %#v", first)
	}
	updated, err := svc.Put(ctx, "owner", task.ID, 365)
	if err != nil || updated.SharePath != first.SharePath {
		t.Fatalf("updated = %#v, %v", updated, err)
	}
	now := updated.ExpiresAt
	svc.now = func() time.Time { return *now }
	if got, err := svc.Get(ctx, "owner", task.ID); err != nil || got.Status != "expired" {
		t.Fatalf("expiry edge = %#v, %v", got, err)
	}
	if _, err := svc.Public(ctx, shareToken(first)); !errors.Is(err, ErrTaskShareGone) {
		t.Fatalf("expired public = %v", err)
	}
	rotated, err := svc.Put(ctx, "owner", task.ID, 7)
	if err != nil || rotated.SharePath == first.SharePath {
		t.Fatalf("rotation = %#v, %v", rotated, err)
	}
	if _, err := svc.Public(ctx, shareToken(first)); !errors.Is(err, ErrTaskShareNotFound) {
		t.Fatalf("old token = %v", err)
	}
	for range 2 {
		if err := svc.Revoke(ctx, "owner", task.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Public(ctx, shareToken(rotated)); !errors.Is(err, ErrTaskShareGone) {
		t.Fatalf("revoked = %v", err)
	}
	again, err := svc.Put(ctx, "owner", task.ID, 7)
	if err != nil || again.SharePath == rotated.SharePath {
		t.Fatalf("reenable = %#v, %v", again, err)
	}
	if _, err := repo.Tasks().BeginDelete(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Public(ctx, shareToken(again)); !errors.Is(err, ErrTaskShareNotFound) {
		t.Fatalf("deleting = %v", err)
	}
}

func TestTaskSharePublicDeliveryAndVersionSwitch(t *testing.T) {
	svc, repo, store, task := shareFixture(t, model.ChannelSeednote)
	files := shareDelivery(t, repo, store, task, "one", "# Full body\n\n![inline](image_2.png)")
	status, err := svc.Put(t.Context(), "owner", task.ID, 7)
	if err != nil {
		t.Fatal(err)
	}
	token := shareToken(status)
	got, err := svc.Public(t.Context(), token)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != model.ChannelSeednote || got.Format != "markdown" || got.Title != task.Title || len(got.Images) != 4 || got.Images[1].ID != "one-image_2.png" || got.Images[2].ID != "one-image_10.png" || !strings.Contains(got.Body, got.Images[1].URL) {
		t.Fatalf("public = %#v", got)
	}
	data, _ := json.Marshal(got)
	for _, private := range []string{"private prompt", "oss_key", "platform.example", "private.png", "execution_id", "user_id"} {
		if bytes.Contains(data, []byte(private)) {
			t.Fatalf("leaked %s: %s", private, data)
		}
	}
	stream, err := svc.Image(t.Context(), token, files[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	imageBytes, _ := io.ReadAll(stream.Body)
	_ = stream.Body.Close()
	if stream.MIMEType != "image/png" || !bytes.HasPrefix(imageBytes, []byte("\x89PNG")) {
		t.Fatalf("image = %#v %q", stream, imageBytes)
	}
	for _, id := range []string{"foreign", files[0].ID, files[5].ID} {
		if _, err := svc.Image(t.Context(), token, id); !errors.Is(err, ErrTaskShareNotFound) {
			t.Fatalf("image %s = %v", id, err)
		}
	}
	if err := repo.TaskExecutions().Create(t.Context(), &model.TaskExecution{ID: "failed", TaskID: task.ID, Attempt: 99, Status: model.TaskExecutionFailed, ManifestStatus: model.TaskExecutionManifestRetained}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskFiles().Create(t.Context(), &model.TaskFile{ID: "retained", TaskID: task.ID, ExecutionID: "failed", FilePath: "output/content.md", FileName: "content.md", Role: model.FileRoleMarkdown, State: model.TaskFileStateRetained}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Public(t.Context(), token); err != nil {
		t.Fatalf("failed continuation hid delivery: %v", err)
	}
	if _, err := svc.Image(t.Context(), token, "retained"); !errors.Is(err, ErrTaskShareNotFound) {
		t.Fatalf("retained = %v", err)
	}
	if err := repo.WithTx(t.Context(), func(tx repository.Repository) error { return tx.TaskFiles().DeleteByTaskID(t.Context(), task.ID) }); err != nil {
		t.Fatal(err)
	}
	shareDelivery(t, repo, store, task, "second", "# Latest complete body")
	latest, err := svc.Public(t.Context(), token)
	if err != nil || !strings.Contains(latest.Body, "Latest complete") {
		t.Fatalf("latest = %#v, %v", latest, err)
	}
	if _, err := svc.Image(t.Context(), token, files[1].ID); !errors.Is(err, ErrTaskShareNotFound) {
		t.Fatalf("old image = %v", err)
	}
	if err := store.Delete(t.Context(), buildTaskMCPArtifactStoragePrefix(task, "second")+"output/image_2.png"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Public(t.Context(), token); !errors.Is(err, ErrTaskShareUnavailable) {
		t.Fatalf("missing image = %v", err)
	}
}

func TestTaskShareHTMLRewritesKnownImagesAndRejectsUnknownImages(t *testing.T) {
	svc, repo, store, task := shareFixture(t, model.ChannelArticle)
	body := `<p>Article</p><img src="image_2.png"><img src="https://platform.example/one-cover.png">`
	shareDelivery(t, repo, store, task, "one", body)
	status, _ := svc.Put(t.Context(), "owner", task.ID, 7)
	got, err := svc.Public(t.Context(), shareToken(status))
	if err != nil || got.Format != "html" || strings.Contains(got.Body, "platform.example") || strings.Count(got.Body, "/images/") != 2 {
		t.Fatalf("html = %#v, %v", got, err)
	}
	t.Run("unknown image", func(t *testing.T) {
		svc, repo, store, task := shareFixture(t, model.ChannelArticle)
		shareDelivery(t, repo, store, task, "one", `<img src="https://unknown.invalid/private.png">`)
		if _, err := svc.Put(t.Context(), "owner", task.ID, 7); !errors.Is(err, ErrTaskShareUnavailable) {
			t.Fatalf("unknown image = %v", err)
		}
	})
}

func TestTaskShareConcurrentUpdatesKeepOneToken(t *testing.T) {
	svc, repo, store, task := shareFixture(t, model.ChannelSeednote)
	shareDelivery(t, repo, store, task, "one", "# Delivered body")
	var wg sync.WaitGroup
	results := make(chan *TaskShareStatus, 8)
	errors := make(chan error, 8)
	for day := 1; day <= 8; day++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, err := svc.Put(t.Context(), "owner", task.ID, day)
			results <- status
			errors <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	path := ""
	for result := range results {
		if path == "" {
			path = result.SharePath
		}
		if result.SharePath != path {
			t.Fatalf("concurrent token changed: %s != %s", result.SharePath, path)
		}
	}
}

func TestTaskShareMarkdownReferenceRewritePreservesProseAndCode(t *testing.T) {
	svc, repo, store, task := shareFixture(t, model.ChannelSeednote)
	body := "image_2.png is prose. `![code](image_2.png)`\n\n![ref][photo]\n\n[photo]: <image_2.png> \"Caption\"\n\n<img src=\"https://private.invalid/raw.png\">\n"
	shareDelivery(t, repo, store, task, "one", body)
	status, _ := svc.Put(t.Context(), "owner", task.ID, 7)
	got, err := svc.Public(t.Context(), shareToken(status))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Body, "image_2.png is prose. `![code](image_2.png)`") || !strings.Contains(got.Body, "[photo]: <"+got.Images[1].URL+">") || strings.Contains(got.Body, "private.invalid") {
		t.Fatalf("markdown = %s", got.Body)
	}
}

func TestTaskShareUnsupportedAndIncompleteDelivery(t *testing.T) {
	for _, mutation := range []struct {
		name   string
		mutate func(*model.Task)
	}{
		{"analysis", func(task *model.Task) { task.TaskKind = model.TaskKindViralAnalysis }},
		{"different agent", func(task *model.Task) { task.AgentID = model.AgentIDWechatPicture }},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			svc, repo, _, task := shareFixture(t, model.ChannelSeednote)
			mutation.mutate(task)
			if err := repo.Tasks().Update(t.Context(), task); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.Put(t.Context(), "owner", task.ID, 7); !errors.Is(err, ErrTaskShareNotFound) {
				t.Fatalf("unsupported task = %v", err)
			}
		})
	}
	t.Run("pending manifest", func(t *testing.T) {
		svc, repo, store, task := shareFixture(t, model.ChannelSeednote)
		files := shareDelivery(t, repo, store, task, "one", "# body")
		status, _ := svc.Put(t.Context(), "owner", task.ID, 7)
		if _, err := repo.TaskExecutions().Transition(t.Context(), "one", []string{model.TaskExecutionSucceeded}, model.TaskExecutionSucceeded, model.ExecutionTransition{ManifestStatus: model.TaskExecutionManifestPending}); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Public(t.Context(), shareToken(status)); !errors.Is(err, ErrTaskShareUnavailable) {
			t.Fatalf("pending manifest = %v", err)
		}
		if _, err := svc.Image(t.Context(), shareToken(status), files[1].ID); !errors.Is(err, ErrTaskShareUnavailable) {
			t.Fatalf("pending image = %v", err)
		}
	})
}

type shareOSSStorage struct {
	*storage.LocalProvider
	reads, streams int
	statError      error
}

func (*shareOSSStorage) Name() string { return "oss" }
func (*shareOSSStorage) Read(context.Context, string) ([]byte, error) {
	panic("share must never use unbounded reads")
}
func (s *shareOSSStorage) ReadObject(ctx context.Context, key string, limit int64) ([]byte, error) {
	s.reads++
	return s.LocalProvider.ReadObject(ctx, key, limit)
}
func (s *shareOSSStorage) OpenObject(ctx context.Context, key string) (io.ReadCloser, error) {
	s.streams++
	return s.LocalProvider.OpenObject(ctx, key)
}
func (s *shareOSSStorage) StatObject(ctx context.Context, key string) (*storage.ObjectInfo, error) {
	if s.statError != nil {
		return nil, s.statError
	}
	return s.LocalProvider.StatObject(ctx, key)
}

func TestTaskShareOSSStorageUsesBoundedBodyAndStreamedImages(t *testing.T) {
	svc, repo, local, task := shareFixture(t, model.ChannelSeednote)
	store := &shareOSSStorage{LocalProvider: local}
	svc.store = store
	files := shareDelivery(t, repo, store, task, "one", "# body")
	status, _ := svc.Put(t.Context(), "owner", task.ID, 7)
	store.reads, store.streams = 0, 0
	if _, err := svc.Public(t.Context(), shareToken(status)); err != nil {
		t.Fatal(err)
	}
	if store.reads != 1 || store.streams != 4 {
		t.Fatalf("reads=%d streams=%d", store.reads, store.streams)
	}
	stream, err := svc.Image(t.Context(), shareToken(status), files[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Body.Close()
	if store.reads != 1 || store.streams != 5 {
		t.Fatalf("image buffered: reads=%d streams=%d", store.reads, store.streams)
	}
	store.statError = errors.New("storage network failure")
	if _, err := svc.Public(t.Context(), shareToken(status)); !errors.Is(err, store.statError) || errors.Is(err, ErrTaskShareUnavailable) || errors.Is(err, ErrTaskShareGone) {
		t.Fatalf("infrastructure failure classified incorrectly: %v", err)
	}
}

func TestTaskShareHTMLRemovesActiveContentAndFetchAttributes(t *testing.T) {
	svc, repo, store, task := shareFixture(t, model.ChannelArticle)
	shareDelivery(t, repo, store, task, "one", `<script>alert(1)</script><iframe src="https://external.invalid"></iframe><p onclick="alert(1)" style="background:url(https://external.invalid/x)">Article</p><img src="image_2.png" srcset="https://external.invalid/x 2x" onerror="alert(1)">`)
	status, _ := svc.Put(t.Context(), "owner", task.ID, 7)
	got, err := svc.Public(t.Context(), shareToken(status))
	if err != nil {
		t.Fatal(err)
	}
	for _, unsafe := range []string{"script", "iframe", "onclick", "onerror", "srcset", "external.invalid"} {
		if strings.Contains(got.Body, unsafe) {
			t.Fatalf("unsafe %s: %s", unsafe, got.Body)
		}
	}
}

func TestTaskSharePutRequiresReadableDeliveredContent(t *testing.T) {
	svc, repo, store, task := shareFixture(t, model.ChannelSeednote)
	if _, err := svc.Put(t.Context(), "owner", task.ID, 7); !errors.Is(err, ErrTaskShareUnavailable) {
		t.Fatalf("no delivered files = %v", err)
	}
	if got, err := svc.Get(t.Context(), "owner", task.ID); err != nil || got.Status != "none" {
		t.Fatalf("unavailable task acquired a share: %#v, %v", got, err)
	}
	files := shareDelivery(t, repo, store, task, "one", "# Delivered body")
	if err := store.Delete(t.Context(), files[0].OSSKey); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Put(t.Context(), "owner", task.ID, 7); !errors.Is(err, ErrTaskShareUnavailable) {
		t.Fatalf("missing main = %v", err)
	}
}
