# Idempotent Final Artifact Collection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the existing success-or-failure final artifact hook upload only missing or changed bytes, submit one authoritative execution manifest, and remove `list_task_files` from Agent-side delivery validation while retaining it as a terminal query.

**Architecture:** The runner computes a stable SHA-256 snapshot for each final workspace file and asks the server whether the deterministic execution object already matches. OSS stores the hash as object metadata; the server verifies size and hash before replacing the pending execution manifest, then the existing `/complete` finalizer publishes or collects that manifest atomically. Server-generated MCP files remain in the same execution manifest, and repeated workspace manifests preserve task-file identity and billing settlement links.

**Tech Stack:** Go, GORM, Fiber v3, Alibaba Cloud OSS Go SDK, MCP Go SDK, repository plugin manifests and contract tests.

**Spec:** `docs/superpowers/specs/2026-07-22-idempotent-final-artifact-collection-design.md`

---

## File Map

- Modify `server/storage/storage.go`, `server/storage/oss.go`, and `server/storage/object_access_test.go` for OSS SHA-256 metadata.
- Modify `server/service/direct_upload.go`, `server/service/task_artifact_upload.go`, and `server/service/task_artifact_upload_test.go` for the hash-aware prepare and verified manifest protocol.
- Modify `server/repository/task_file.go` and `server/repository/task_file_execution_test.go` to preserve task-file identity during manifest replacement.
- Modify `agent/main.go`, `agent/artifact_upload.go`, and `agent/artifact_upload_test.go`, and create `agent/artifact_snapshot.go`, for idempotent final collection, failure normalization, and stable file reads.
- Modify `server/mcp/tools.go`, `server/mcp/tools_test.go`, plugin Agent/Skill files, both native manifests, `plugins/CHANGELOG.md`, and `server/agent/unified_plugin_contract_test.go` for the terminal-query-only contract.

### Task 1: Expose OSS SHA-256 Metadata

**Files:**
- Modify: `server/storage/storage.go:18-25`
- Modify: `server/storage/oss.go:143-163`
- Test: `server/storage/object_access_test.go`

- [ ] **Step 1: Write the failing OSS metadata test**

Add next to `TestOSSProviderStatObjectClassifiesNotFound`:

```go
func TestOSSProviderStatObjectReturnsArtifactSHA256(t *testing.T) {
	const hash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "7")
		w.Header().Set("Content-Type", "text/markdown")
		w.Header().Set("ETag", "etag-1")
		w.Header().Set("X-Oss-Meta-Sha256", hash)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	provider := newTestOSSProvider(t, server.URL)

	info, err := provider.StatObject(context.Background(), "output/article.md")
	if err != nil {
		t.Fatalf("StatObject: %v", err)
	}
	if info.SHA256 != hash || info.Size != 7 || info.ETag != "etag-1" {
		t.Fatalf("ObjectInfo = %#v, want stored hash, size, and ETag", info)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./server/storage -run TestOSSProviderStatObjectReturnsArtifactSHA256 -count=1`

Expected: FAIL because `storage.ObjectInfo` has no `SHA256` field.

- [ ] **Step 3: Add the field and OSS mapping**

In `server/storage/storage.go`:

```go
const ObjectMetadataSHA256 = "sha256"

type ObjectInfo struct {
	Key         string
	Size        int64
	MimeType    string
	ContentType string
	ETag        string
	SHA256      string
}
```

In `OSSProvider.StatObject`:

```go
sha256 := strings.ToLower(strings.TrimSpace(meta.Get("X-Oss-Meta-Sha256")))
return &ObjectInfo{
	Key: key, Size: size, MimeType: contentType, ContentType: contentType,
	ETag: etag, SHA256: sha256,
}, nil
```

Do not synthesize hashes for local or legacy objects; empty means not reusable.

- [ ] **Step 4: Run tests and commit**

Run: `go test ./server/storage -count=1`

Expected: PASS.

```bash
git add server/storage/storage.go server/storage/oss.go server/storage/object_access_test.go
git commit -m "feat(storage): expose artifact content hashes"
```

### Task 2: Make Prepare And Manifest Hash-Aware

**Files:**
- Modify: `server/service/direct_upload.go:67-85`
- Modify: `server/service/task_artifact_upload.go:34-261`
- Test: `server/service/task_artifact_upload_test.go`

- [ ] **Step 1: Make test fixtures express the new contract**

Add `fmt` to the test imports, then add:

```go
const taskArtifactTestSHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
```

Add `SHA256: taskArtifactTestSHA256` to every valid
`TaskArtifactPrepareRequest`. Change fake not-found behavior to:

```go
func (f *fakeTaskArtifactStorage) StatObject(_ context.Context, key string) (*storage.ObjectInfo, error) {
	if f.stats == nil || f.stats[key] == nil {
		return nil, fmt.Errorf("%w: %s", storage.ErrObjectNotFound, key)
	}
	cp := *f.stats[key]
	return &cp, nil
}
```

- [ ] **Step 2: Write failing idempotency tests**

Add the execution helper:

```go
func startTaskArtifactExecution(t *testing.T, repo repository.Repository, task *model.Task) string {
	t.Helper()
	executionID := uuid.NewString()
	task.CurrentExecutionID = &executionID
	if err := repo.Tasks().Update(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := repo.TaskExecutions().Create(context.Background(), &model.TaskExecution{
		ID: executionID, TaskID: task.ID, Attempt: 1, Target: "kubernetes",
		Status: model.TaskExecutionRunning, Started: true, StartedAt: &now,
	}); err != nil {
		t.Fatal(err)
	}
	return executionID
}
```

Add:

