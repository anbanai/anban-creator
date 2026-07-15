# Bootstrap Pending Reference Validation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make pending-upload identity checks accept canonical `%2F`-encoded OSS URLs, reject malformed pending keys at write time, and keep bootstrap authorization fail-closed.

**Architecture:** Reuse `storage.StorageKeyFromURL` as the only URL-to-object-key canonicalizer. Extract upload identity and compare pending URLs from the decoded key, then retain the existing repository-backed user, purpose, status, host, and exact-key authorization checks.

**Tech Stack:** Go 1.24, Fiber v3, GORM repositories, `net/url`, repository table-driven tests.

---

### Task 1: Canonicalize Pending Upload URLs

**Files:**
- Modify: `server/service/direct_upload_test.go`
- Modify: `server/service/direct_upload.go`

- [ ] **Step 1: Write failing canonicalization tests**

Add these focused tests to `server/service/direct_upload_test.go`:

```go
func TestFinalizePendingUploadURLsAcceptsEncodedOSSPath(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	upload := &model.PendingUpload{
		ID: "upload-1", UserID: "user-1", Purpose: DirectUploadPurposeTaskReference,
		Key: "uploads/pending/user-1/upload-1/ref.png",
		PublicURL: "https://cdn.example.com/uploads/pending/user-1/upload-1/ref.png",
		Status: model.PendingUploadStatusPending, ExpiresAt: now.Add(time.Minute),
	}
	repo := &fakePendingUploadRepo{uploads: map[string]*model.PendingUpload{upload.ID: upload}}
	raw := "https://cdn.example.com/uploads%2Fpending%2Fuser-1%2Fupload-1%2Fref.png?Expires=1&Signature=redacted"

	if err := FinalizePendingUploadURLs(context.Background(), repo, upload.UserID, upload.Purpose, []string{raw}, now); err != nil {
		t.Fatalf("FinalizePendingUploadURLs() error = %v", err)
	}
	if len(repo.finalized) != 1 || repo.finalized[0] != "upload-1" {
		t.Fatalf("finalized = %v, want [upload-1]", repo.finalized)
	}
}

func TestFinalizePendingUploadURLsRejectsMalformedPendingKey(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	raw := "https://cdn.example.com/uploads%2Fpending%2Fuser-1"
	err := FinalizePendingUploadURLs(context.Background(), &fakePendingUploadRepo{}, "user-1", DirectUploadPurposeTaskReference, []string{raw}, now)
	if !errors.Is(err, ErrPendingUploadInvalidURL) {
		t.Fatalf("error = %v, want ErrPendingUploadInvalidURL", err)
	}
}
```

- [ ] **Step 2: Run the tests and verify RED**

Run:

```bash
go test ./server/service -run 'TestFinalizePendingUploadURLs(AcceptsEncodedOSSPath|RejectsMalformedPendingKey)' -count=1
```

Expected: the encoded URL is skipped or mismatched, and the malformed pending key returns nil instead of `ErrPendingUploadInvalidURL`.

- [ ] **Step 3: Replace raw-string identity parsing with canonical key parsing**

In `server/service/direct_upload.go`, import `server/storage` and replace `pendingUploadIDFromURL` with canonical helpers:

```go
func pendingUploadKeyFromURL(raw string) (string, bool) {
	key, ok := storage.StorageKeyFromURL(strings.TrimSpace(raw))
	if !ok {
		return "", false
	}
	key = strings.TrimPrefix(key, "/")
	if path.Clean(key) != key {
		return "", false
	}
	return key, true
}

func pendingUploadIDFromURL(raw string) string {
	key, ok := pendingUploadKeyFromURL(raw)
	if !ok {
		return ""
	}
	segments := strings.Split(key, "/")
	if len(segments) < 5 || segments[0] != "uploads" || segments[1] != "pending" || segments[2] == "" || segments[3] == "" {
		return ""
	}
	return segments[3]
}
```

Update `pendingUploadURLMatches` to compare decoded paths:

```go
candidatePath := strings.TrimPrefix(candidateURL.Path, "/")
publicPath := strings.TrimPrefix(publicURL.Path, "/")
return candidatePath == publicPath && candidatePath == key
```

In `FinalizePendingUploadURLs`, reject canonical pending keys that lack an upload ID:

```go
id := pendingUploadIDFromURL(raw)
if id == "" {
	if key, ok := pendingUploadKeyFromURL(raw); ok && strings.HasPrefix(key, "uploads/pending/") {
		return ErrPendingUploadInvalidURL
	}
	continue
}
```

