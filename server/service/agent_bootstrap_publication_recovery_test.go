package service

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"path"
	"strings"
	"testing"
	"time"

	serveragent "github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/auth"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestPublicationPackageRecoveryPreservesImagesAndRechecksReviews(t *testing.T) {
	ctx := t.Context()
	_, repo, db, task, source := setupCloudCompletionTestWithDB(t, false)
	source.Status = model.TaskExecutionSucceeded
	if err := db.Save(source).Error; err != nil {
		t.Fatal(err)
	}
	store := &fakeTaskStorage{files: map[string][]byte{}}
	paths := append(append([]string{}, recoveryBootstrapTestPaths...), "output/images.json", "output/cover-quality.json", "output/cover.png")
	for _, p := range paths {
		body := []byte("{}")
		mime := recoveryBootstrapTestMIMETypes[p]
		if mime == "" {
			mime = "application/json"
		}
		role := model.FileRoleOther
		if p == "output/cover.png" {
			body = tinyImagePNG(t)
			mime = "image/png"
			role = model.FileRoleCover
		}
		hash := fmt.Sprintf("%x", sha256.Sum256(body))
		key := buildTaskArtifactFinalStorageKey(task, source.ID, hash, p)
		store.files[key] = body
		file := &model.TaskFile{TaskID: task.ID, ExecutionID: source.ID, State: model.TaskFileStateDelivered, FilePath: p, FileName: path.Base(p), Role: role, MimeType: mime, ContentHash: hash, FileSize: int64(len(body)), OSSKey: key, StorageProvider: store.Name()}
		if mime == "image/png" {
			file.MediaID = "verified-cover"
			file.WechatURL = "https://mmbiz.qpic.cn/verified-cover"
		}
		if err := repo.TaskFiles().Create(ctx, file); err != nil {
			t.Fatal(err)
		}
	}
	recovery := *source
	recovery.ID = uuid.NewString()
	recovery.Attempt++
	recovery.ParentExecutionID = source.ID
	recovery.Purpose = model.TaskExecutionPurposePublicationRecovery
	recovery.Status = model.TaskExecutionStarting
	recovery.Started = false
	recovery.RuntimeScope = "docker"
	recovery.RuntimeWorkload = "recovery"
	recovery.RuntimeInstanceID = ""
	task.Outcome = &model.TaskOutcome{Publication: model.TaskPublicationOutcome{Action: "review_content", Code: "publication_package_invalid"}}
	tokens, err := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	svc := NewAgentBootstrapService(repo, tokens, AgentBootstrapConfig{Store: store}, zerolog.Nop())
	applyBootstrapTestProfile(t, svc, task, &recovery)
	if err := repo.TaskExecutions().Create(ctx, &recovery); err != nil {
		t.Fatal(err)
	}
	task.CurrentExecutionID = &recovery.ID
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.User{ID: task.UserID}).Error; err != nil {
		t.Fatal(err)
	}
	identity := &serveragent.WorkloadIdentity{Target: recovery.Target, RuntimeIdentity: model.RuntimeIdentity{Scope: "docker", Workload: "recovery", InstanceID: "recovery-instance"}, ExecutionID: recovery.ID, TaskID: task.ID, ProjectID: task.ProjectID, UserID: task.UserID, Deadline: time.Now().Add(time.Hour)}
	response, err := svc.Bootstrap(ctx, identity)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(response.Prompt, "必须重新执行内容质量、营销合规、最终审核和爆款审核") || strings.Contains(response.Prompt, "不得重新执行选题、正文创作、SEO 或语义审核") {
		t.Fatalf("unsafe repair prompt: %s", response.Prompt)
	}
	restored := map[string]BootstrapFile{}
	for _, file := range response.Files {
		restored[file.Path] = file
	}
	for _, p := range paths {
		if restored[p].DownloadURL == "" {
			t.Fatalf("missing restored file %s", p)
		}
	}
	images, err := repo.TaskFiles().FindByExecutionID(ctx, recovery.ID)
	if err != nil || len(images) != 1 {
		t.Fatalf("images=%#v err=%v", images, err)
	}
	cover := images[0]
	if cover.MediaID != "verified-cover" || cover.WechatURL == "" || cover.State != model.TaskFileStatePending || !bytes.Equal(store.files[cover.OSSKey], tinyImagePNG(t)) {
		t.Fatalf("lost verified cover: %#v", cover)
	}
	// A runtime re-bootstrap must not overwrite a subsequently updated image.
	updated := *cover
	updated.MediaID = "updated-cover"
	if _, err := repo.TaskFiles().UpsertPendingCurrentExecution(ctx, task.ID, recovery.ID, &updated); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Bootstrap(ctx, identity); err != nil {
		t.Fatal(err)
	}
	// The runtime manifest is untrusted; only persisted metadata for identical
	// bytes may survive finalization into the current execution.
	incoming := *cover
	incoming.ID = ""
	incoming.MediaID = "forged"
	incoming.WechatURL = "https://untrusted.invalid/"
	if err := repo.TaskFiles().ReplacePendingCurrentExecutionPreservingMCPArtifacts(ctx, task.ID, recovery.ID, []*model.TaskFile{&incoming}); err != nil {
		t.Fatal(err)
	}
	images, err = repo.TaskFiles().FindByExecutionID(ctx, recovery.ID)
	if err != nil || len(images) != 1 || images[0].MediaID != "updated-cover" || images[0].WechatURL != cover.WechatURL {
		t.Fatalf("manifest lost trusted metadata: %#v, %v", images, err)
	}
}