```go
func TestPrepareTaskArtifactUploadSkipsOnlyMatchingStoredObject(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	executionID := startTaskArtifactExecution(t, repo, task)
	key := buildTaskArtifactStorageKey(task, executionID, "output/article.md")
	request := TaskArtifactPrepareRequest{
		TaskID: task.ID, ExecutionID: executionID, RelativePath: "output/article.md",
		ContentType: "text/markdown", Size: 7, SHA256: taskArtifactTestSHA256,
	}
	store.stats = map[string]*storage.ObjectInfo{key: {
		Key: key, Size: 7, ContentType: "text/markdown", ETag: "etag-1", SHA256: taskArtifactTestSHA256,
	}}
	matched, err := svc.PrepareTaskArtifactUpload(context.Background(), task.ID, task.UserID, executionID, taskArtifactDirectUploadConfig(t), request)
	if err != nil {
		t.Fatal(err)
	}
	if matched.UploadRequired || matched.ETag != "etag-1" || matched.STSAccessKeyID != "" {
		t.Fatalf("matched prepare = %#v, want reusable object without credentials", matched)
	}
	store.stats[key].SHA256 = strings.Repeat("b", 64)
	changed, err := svc.PrepareTaskArtifactUpload(context.Background(), task.ID, task.UserID, executionID, taskArtifactDirectUploadConfig(t), request)
	if err != nil {
		t.Fatal(err)
	}
	if !changed.UploadRequired || changed.STSAccessKeyID == "" || changed.Headers["X-Oss-Meta-Sha256"] != taskArtifactTestSHA256 {
		t.Fatalf("changed prepare = %#v, want upload authority with hash metadata", changed)
	}
	store.stats[key].SHA256 = taskArtifactTestSHA256
	store.stats[key].Size = 8
	wrongSize, err := svc.PrepareTaskArtifactUpload(context.Background(), task.ID, task.UserID, executionID, taskArtifactDirectUploadConfig(t), request)
	if err != nil {
		t.Fatal(err)
	}
	if !wrongSize.UploadRequired {
		t.Fatalf("wrong-size prepare = %#v, want upload required", wrongSize)
	}
	store.stats[key].Size = 7
	store.stats[key].SHA256 = ""
	missingHash, err := svc.PrepareTaskArtifactUpload(context.Background(), task.ID, task.UserID, executionID, taskArtifactDirectUploadConfig(t), request)
	if err != nil {
		t.Fatal(err)
	}
	if !missingHash.UploadRequired {
		t.Fatalf("missing-hash prepare = %#v, want upload required", missingHash)
	}
}

func TestFinalizeTaskArtifactManifestRejectsStoredHashMismatch(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	executionID := startTaskArtifactExecution(t, repo, task)
	key := buildTaskArtifactStorageKey(task, executionID, "output/article.md")
	store.stats = map[string]*storage.ObjectInfo{key: {
		Key: key, Size: 7, ContentType: "text/markdown", SHA256: strings.Repeat("b", 64),
	}}
	err := svc.FinalizeTaskArtifactManifest(context.Background(), task.ID, task.UserID, executionID, TaskArtifactManifestRequest{
		TaskID: task.ID, ExecutionID: executionID,
		Files: []TaskArtifactManifestFile{{RelativePath: "output/article.md", ObjectKey: key, Size: 7, SHA256: taskArtifactTestSHA256}},
	})
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("FinalizeTaskArtifactManifest error = %v, want sha256 mismatch", err)
	}
}

func TestFinalizeTaskArtifactManifestRejectsStoredSizeMismatch(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	executionID := startTaskArtifactExecution(t, repo, task)
	key := buildTaskArtifactStorageKey(task, executionID, "output/article.md")
	store.stats = map[string]*storage.ObjectInfo{key: {
		Key: key, Size: 8, ContentType: "text/markdown", SHA256: taskArtifactTestSHA256,
	}}
	err := svc.FinalizeTaskArtifactManifest(context.Background(), task.ID, task.UserID, executionID, TaskArtifactManifestRequest{
		TaskID: task.ID, ExecutionID: executionID,
		Files: []TaskArtifactManifestFile{{RelativePath: "output/article.md", ObjectKey: key, Size: 7, SHA256: taskArtifactTestSHA256}},
	})
	if err == nil || !strings.Contains(err.Error(), "size mismatch") {
		t.Fatalf("FinalizeTaskArtifactManifest error = %v, want size mismatch", err)
	}
}

func TestTaskArtifactEndpointsRejectTerminalExecution(t *testing.T) {
	svc, repo, _, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := startTaskArtifactExecution(t, repo, task)
	changed, err := repo.TaskExecutions().Transition(ctx, executionID, []string{model.TaskExecutionRunning}, model.TaskExecutionSucceeded, model.ExecutionTransition{})
	if err != nil || !changed {
		t.Fatalf("terminal transition: changed=%v err=%v", changed, err)
	}
	prepare := TaskArtifactPrepareRequest{
		TaskID: task.ID, ExecutionID: executionID, RelativePath: "output/article.md",
		ContentType: "text/markdown", Size: 7, SHA256: taskArtifactTestSHA256,
	}
	if _, err := svc.PrepareTaskArtifactUpload(ctx, task.ID, task.UserID, executionID, taskArtifactDirectUploadConfig(t), prepare); !errors.Is(err, ErrTaskArtifactExecutionConflict) {
		t.Fatalf("terminal prepare error = %v, want execution conflict", err)
	}
	manifest := TaskArtifactManifestRequest{TaskID: task.ID, ExecutionID: executionID, Files: nil}
	if err := svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, manifest); !errors.Is(err, ErrTaskArtifactExecutionConflict) {
		t.Fatalf("terminal manifest error = %v, want execution conflict", err)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run:

```bash
go test ./server/service -run 'Test(PrepareTaskArtifactUploadSkipsOnlyMatchingStoredObject|FinalizeTaskArtifactManifestRejectsStored(Hash|Size)Mismatch|TaskArtifactEndpointsRejectTerminalExecution)' -count=1
```

Expected: FAIL because prepare always issues credentials and manifest does not
verify stored SHA-256. The terminal test may already pass and remains as an
explicit regression for the immutable terminal-execution contract.

- [ ] **Step 4: Implement the prepare response and exact-match check**

Add to `DirectUploadPrepareResult`:

```go
UploadRequired bool   `json:"upload_required"`
ETag           string `json:"etag,omitempty"`
```

Add to `task_artifact_upload.go`:

```go
const taskArtifactSHA256Header = "X-Oss-Meta-Sha256"
```

Require a normalized valid hash:

```go
if !validTaskArtifactSHA256(req.SHA256) {
	return nil, taskArtifactInvalidf("sha256 must be a 64-character hex string")
}
req.SHA256 = strings.ToLower(strings.TrimSpace(req.SHA256))
```

After deriving `key`, add:

```go
statProvider, ok := s.store.(taskArtifactObjectStatProvider)
if !ok {
	return nil, fmt.Errorf("%w: task artifact object metadata is unavailable", ErrTaskArtifactUnavailable)
}
info, statErr := statProvider.StatObject(ctx, key)
switch {
case statErr == nil && info != nil && info.Size == req.Size && strings.EqualFold(info.SHA256, req.SHA256):
	return &DirectUploadPrepareResult{
		UploadRequired: false, UploadID: uuid.NewString(), StagingKey: key, Key: key,
		PublicURL: s.store.GetURL(key), Method: "PUT", ETag: info.ETag,
		Headers: map[string]string{"Content-Type": contentType, taskArtifactSHA256Header: req.SHA256},
		MaxSize: maxTaskArtifactUploadBytes,
	}, nil
case statErr == nil:
	// Existing bytes differ; issue replacement authority below.
case errors.Is(statErr, storage.ErrObjectNotFound):
	// Missing object; issue upload authority below.
default:
	return nil, fmt.Errorf("%w: stat task artifact %s: %v", ErrTaskArtifactUnavailable, key, statErr)
}
```

Set this on the existing upload-required response:

```go
UploadRequired: true,
Headers: map[string]string{
	"Content-Type": contentType,
	taskArtifactSHA256Header: req.SHA256,
},
```

- [ ] **Step 5: Verify hash metadata during manifest finalization**

Require object metadata support before iterating over manifest files:

```go
statProvider, ok := s.store.(taskArtifactObjectStatProvider)
if !ok {
	return fmt.Errorf("%w: task artifact object metadata is unavailable", ErrTaskArtifactUnavailable)
}
```

Replace the existing `size := file.Size`, optional object-stat block, and later
`size <= 0` check with the following unconditional stat. Require the stored
size to equal the positive manifest size exactly, then require the stored hash
to match:

```go
stat, err := statProvider.StatObject(ctx, objectKey)
if err != nil {
	return fmt.Errorf("%w: stat task artifact %s: %v", ErrTaskArtifactUnavailable, objectKey, err)
}
if file.Size <= 0 {
	return taskArtifactInvalidf("file size is required for %s", relPath)
}
if stat.Size != file.Size {
	return taskArtifactInvalidf("task artifact %s size mismatch: manifest=%d storage=%d", relPath, file.Size, stat.Size)
}
if !strings.EqualFold(strings.TrimSpace(stat.SHA256), strings.TrimSpace(file.SHA256)) {
	return taskArtifactInvalidf(
		"task artifact %s sha256 mismatch: manifest=%s storage=%s",
		relPath, strings.ToLower(file.SHA256), strings.ToLower(stat.SHA256),
	)
}
size := stat.Size
if statContentType := firstNonEmptyString(stat.ContentType, stat.MimeType); statContentType != "" {
	contentType = statContentType
}
```

Give every successful manifest fixture matching `ObjectInfo.SHA256` metadata.
Missing metadata and zero-sized stored objects must be rejected, not treated as
reusable or unknown.

- [ ] **Step 6: Run tests and commit**

Run: `go test ./server/service -run 'Test.*TaskArtifact' -count=1`

Expected: PASS.

```bash
git add server/service/direct_upload.go server/service/task_artifact_upload.go server/service/task_artifact_upload_test.go
git commit -m "feat(server): make artifact uploads hash aware"
```

### Task 3: Preserve Task-File Identity During Manifest Replacement

**Files:**
- Modify: `server/repository/task_file.go:292-340`
- Test: `server/repository/task_file_execution_test.go`

- [ ] **Step 1: Write failing identity and removal tests**

Add:

```go
func TestTaskFileRepositoryReplacePendingExecutionPreservesLogicalPathIdentity(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e1")
	original := &model.TaskFile{
		ID: "file-1", TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending,
		Role: model.FileRoleMarkdown, FilePath: "output/article.md", FileName: "article.md",
		ContentHash: strings.Repeat("a", 64), OSSKey: "old-key",
	}
	if err := repo.TaskFiles().Create(ctx, original); err != nil {
		t.Fatal(err)
	}
	createdAt := original.CreatedAt
	if err := repo.TaskFiles().ReplacePendingCurrentExecution(ctx, "t1", "e1", []*model.TaskFile{{
		Role: model.FileRoleMarkdown, FilePath: "output/article.md", FileName: "article.md",
		ContentHash: strings.Repeat("b", 64), OSSKey: "new-key",
	}}); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.TaskFiles().FindByExecutionID(ctx, "e1")
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows = %#v, err=%v", rows, err)
	}
	if rows[0].ID != "file-1" || !rows[0].CreatedAt.Equal(createdAt) || rows[0].OSSKey != "new-key" {
		t.Fatalf("replacement = %#v, want stable ID/time and updated storage", rows[0])
	}
}