- [ ] **Step 4: Run direct-upload tests and verify GREEN**

Run:

```bash
go test ./server/service -run 'Test(Finalize|Validate)PendingUpload' -count=1
```

Expected: PASS, including the existing host/path mismatch tests.

- [ ] **Step 5: Commit the canonical parser**

```bash
git add server/service/direct_upload.go server/service/direct_upload_test.go
git commit -m "fix(server): canonicalize pending upload URLs"
```

### Task 2: Prove Bootstrap and Handler Behavior

**Files:**
- Modify: `server/service/agent_bootstrap_test.go`
- Modify: `server/handler/task_test.go`

- [ ] **Step 1: Add an encoded pending bootstrap regression test**

Extend `TestBootstrapDownloadSigningValidatesFinalizedPendingUpload` with a second finalized record and encoded URL:

```go
encodedKey := "uploads/pending/user-1/upload-encoded/reference.png"
encodedPublicURL := "https://bucket.oss-cn-x.aliyuncs.com/" + encodedKey
if err := repo.PendingUploads().CreatePendingUpload(context.Background(), &model.PendingUpload{
	ID: "upload-encoded", UserID: task.UserID, Purpose: DirectUploadPurposeTaskReference,
	Key: encodedKey, PublicURL: encodedPublicURL,
	Status: model.PendingUploadStatusFinalized, ExpiresAt: time.Now().Add(time.Hour),
}); err != nil {
	t.Fatal(err)
}
encodedURL := "https://bucket.oss-cn-x.aliyuncs.com/uploads%2Fpending%2Fuser-1%2Fupload-encoded%2Freference.png?Expires=1&Signature=redacted"
if _, err := svc.signedDownloadURL(context.Background(), task, bootstrapDownloadSource{
	URL: encodedURL, AllowedPurposes: []string{DirectUploadPurposeTaskReference},
}, deadline); err != nil {
	t.Fatalf("encoded finalized pending input rejected: %v", err)
}
```

- [ ] **Step 2: Add a task-write rejection test**

Add `TestCreateTaskRejectsMalformedPendingReferenceWithoutCreatingTask` to `server/handler/task_test.go`:

```go
func TestCreateTaskRejectsMalformedPendingReferenceWithoutCreatingTask(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{
		ID: userID, Email: userID + "@example.com", Password: "hashed", InviteCode: "badpending",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID: projectID, UserID: userID, Platform: model.PlatformSeednote,
		Name: "Seednote", Status: model.ProjectStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Create(c)
	})
	body := fmt.Sprintf(`{
		"project_id": %q,
		"prompt": "test",
		"reference_image_url": %q
	}`, projectID, "https://cdn.example.com/uploads%2Fpending%2F"+userID)
	resp := postJSON(t, app, "/tasks", body)
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != fiber.StatusBadRequest || !bytes.Contains(responseBody, []byte("pending upload URL is invalid")) {
		t.Fatalf("status/body = %d/%s", resp.StatusCode, responseBody)
	}
	tasks, err := repo.Tasks().FindByUserID(ctx, userID, projectID, "", 0, 10)
	if err != nil || len(tasks) != 0 {
		t.Fatalf("tasks/error = %d/%v, want 0/nil", len(tasks), err)
	}
}
```

Add `fmt` to the test imports.

- [ ] **Step 3: Run the new tests and verify behavior**

Run:

```bash
go test ./server/service -run TestBootstrapDownloadSigningValidatesFinalizedPendingUpload -count=1
go test ./server/handler -run TestCreateTaskRejectsMalformedPendingReferenceWithoutCreatingTask -count=1
```

Expected: PASS. The first proves existing encoded task data becomes usable; the second proves malformed pending keys cannot reach bootstrap.

- [ ] **Step 4: Commit the boundary regressions**

```bash
git add server/service/agent_bootstrap_test.go server/handler/task_test.go
git commit -m "test(server): cover pending reference boundaries"
```

### Task 3: Verify the Complete Fix

**Files:**
- Verify only

- [ ] **Step 1: Run focused packages**

```bash
go test ./server/service ./server/handler -count=1
```

Expected: PASS.

- [ ] **Step 2: Run full Go verification**

```bash
go test ./...
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
```

Expected: all commands exit 0.

- [ ] **Step 3: Inspect the final diff**

```bash
git status --short
git diff HEAD~2 -- server/service/direct_upload.go server/service/direct_upload_test.go server/service/agent_bootstrap_test.go server/handler/task_test.go
```

Confirm that unrelated `claudecode` submodule state and Studio work are not staged or modified.