func TestPublicationRecoveryImagesRejectUntrustedSource(t *testing.T) {
	for _, kind := range []string{"hash mismatch", "foreign key", "non-output image"} {
		t.Run(kind, func(t *testing.T) {
			_, repo, db, task, source := setupCloudCompletionTestWithDB(t, false)
			if err := db.Model(source).Update("status", model.TaskExecutionSucceeded).Error; err != nil {
				t.Fatal(err)
			}
			body := tinyImagePNG(t)
			hash := fmt.Sprintf("%x", sha256.Sum256(body))
			p := "output/cover.png"
			if kind == "non-output image" {
				p = "input/cover.png"
			}
			key := buildTaskMCPArtifactStoragePrefix(task, source.ID) + p
			if kind == "foreign key" {
				key = "foreign/cover.png"
			}
			store := &fakeTaskStorage{files: map[string][]byte{key: body}}
			if kind == "hash mismatch" {
				hash = strings.Repeat("a", 64)
			}
			file := &model.TaskFile{TaskID: task.ID, ExecutionID: source.ID, State: model.TaskFileStateDelivered, FilePath: p, FileName: "cover.png", Role: model.FileRoleCover, MimeType: "image/png", ContentHash: hash, FileSize: int64(len(body)), OSSKey: key, StorageProvider: store.Name()}
			if err := repo.TaskFiles().Create(t.Context(), file); err != nil {
				t.Fatal(err)
			}
			svc := &AgentBootstrapService{repo: repo, cfg: AgentBootstrapConfig{Store: store}}
			_, _, err := svc.buildPublicationRecoveryImages(t.Context(), &model.TaskExecution{ID: uuid.NewString(), ParentExecutionID: source.ID, Purpose: model.TaskExecutionPurposePublicationRecovery}, task, time.Now().Add(time.Hour))
			if err == nil {
				t.Fatal("accepted untrusted recovery image")
			}
			if len(store.files) != 1 {
				t.Fatal("copied unverified image")
			}
		})
	}
}

func TestPublicationRecoveryImagesAcceptDeliverySizeLimit(t *testing.T) {
	_, repo, db, task, source := setupCloudCompletionTestWithDB(t, false)
	if err := db.Model(source).Update("status", model.TaskExecutionSucceeded).Error; err != nil {
		t.Fatal(err)
	}
	// PNG decoders permit trailing bytes; this exercises the actual byte limit
	// without allocating a large decoded raster.
	body := make([]byte, maxTaskDeliveryImageBytes)
	copy(body, tinyImagePNG(t))
	hash := fmt.Sprintf("%x", sha256.Sum256(body))
	p := "output/cover.png"
	key := buildTaskArtifactFinalStorageKey(task, source.ID, hash, p)
	store := &fakeTaskStorage{files: map[string][]byte{key: body}}
	file := &model.TaskFile{TaskID: task.ID, ExecutionID: source.ID, State: model.TaskFileStateDelivered, FilePath: p, FileName: "cover.png", Role: model.FileRoleCover, MimeType: "image/png", ContentHash: hash, FileSize: int64(len(body)), OSSKey: key, StorageProvider: store.Name()}
	if err := repo.TaskFiles().Create(t.Context(), file); err != nil {
		t.Fatal(err)
	}
	svc := NewAgentBootstrapService(repo, nil, AgentBootstrapConfig{Store: store}, zerolog.Nop())
	recovery := &model.TaskExecution{ID: uuid.NewString(), ParentExecutionID: source.ID, Purpose: model.TaskExecutionPurposePublicationRecovery}
	files, images, err := svc.buildPublicationRecoveryImages(t.Context(), recovery, task, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("valid delivered image cannot be recovered: %v", err)
	}
	if len(files) != 1 || len(images) != 1 || files[0].MaxBytes != maxTaskDeliveryImageBytes || images[0].FileSize != maxTaskDeliveryImageBytes {
		t.Fatalf("wrong recovery limits: %#v, %#v", files, images)
	}
	if err := db.Model(file).Update("file_size", maxTaskDeliveryImageBytes+1).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.buildPublicationRecoveryImages(t.Context(), recovery, task, time.Now().Add(time.Hour)); err == nil {
		t.Fatal("accepted image above delivery limit")
	}
}