func TestTaskFileRepositoryReplacePendingExecutionRemovesMissingPaths(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e1")
	if err := repo.TaskFiles().BatchCreate(ctx, []*model.TaskFile{
		{ID: "keep", TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "output/keep.md", FileName: "keep.md"},
		{ID: "drop", TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "output/drop.md", FileName: "drop.md"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskFiles().ReplacePendingCurrentExecution(ctx, "t1", "e1", []*model.TaskFile{{Role: model.FileRoleOther, FilePath: "output/keep.md", FileName: "keep.md"}}); err != nil {
		t.Fatal(err)
	}
	rows, _ := repo.TaskFiles().FindByExecutionID(ctx, "e1")
	if len(rows) != 1 || rows[0].ID != "keep" {
		t.Fatalf("rows = %#v, want only stable keep row", rows)
	}
}

func TestTaskFileRepositoryReplacePendingExecutionIdenticalManifestIsStable(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e1")
	original := &model.TaskFile{
		ID: "file-1", TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending,
		Role: model.FileRoleMarkdown, FilePath: "output/article.md", FileName: "article.md",
		ContentHash: strings.Repeat("a", 64), OSSKey: "same-key",
	}
	if err := repo.TaskFiles().Create(ctx, original); err != nil {
		t.Fatal(err)
	}
	createdAt := original.CreatedAt
	if err := repo.TaskFiles().ReplacePendingCurrentExecution(ctx, "t1", "e1", []*model.TaskFile{{
		Role: model.FileRoleMarkdown, FilePath: "output/article.md", FileName: "article.md",
		ContentHash: strings.Repeat("a", 64), OSSKey: "same-key",
	}}); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.TaskFiles().FindByExecutionID(ctx, "e1")
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows = %#v, err=%v", rows, err)
	}
	if rows[0].ID != "file-1" || !rows[0].CreatedAt.Equal(createdAt) || rows[0].OSSKey != "same-key" || rows[0].ContentHash != strings.Repeat("a", 64) {
		t.Fatalf("identical replacement changed observable identity: %#v", rows[0])
	}
}
```

- [ ] **Step 2: Run tests to verify identity fails**

Run:

```bash
go test ./server/repository -run 'TestTaskFileRepositoryReplacePendingExecution(PreservesLogicalPathIdentity|RemovesMissingPaths|IdenticalManifestIsStable)' -count=1
```

Expected: identity test FAILS because delete-and-create assigns a new ID/time.

- [ ] **Step 3: Replace delete-and-create with stale-delete plus upsert**

Inside the locked transaction use:

```go
incomingPaths := make([]string, 0, len(files))
for _, file := range files {
	incomingPaths = append(incomingPaths, file.FilePath)
}
stale := tx.Where("task_id = ? AND execution_id = ? AND state = ?", taskID, executionID, model.TaskFileStatePending)
if len(incomingPaths) > 0 {
	stale = stale.Where("file_path NOT IN ?", incomingPaths)
}
if err := stale.Delete(&model.TaskFile{}).Error; err != nil {
	return err
}
for _, file := range files {
	if file.ID == "" {
		file.ID = uuid.NewString()
	}
	if err := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "task_id"}, {Name: "execution_id"}, {Name: "file_path"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"state", "file_name", "mime_type", "file_size", "oss_key", "oss_url",
			"storage_provider", "role", "content_hash", "media_id", "wechat_url",
		}),
	}).Create(file).Error; err != nil {
		return err
	}
}
```

The upsert excludes `id` and `created_at`, preserving billing outbox links.

- [ ] **Step 4: Run tests and commit**

Run:

```bash
go test ./server/repository -run 'TestTaskFileRepository.*(Pending|Publish|Collect|Discard|Artifact)' -count=1
```

Expected: PASS.

```bash
git add server/repository/task_file.go server/repository/task_file_execution_test.go
git commit -m "fix(repository): preserve artifact manifest identities"
```

### Task 4: Skip Reusable Uploads And Submit Empty Manifests

**Files:**
- Modify: `agent/artifact_upload.go:22-199,356-384`
- Test: `agent/artifact_upload_test.go`

- [ ] **Step 1: Extend the fake reporter**

Use:

```go
type fakeArtifactReporter struct {
	prepared      []ArtifactPrepareRequest
	prepareResult *ArtifactPrepareResponse
	manifest      ArtifactManifestRequest
	manifestCalls int
	manifestErrors []error
	progress      []string
}

func (f *fakeArtifactReporter) PrepareArtifactUpload(_ context.Context, req ArtifactPrepareRequest) (*ArtifactPrepareResponse, error) {
	f.prepared = append(f.prepared, req)
	if f.prepareResult != nil {
		copy := *f.prepareResult
		return &copy, nil
	}
	return &ArtifactPrepareResponse{
		UploadRequired: true,
		Key: "uploads/users/u/projects/p/tasks/" + req.TaskID + "/artifacts/" + req.RelativePath,
		Bucket: "bucket", Endpoint: "oss-cn-hangzhou.aliyuncs.com",
		Headers: map[string]string{"Content-Type": req.ContentType, "X-Oss-Meta-Sha256": req.SHA256},
		MaxSize: 512 * 1024 * 1024, ExpiresAt: "2026-07-09T10:15:00Z",
	}, nil
}

func (f *fakeArtifactReporter) ReportArtifactManifest(_ context.Context, req ArtifactManifestRequest) error {
	f.manifestCalls++
	f.manifest = req
	if len(f.manifestErrors) > 0 {
		err := f.manifestErrors[0]
		f.manifestErrors = f.manifestErrors[1:]
		return err
	}
	return nil
}
```

- [ ] **Step 2: Write failing skip and empty-manifest tests**

Add:

```go
func TestArtifactUploaderSkipsMatchingObjectButStillManifestsIt(t *testing.T) {
	root := t.TempDir()
	writeAgentArtifactTestFile(t, root, "output/article.md", "# article")
	reporter := &fakeArtifactReporter{prepareResult: &ArtifactPrepareResponse{
		UploadRequired: false, Key: "existing-key", ETag: "etag-existing",
		Headers: map[string]string{"Content-Type": "text/markdown"}, MaxSize: 512 * 1024 * 1024,
	}}
	uploader := NewArtifactUploader(&Config{TaskID: "task-1", ExecutionID: "execution-1", Workspace: root}, reporter)
	uploader.putObject = func(context.Context, *ArtifactPrepareResponse, string, string) (string, error) {
		t.Fatal("matching object must not be uploaded")
		return "", nil
	}
	if err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: true, WorkDir: root}); err != nil {
		t.Fatal(err)
	}
	if reporter.manifestCalls != 1 || len(reporter.manifest.Files) != 1 || reporter.manifest.Files[0].ETag != "etag-existing" {
		t.Fatalf("manifest = %#v calls=%d", reporter.manifest, reporter.manifestCalls)
	}
}

func TestJobArtifactUploaderSubmitsEmptyManifest(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "output"), 0o755); err != nil {
		t.Fatal(err)
	}
	reporter := &fakeArtifactReporter{}
	uploader := NewArtifactUploader(&Config{TaskID: "task-1", ExecutionID: "execution-1", Workspace: root}, reporter)
	if err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: false, WorkDir: root}); err != nil {
		t.Fatal(err)
	}
	if reporter.manifestCalls != 1 || reporter.manifest.ExecutionID != "execution-1" || len(reporter.manifest.Files) != 0 {
		t.Fatalf("empty manifest = %#v calls=%d", reporter.manifest, reporter.manifestCalls)
	}
}

func TestJobArtifactUploaderSubmitsEmptyManifestWhenOutputIsMissing(t *testing.T) {
	root := t.TempDir()
	reporter := &fakeArtifactReporter{}
	uploader := NewArtifactUploader(&Config{TaskID: "task-1", ExecutionID: "execution-1", Workspace: root}, reporter)
	if err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: false, WorkDir: root}); err != nil {
		t.Fatal(err)
	}
	if reporter.manifestCalls != 1 || len(reporter.prepared) != 0 || len(reporter.manifest.Files) != 0 {
		t.Fatalf("missing-output manifest = %#v calls=%d", reporter.manifest, reporter.manifestCalls)
	}
}

func TestArtifactUploaderRetryAfterManifestFailureReusesUploadedObject(t *testing.T) {
	root := t.TempDir()
	writeAgentArtifactTestFile(t, root, "output/article.md", "# article")
	reporter := &fakeArtifactReporter{manifestErrors: []error{errors.New("manifest unavailable")}}
	uploader := NewArtifactUploader(&Config{TaskID: "task-1", ExecutionID: "execution-1", Workspace: root}, reporter)
	var uploads int
	uploader.putObject = func(_ context.Context, prepared *ArtifactPrepareResponse, _ string, _ string) (string, error) {
		uploads++
		reporter.prepareResult = &ArtifactPrepareResponse{
			UploadRequired: false, Key: prepared.Key, ETag: "etag-existing",
			Headers: prepared.Headers, MaxSize: prepared.MaxSize,
		}
		return "etag-existing", nil
	}
	if err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: true, WorkDir: root}); err == nil {
		t.Fatal("first manifest report unexpectedly succeeded")
	}
	if err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: true, WorkDir: root}); err != nil {
		t.Fatal(err)
	}
	if uploads != 1 || reporter.manifestCalls != 2 {
		t.Fatalf("uploads=%d manifestCalls=%d, want one PUT and two manifests", uploads, reporter.manifestCalls)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run:

```bash
go test ./agent -run 'Test(ArtifactUploaderSkipsMatchingObjectButStillManifestsIt|ArtifactUploaderRetryAfterManifestFailureReusesUploadedObject|JobArtifactUploaderSubmitsEmptyManifest)' -count=1
```

Expected: FAIL because skip fields are absent and empty scans return early.

- [ ] **Step 4: Honor prepare decisions and attach PUT metadata**

Add to `ArtifactPrepareResponse`:

```go
UploadRequired bool   `json:"upload_required"`
ETag           string `json:"etag,omitempty"`
```

For execution jobs, distinguish a missing `output/` directory from a legacy
workspace scan and treat it as an empty file list:

```go
var (
	files []WorkspaceArtifact
	err   error
)
if u.cfg.ExecutionID != "" {
	outputDir := filepath.Join(workDir, "output")
	info, statErr := os.Lstat(outputDir)
	switch {
	case os.IsNotExist(statErr):
		files = []WorkspaceArtifact{}
	case statErr != nil:
		return fmt.Errorf("inspect job output directory: %w", statErr)
	case !info.IsDir() || info.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("job output must be a real directory")
	default:
		files, err = scanWorkspaceArtifacts(ctx, workDir, u.cfg.TaskType)
	}
} else {
	files, err = scanWorkspaceArtifacts(ctx, workDir, u.cfg.TaskType)
}
if err != nil {
	return err
}
```

Remove both the old missing-output return and the empty-files early return. In
the loop:

```go
etag := prepared.ETag
if prepared.UploadRequired {
	etag, err = u.putObject(ctx, prepared, file.LocalPath, contentType)
	if err != nil {
		return fmt.Errorf("upload artifact %s: %w", file.RelativePath, err)
	}
}
```

Always call `ReportArtifactManifest`. Keep progress conditional on at least one
manifested file. Add to `putOSSObjectFromFile` options:

```go
if hash := strings.TrimSpace(prepared.Headers["X-Oss-Meta-Sha256"]); hash != "" {
	options = append(options, oss.Meta("sha256", strings.ToLower(hash)))
}
```

Extract the bucket operation so it can be tested without credential discovery:

```go
func putOSSObjectFromBucket(ctx context.Context, bucket *oss.Bucket, key, localPath, contentType, hash string) (string, error) {
	options := []oss.Option{oss.WithContext(ctx)}
	if contentType != "" {
		options = append(options, oss.ContentType(contentType))
	}
	if hash != "" {
		options = append(options, oss.Meta("sha256", strings.ToLower(hash)))
	}
	if err := bucket.PutObjectFromFile(key, localPath, options...); err != nil {
		return "", fmt.Errorf("put oss object: %w", err)
	}
	return "", nil
}
```

Call this helper from `putOSSObjectFromFile`. Add the complete header test:

```go
func TestPutOSSObjectFromBucketAddsSHA256Metadata(t *testing.T) {
	const hash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var gotHash string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHash = r.Header.Get("X-Oss-Meta-Sha256")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	client, err := oss.New(server.URL, "ak", "secret", oss.UseCname(true))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.Bucket("bucket")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "article.md")
	if err := os.WriteFile(path, []byte("article"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := putOSSObjectFromBucket(context.Background(), bucket, "output/article.md", path, "text/markdown", hash); err != nil {
		t.Fatal(err)
	}
	if gotHash != hash {
		t.Fatalf("X-Oss-Meta-Sha256 = %q, want %q", gotHash, hash)
	}
}
```

- [ ] **Step 5: Preserve failure normalization in a testable helper**

Add to `agent/main.go` and call it from the current upload-error branch:

```go
func applyArtifactUploadFailure(result *serveragent.ExecutionResult, runErr, uploadErr error) error {
	if uploadErr == nil {
		return runErr
	}
	if result != nil && result.Success {
		result.Success = false
		result.Error = "artifact upload failed: " + uploadErr.Error()
	}
	if runErr == nil {
		return uploadErr
	}
	return runErr
}
```

Add:

```go
func TestApplyArtifactUploadFailureRejectsOtherwiseSuccessfulRun(t *testing.T) {
	result := &serveragent.ExecutionResult{Success: true}
	uploadErr := errors.New("manifest unavailable")
	gotErr := applyArtifactUploadFailure(result, nil, uploadErr)
	if !errors.Is(gotErr, uploadErr) || result.Success || result.Error != "artifact upload failed: manifest unavailable" {
		t.Fatalf("result=%#v err=%v", result, gotErr)
	}
}
```

- [ ] **Step 6: Run tests and commit**

Run: `go test ./agent -run 'Test.*Artifact' -count=1`

Expected: PASS.

```bash
git add agent/main.go agent/artifact_upload.go agent/artifact_upload_test.go
git commit -m "feat(agent): skip unchanged final artifacts"
```

### Task 5: Upload A Stable File Snapshot

**Files:**
- Create: `agent/artifact_snapshot.go`
- Modify: `agent/artifact_upload.go`
- Modify: `agent/artifact_upload_test.go`

- [ ] **Step 1: Write the failing mutation test**

Add `crypto/sha256` and `encoding/hex` to the test imports. Change the uploader
test seam to accept an `io.Reader`, then add:

```go
func TestArtifactUploaderRetriesFileChangedDuringUpload(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "output", "article.md")
	writeAgentArtifactTestFile(t, root, "output/article.md", "version-one")
	reporter := &fakeArtifactReporter{}
	uploader := NewArtifactUploader(&Config{TaskID: "task-1", ExecutionID: "execution-1", Workspace: root}, reporter)
	var uploads int
	uploader.putObject = func(_ context.Context, _ *ArtifactPrepareResponse, source io.Reader, _ string) (string, error) {
		uploads++
		if _, err := io.Copy(io.Discard, source); err != nil {
			return "", err
		}
		if uploads == 1 {
			if err := os.WriteFile(path, []byte("version-two-expanded"), 0o644); err != nil {
				return "", err
			}
		}
		return fmt.Sprintf("etag-%d", uploads), nil
	}
	if err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: true, WorkDir: root}); err != nil {
		t.Fatal(err)
	}
	if uploads != 2 || reporter.manifest.Files[0].SHA256 != sha256Hex("version-two-expanded") {
		t.Fatalf("uploads=%d manifest=%#v, want retried final bytes", uploads, reporter.manifest)
	}
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
```

Update the existing slow-reader cancellation test so its injected file supports
`io.ReadSeeker`, `Stat()`, and `Close()`.

Add bounded exhaustion coverage:

```go
func TestArtifactUploaderFailsWhenFileNeverStabilizes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "output", "article.md")
	writeAgentArtifactTestFile(t, root, "output/article.md", "version-0")
	reporter := &fakeArtifactReporter{}
	uploader := NewArtifactUploader(&Config{TaskID: "task-1", ExecutionID: "execution-1", Workspace: root}, reporter)
	var uploads int
	uploader.putObject = func(_ context.Context, _ *ArtifactPrepareResponse, source io.Reader, _ string) (string, error) {
		uploads++
		if _, err := io.Copy(io.Discard, source); err != nil {
			return "", err
		}
		return "", os.WriteFile(path, []byte(fmt.Sprintf("version-%d-expanded", uploads)), 0o644)
	}
	err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: true, WorkDir: root})
	if err == nil || !strings.Contains(err.Error(), "changed during final collection") || uploads != maxArtifactSnapshotAttempts {
		t.Fatalf("error=%v uploads=%d", err, uploads)
	}
}
```

- [ ] **Step 2: Run the mutation test to verify it fails**

Run: `go test ./agent -run TestArtifactUploaderRetriesFileChangedDuringUpload -count=1`

Expected: FAIL because the uploader hashes and uploads separate opens without a
stability recheck.

- [ ] **Step 3: Create the snapshot helper**

Create `agent/artifact_snapshot.go`:

```go
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
)

const maxArtifactSnapshotAttempts = 3

type artifactFile interface {
	io.ReadSeeker
	io.Closer
	Stat() (fs.FileInfo, error)
}

type artifactSnapshot struct {
	file   artifactFile
	path   string
	before fs.FileInfo
	size   int64
	hash   string
}

var openArtifactFile = func(path string) (artifactFile, error) { return os.Open(path) }

func openArtifactSnapshot(ctx context.Context, path string) (*artifactSnapshot, error) {
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !pathInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("artifact is not a regular file: %s", path)
	}
	file, err := openArtifactFile(path)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(pathInfo, info) {
		file.Close()
		return nil, fmt.Errorf("artifact changed while opening: %s", path)
	}
	h := sha256.New()
	if _, err := copyArtifactWithContext(ctx, h, file); err != nil {
		file.Close()
		return nil, err
	}
	return &artifactSnapshot{file: file, path: path, before: info, size: info.Size(), hash: hex.EncodeToString(h.Sum(nil))}, nil
}

func (s *artifactSnapshot) rewind() error {
	_, err := s.file.Seek(0, io.SeekStart)
	return err
}

func (s *artifactSnapshot) unchanged() bool {
	after, err := s.file.Stat()
	if err != nil || after.Size() != s.before.Size() || !after.ModTime().Equal(s.before.ModTime()) {
		return false
	}
	pathInfo, err := os.Lstat(s.path)
	return err == nil && pathInfo.Mode().IsRegular() && os.SameFile(pathInfo, after)
}

func copyArtifactWithContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buffer := make([]byte, 32*1024)
	var total int64
	for {
		if err := artifactContextCause(ctx); err != nil {
			return total, err
		}
		n, readErr := src.Read(buffer)
		if n > 0 {
			written, writeErr := dst.Write(buffer[:n])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
		}
		if readErr == io.EOF {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
	}
}
```

Remove the old `openArtifactFile` declaration from `artifact_upload.go`. Replace
its old hashing function with this wrapper so `ScanWorkspaceArtifacts` keeps its
existing result contract while sharing the secure open path:

```go
func fileSHA256(ctx context.Context, path string) (string, error) {
	snapshot, err := openArtifactSnapshot(ctx, path)
	if err != nil {
		return "", err
	}
	defer snapshot.file.Close()
	return snapshot.hash, nil
}
```

- [ ] **Step 4: Integrate bounded snapshot retries**

Change the uploader seam to:

```go
putObject func(context.Context, *ArtifactPrepareResponse, io.Reader, string) (string, error)
```

Replace the Task 4 path helper with the streaming form:

```go
func putOSSObjectFromBucket(ctx context.Context, bucket *oss.Bucket, key string, source io.Reader, contentType, hash string) (string, error) {
	options := []oss.Option{oss.WithContext(ctx)}
	if contentType != "" {
		options = append(options, oss.ContentType(contentType))
	}
	if hash != "" {
		options = append(options, oss.Meta("sha256", strings.ToLower(hash)))
	}
	if err := bucket.PutObject(key, source, options...); err != nil {
		return "", fmt.Errorf("put oss object: %w", err)
	}
	return "", nil
}
```

Update `putOSSObjectFromFile` into `putOSSObject` so it constructs the client and
bucket exactly as before, then calls the helper with
`prepared.Headers["X-Oss-Meta-Sha256"]`.

Update `TestPutOSSObjectFromBucketAddsSHA256Metadata` to pass the open file as
the streaming source:

```go
file, err := os.Open(path)
if err != nil {
	t.Fatal(err)
}
defer file.Close()
if _, err := putOSSObjectFromBucket(context.Background(), bucket, "output/article.md", file, "text/markdown", hash); err != nil {
	t.Fatal(err)
}
```

Update every injected `uploader.putObject` callback in
`agent/artifact_upload_test.go` from the old path argument to an `io.Reader`;
tests that only count uploads may ignore the reader, while content-sensitive
tests consume it with `io.Copy(io.Discard, source)`.

Add this complete method and call it once per scanned logical file:

```go
func (u *ArtifactUploader) uploadWorkspaceArtifact(ctx context.Context, file WorkspaceArtifact) (ArtifactManifestFile, error) {
	for attempt := 1; attempt <= maxArtifactSnapshotAttempts; attempt++ {
		snapshot, err := openArtifactSnapshot(ctx, file.LocalPath)
		if err != nil {
			return ArtifactManifestFile{}, fmt.Errorf("snapshot artifact %s: %w", file.RelativePath, err)
		}
		prepared, err := u.reporter.PrepareArtifactUpload(ctx, ArtifactPrepareRequest{
			TaskID: u.cfg.TaskID, ExecutionID: u.cfg.ExecutionID,
			RelativePath: file.RelativePath, Filename: file.Filename,
			ContentType: file.ContentType, Size: snapshot.size, SHA256: snapshot.hash,
		})
		if err != nil {
			snapshot.file.Close()
			return ArtifactManifestFile{}, fmt.Errorf("prepare artifact upload %s: %w", file.RelativePath, err)
		}
		if prepared == nil || strings.TrimSpace(prepared.Key) == "" {
			snapshot.file.Close()
			return ArtifactManifestFile{}, fmt.Errorf("prepare artifact upload %s returned no object key", file.RelativePath)
		}
		if prepared.MaxSize > 0 && snapshot.size > prepared.MaxSize {
			snapshot.file.Close()
			return ArtifactManifestFile{}, fmt.Errorf("artifact %s exceeds prepared upload limit", file.RelativePath)
		}
		contentType := prepared.Headers["Content-Type"]
		if contentType == "" {
			contentType = file.ContentType
		}
		etag := prepared.ETag
		if prepared.UploadRequired {
			if err := snapshot.rewind(); err != nil {
				snapshot.file.Close()
				return ArtifactManifestFile{}, err
			}
			etag, err = u.putObject(ctx, prepared, snapshot.file, contentType)
		}
		stable := snapshot.unchanged()
		closeErr := snapshot.file.Close()
		if err != nil {
			return ArtifactManifestFile{}, fmt.Errorf("upload artifact %s: %w", file.RelativePath, err)
		}
		if closeErr != nil {
			return ArtifactManifestFile{}, fmt.Errorf("close artifact %s: %w", file.RelativePath, closeErr)
		}
		if stable {
			return ArtifactManifestFile{
				RelativePath: file.RelativePath, ObjectKey: prepared.Key,
				ContentType: contentType, Size: snapshot.size,
				SHA256: snapshot.hash, ETag: etag,
			}, nil
		}
	}
	return ArtifactManifestFile{}, fmt.Errorf("artifact %s changed during final collection", file.RelativePath)
}
```

Append only the returned stable manifest entry. Keep large files streamed; do
not use `io.ReadAll`.

- [ ] **Step 5: Run repeated Agent tests and commit**

Run:

```bash
go test ./agent -run 'Test.*Artifact' -count=1
go test ./agent -run TestArtifactUploaderRetriesFileChangedDuringUpload -count=20
```

Expected: PASS.

```bash
git add agent/artifact_snapshot.go agent/artifact_upload.go agent/artifact_upload_test.go
git commit -m "fix(agent): snapshot final artifacts consistently"
```

### Task 6: Lock Workspace/MCP Merge And Version Semantics

**Files:**
- Modify: `server/service/task_artifact_upload.go:268-290`
- Modify: `server/service/task_artifact_upload_test.go`
- Test: `server/repository/task_file_execution_test.go`

- [ ] **Step 1: Write the same-path settlement test**

Add:

```go
func TestFinalizeTaskArtifactManifestWorkspacePathWinsWithoutBreakingSettlement(t *testing.T) {
	fixture := newBillingWalletFixture(t, 500, 0, 0)
	ctx := context.Background()
	logger := zerolog.New(io.Discard)
	store := &fakeTaskArtifactStorage{name: "oss"}
	svc := NewTaskService(fixture.repo, nil, &mockEnqueuer{}, store, &logger, "", nil, "", nil, nil)
	svc.SetBillingWalletService(fixture.wallet)
	projectID := createTestProject(t, fixture.repo, billingWalletUserID, model.PlatformArticle)
	task := &model.Task{ID: uuid.NewString(), UserID: billingWalletUserID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusRunning}
	if err := fixture.repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	executionID := startTaskArtifactExecution(t, fixture.repo, task)
	operationID := "internal:image:collision"
	generated, err := svc.UploadExecutionTaskFileWithSettlementFromReader(ctx, task.ID, task.UserID, executionID, "output/cover.png", strings.NewReader("old"), "image/png", 3, TaskFileOperationSettlement{
		CatalogID: "retail-test-v1", SKUID: "image.cover.v1", ToolCallID: operationID, RequestFingerprint: billingFingerprint(operationID),
	})
	if err != nil {
		t.Fatal(err)
	}
	key := buildTaskArtifactStorageKey(task, executionID, "output/cover.png")
	newHash := strings.Repeat("c", 64)
	store.stats = map[string]*storage.ObjectInfo{key: {Key: key, Size: 3, ContentType: "image/png", SHA256: newHash}}
	if err := svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, TaskArtifactManifestRequest{
		TaskID: task.ID, ExecutionID: executionID,
		Files: []TaskArtifactManifestFile{{RelativePath: "output/cover.png", ObjectKey: key, ContentType: "image/png", Size: 3, SHA256: newHash}},
	}); err != nil {
		t.Fatal(err)
	}
	rows, _ := fixture.repo.TaskFiles().FindByExecutionID(ctx, executionID)
	if len(rows) != 1 || rows[0].ID != generated.ID || rows[0].OSSKey != key || rows[0].ContentHash != newHash {
		t.Fatalf("merged rows = %#v, want workspace bytes on stable MCP identity", rows)
	}
	settlement, err := fixture.repo.Billing().FindSettlementByKey(ctx, "mcp-image-settlement", billingFingerprint(task.ID, executionID, operationID))
	if err != nil || settlement.ResourceID != generated.ID {
		t.Fatalf("settlement = %#v err=%v", settlement, err)
	}
}
```

Add the empty-manifest test:

```go
func TestFinalizeTaskArtifactEmptyManifestPreservesOnlyMCPArtifacts(t *testing.T) {
	svc, repo, _, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := startTaskArtifactExecution(t, repo, task)
	mcpFile := &model.TaskFile{
		ID: uuid.NewString(), TaskID: task.ID, ExecutionID: executionID,
		State: model.TaskFileStatePending, Role: model.FileRoleImage,
		FilePath: "output/cover.png", FileName: "cover.png",
		OSSKey: buildTaskMCPArtifactStoragePrefix(task, executionID) + "output/cover.png",
	}
	workspaceFile := &model.TaskFile{
		ID: uuid.NewString(), TaskID: task.ID, ExecutionID: executionID,
		State: model.TaskFileStatePending, Role: model.FileRoleMarkdown,
		FilePath: "output/article.md", FileName: "article.md",
		OSSKey: buildTaskArtifactStorageKey(task, executionID, "output/article.md"),
	}
	if err := repo.TaskFiles().BatchCreate(ctx, []*model.TaskFile{mcpFile, workspaceFile}); err != nil {
		t.Fatal(err)
	}
	if err := svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, TaskArtifactManifestRequest{
		TaskID: task.ID, ExecutionID: executionID, Files: nil,
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.TaskFiles().FindByExecutionID(ctx, executionID)
	if err != nil || len(rows) != 1 || rows[0].ID != mcpFile.ID {
		t.Fatalf("rows = %#v err=%v, want only MCP artifact", rows, err)
	}
}
```

- [ ] **Step 2: Run tests to expose any remaining gap**

Run:

```bash
go test ./server/service -run 'TestFinalizeTaskArtifact(ManifestWorkspacePathWinsWithoutBreakingSettlement|EmptyManifestPreservesOnlyMCPArtifacts)' -count=1
```

Expected before Task 3: FAIL on stable identity. After Task 3 it may PASS; retain
the tests as regression coverage.

- [ ] **Step 3: Make collision precedence explicit**

Keep workspace `manifestPaths` authoritative. Add above the existing skip:

```go
// The post-execution workspace manifest owns a colliding logical path.
// ReplacePendingCurrentExecution upserts that path, preserving the existing
// task-file ID so operation settlement evidence remains linked without a new charge.
```

Do not enqueue settlement work or delete MCP objects from storage here.

- [ ] **Step 4: Verify terminal versions and commit**

Run:

```bash
go test ./server/repository -run 'TestTaskFileRepository(Publish|Collect)' -count=1
go test ./server/service -run 'Test.*(ExecutionArtifactManifest|TaskArtifact)' -count=1
```

Expected: PASS, including successful supersession and failed-attempt collection.

```bash
git add server/service/task_artifact_upload.go server/service/task_artifact_upload_test.go server/repository/task_file_execution_test.go
git commit -m "test(server): lock final artifact merge semantics"
```

### Task 7: Make `list_task_files` Terminal-Query-Only

**Files:**
- Modify: `server/mcp/tools.go:235-245`
- Modify: `server/mcp/tools_test.go:53-104`
- Modify: `plugins/agents/ecommerce.md:65-70`
- Modify: `plugins/agents/ecommerce.toml:54-60`
- Modify: `plugins/skills/seednote-visual-design/SKILL.md:33-38`
- Modify: `plugins/.claude-plugin/plugin.json`
- Modify: `plugins/.codex-plugin/plugin.json`
- Modify: `plugins/CHANGELOG.md`
- Modify: `server/agent/unified_plugin_contract_test.go`

- [ ] **Step 1: Write failing visibility and plugin tests**

Add pending and superseded rows to `TestListTaskFilesReturnsCollectedFiles`:

```go
{ID: uuid.NewString(), TaskID: task.ID, ExecutionID: "running", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "output/pending.md", FileName: "pending.md"},
{ID: uuid.NewString(), TaskID: task.ID, ExecutionID: "old", State: model.TaskFileStateSuperseded, Role: model.FileRoleOther, FilePath: "output/old.md", FileName: "old.md"},
```

Keep expected returned count at two and add this assertion after decoding:

```go
for _, raw := range files {
	state := raw.(map[string]any)["state"]
	if state == model.TaskFileStatePending || state == model.TaskFileStateSuperseded {
		t.Fatalf("hidden task-file state leaked: %#v", raw)
	}
}
```

Add to `unified_plugin_contract_test.go`:

```go
func TestRuntimeContractsDoNotUseTaskFileListingAsCompletionGate(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range []string{
		"plugins/agents/ecommerce.md",
		"plugins/agents/ecommerce.toml",
		"plugins/skills/seednote-visual-design/SKILL.md",
	} {
		body := readRepoFile(t, filepath.Join(root, rel))
		if strings.Contains(body, "list_task_files") {
			t.Fatalf("%s still depends on list_task_files during execution", rel)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify the plugin contract fails**

Run:

```bash
go test ./server/mcp -run 'TestListTaskFiles' -count=1
go test ./server/agent -run TestRuntimeContractsDoNotUseTaskFileListingAsCompletionGate -count=1
```

Expected: MCP test PASS; plugin test FAIL on three current references.

- [ ] **Step 3: Correct the MCP and Agent contracts**

Use this MCP description:

```go
Description: "List terminal task files owned by the authenticated user. Returns the latest successful published deliverables followed by collected files retained from failed attempts, including names, roles, states, sizes, and download URLs. This is a post-run inspection and recovery query, not a live workspace listing or upload-completion check; pending and superseded files are excluded.",
```

Remove `list_task_files` from both Ecommerce required-tool lists. Replace the
Seednote `generate_image` row with:

```markdown
| `generate_image` (project_id, prompt, image_type, output_path, task_id, ref_image_paths, verify_with_vision, verification_prompt) | 生成、持久化、登记并核验单张图片。每次都传 `verify_with_vision=true` 和当页动态 `verification_prompt`；**`task_id=$TASK_ID` 必传**。服务端生成资产由当前 execution 持有，并在最终 Hook 提交工作区 manifest 时合并进入同一终态文件集合；Agent 不调用 `list_task_files` 判断当前上传是否完成 |
```

- [ ] **Step 4: Release plugin version `3.0.2`**

Change both manifest versions to `3.0.2`. Add below the changelog introduction:

```markdown
## [3.0.2] - 2026-07-22

### Changed

- Removed `list_task_files` from in-execution Agent delivery validation; the existing final Hook remains the single workspace artifact collector for both successful and failed runs.
- Clarified that `list_task_files` is a terminal inspection and recovery query over published and collected files.
```

- [ ] **Step 5: Run tests and commit**

Run:

```bash
go test ./server/mcp -run 'TestListTaskFiles' -count=1
go test ./server/agent -run 'Test(UnifiedPluginLayout|RuntimeContractsDoNotUseTaskFileListingAsCompletionGate|ClaudeCodePluginChangelogMentionsManifestVersion)' -count=1
```

Expected: PASS.

```bash
git add server/mcp/tools.go server/mcp/tools_test.go server/agent/unified_plugin_contract_test.go plugins/agents/ecommerce.md plugins/agents/ecommerce.toml plugins/skills/seednote-visual-design/SKILL.md plugins/.claude-plugin/plugin.json plugins/.codex-plugin/plugin.json plugins/CHANGELOG.md
git commit -m "fix(plugin): make task file listing terminal only"
```

### Task 8: Full Verification

**Files:**
- Verify all files changed in Tasks 1-7.

- [ ] **Step 1: Format changed Go files**

Run:

```bash
gofmt -w agent/main.go agent/artifact_snapshot.go agent/artifact_upload.go agent/artifact_upload_test.go server/storage/storage.go server/storage/oss.go server/storage/object_access_test.go server/service/direct_upload.go server/service/task_artifact_upload.go server/service/task_artifact_upload_test.go server/repository/task_file.go server/repository/task_file_execution_test.go server/mcp/tools.go server/mcp/tools_test.go server/agent/unified_plugin_contract_test.go
```

Expected: exit 0.

- [ ] **Step 2: Run focused suites**

Run:

```bash
go test ./agent ./server/storage ./server/repository ./server/service ./server/mcp ./server/agent -run 'Test.*(Artifact|TaskFile|Plugin|RuntimeContract)' -count=1
```

Expected: PASS.

- [ ] **Step 3: Run full tests and builds**

Run:

```bash
go test ./... -count=1
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
```

Expected: all commands exit 0.

- [ ] **Step 4: Check diff hygiene**

Run:

```bash
git diff --check
git status --short
```

Expected: no whitespace errors and only intended implementation files, or a
clean worktree after task commits. Leave unrelated user-owned files untouched.

- [ ] **Step 5: Commit formatting fixes only when needed**

If Step 1 changed already committed files:

```bash
git add agent server plugins
git commit -m "style: format artifact collection changes"
```

Expected: normally no extra commit is required because each task formats before
its own commit.
