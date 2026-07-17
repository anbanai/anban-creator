# Finalized Reference Assets Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace URL-backed reference images with immutable, repository-owned asset IDs finalized by OSS-internal copy before project, plan, task, or billing persistence.

**Architecture:** New direct uploads create short-lived `UploadSession` rows. Finalization claims a session, validates the staging object, promotes it with the existing conditional OSS copy primitive, and atomically records an immutable `Asset`; reference-image business records store only that asset ID. HTTP responses and runtimes resolve the asset through repositories, and URLs remain short-lived presentation data only.

**Tech Stack:** Go 1.24, Fiber v3, GORM, Alibaba Cloud OSS SDK, SQLite/MySQL, React 19, TypeScript, Vite 8, Vitest, Bun.

---

## File Map

- `server/model/upload_session.go`, `server/model/asset.go`: temporary upload capability, immutable asset identity, and response-only asset view.
- `server/repository/upload_session.go`, `server/repository/asset.go`, `server/repository/repository.go`: compare-and-swap claims, opaque owned lookup, and transactional finalization.
- `server/service/direct_upload.go`, `server/service/direct_upload_cleanup.go`: prepare, metadata validation, OSS-side promotion, idempotent database completion, and abandoned staging cleanup.
- `server/service/reference_asset.go`, `server/handler/reference_asset.go`: shared tagged selection, purpose/owner validation, error mapping, and signed presentation.
- Project/plan/task models, services, handlers, AI entry, and scheduler paths: asset-ID persistence before task billing.
- Agent bootstrap, local/Docker execution, and MCP profile output: repository-backed asset resolution and local materialization.
- `studio/src/types/asset.ts`, `studio/src/components/projects/ReferenceAssetUpload.tsx`, and project/plan/task forms: session/asset identity state separated from preview URLs.
- `server/migrations/20260717_finalized_reference_assets.sql`: forward-only schema cutover with no URL backfill.

### Task 1: Introduce Upload Session And Asset Persistence

**Files:**
- Create: `server/model/upload_session.go`
- Create: `server/model/asset.go`
- Create: `server/repository/upload_session.go`
- Create: `server/repository/upload_session_test.go`
- Create: `server/repository/asset.go`
- Create: `server/repository/asset_test.go`
- Modify: `server/model/model.go`
- Modify: `server/repository/repository.go`
- Modify: `server/repository/repository_test.go`

- [ ] **Step 1: Write failing repository tests against the existing test database helper**

```go
func TestUploadSessionFinalizationClaimCAS(t *testing.T) {
	ctx := context.Background()
	repo := New(setupTestDB(t))
	now := time.Date(2026, 7, 17, 9, 0, 0, 0, time.UTC)
	session := &model.UploadSession{
		ID: "session-1", UserID: "user-1", Purpose: "project_reference",
		StagingKey: "uploads/pending/user-1/session-1/ref.png",
		FileName: "ref.png", ContentType: "image/png", Size: 3,
		Status: model.UploadSessionPending, ExpiresAt: now.Add(time.Minute),
	}
	require.NoError(t, repo.UploadSessions().Create(ctx, session))

	won, err := repo.UploadSessions().ClaimFinalization(ctx, session.ID, "claim-1", now, now.Add(-time.Minute))
	require.NoError(t, err)
	require.True(t, won)
	won, err = repo.UploadSessions().ClaimFinalization(ctx, session.ID, "claim-2", now, now.Add(-time.Minute))
	require.NoError(t, err)
	require.False(t, won)
}

func TestAssetRepositoryFindOwnedByIDIsOpaque(t *testing.T) {
	ctx := context.Background()
	repo := New(setupTestDB(t))
	asset := &model.Asset{
		ID: "asset-1", UserID: "user-1", Purpose: "project_reference",
		StorageKey: "assets/users/user-1/asset-1/ref.png", FileName: "ref.png",
		ContentType: "image/png", Size: 3, ETag: "etag-1",
	}
	require.NoError(t, repo.Assets().Create(ctx, asset))
	got, err := repo.Assets().FindOwnedByID(ctx, asset.ID, asset.UserID)
	require.NoError(t, err)
	require.Equal(t, asset.StorageKey, got.StorageKey)
	_, foreignErr := repo.Assets().FindOwnedByID(ctx, asset.ID, "user-2")
	_, missingErr := repo.Assets().FindOwnedByID(ctx, "missing", "user-2")
	require.ErrorIs(t, foreignErr, model.ErrAssetNotFound)
	require.ErrorIs(t, missingErr, model.ErrAssetNotFound)
}
```

- [ ] **Step 2: Run the repository tests to verify RED**

Run: `go test ./server/repository -run 'TestUploadSessionFinalizationClaimCAS|TestAssetRepositoryFindOwnedByIDIsOpaque' -count=1`

Expected: FAIL because `UploadSession`, `Asset`, `UploadSessions()`, and `Assets()` do not exist.

- [ ] **Step 3: Add the exact persistence models and states**

```go
const (
	UploadSessionPending    = "pending"
	UploadSessionFinalizing = "finalizing"
	UploadSessionFinalized  = "finalized"
	UploadSessionExpiring   = "expiring"
	UploadSessionExpired    = "expired"
)

var (
	ErrUploadSessionNotFound      = errors.New("upload session not found")
	ErrUploadSessionClaimRejected = errors.New("upload session claim rejected")
	ErrAssetNotFound              = errors.New("asset not found")
)

type UploadSession struct {
	ID                    string     `gorm:"type:char(36);primaryKey" json:"id"`
	UserID                string     `gorm:"type:char(36);index;not null" json:"user_id"`
	Purpose               string     `gorm:"type:varchar(50);index;not null" json:"purpose"`
	StagingKey            string     `gorm:"type:varchar(500);uniqueIndex;not null" json:"staging_key"`
	FileName              string     `gorm:"type:varchar(255);not null" json:"file_name"`
	ContentType           string     `gorm:"type:varchar(120);not null" json:"content_type"`
	Size                  int64      `gorm:"not null" json:"size"`
	Status                string     `gorm:"type:varchar(20);index;not null;default:pending" json:"status"`
	ExpiresAt             time.Time  `gorm:"index;not null" json:"expires_at"`
	FinalizationToken     string     `gorm:"type:char(36);index" json:"-"`
	FinalizationClaimedAt *time.Time `gorm:"index" json:"-"`
	AssetID               string     `gorm:"type:char(36);index" json:"asset_id,omitempty"`
	FinalizedAt           *time.Time `json:"finalized_at,omitempty"`
	CleanupClaimID        string     `gorm:"type:char(36);index" json:"-"`
	CleanupClaimedAt      *time.Time `gorm:"index" json:"-"`
	ExpiredAt             *time.Time `json:"expired_at,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

type Asset struct {
	ID          string    `gorm:"type:char(36);primaryKey" json:"asset_id"`
	UserID      string    `gorm:"type:char(36);index;not null" json:"-"`
	Purpose     string    `gorm:"type:varchar(50);index;not null" json:"-"`
	StorageKey  string    `gorm:"type:varchar(500);uniqueIndex;not null" json:"-"`
	FileName    string    `gorm:"type:varchar(255);not null" json:"file_name"`
	ContentType string    `gorm:"type:varchar(120);not null" json:"content_type"`
	Size        int64     `gorm:"not null" json:"size"`
	ETag        string    `gorm:"type:varchar(255);not null" json:"-"`
	CreatedAt   time.Time `json:"created_at"`
}

type AssetView struct {
	AssetID           string    `json:"asset_id"`
	FileName          string    `json:"file_name"`
	ContentType       string    `json:"content_type"`
	Size              int64     `json:"size"`
	DownloadURL       string    `json:"download_url"`
	DownloadExpiresAt time.Time `json:"download_expires_at"`
}
```

Use table names `upload_sessions` and `assets`. Add both models to `model.AutoMigrate`; do not remove `PendingUpload` from migration until Task 10 has cut over every existing direct-upload caller.

- [ ] **Step 4: Add repository contracts and wire both normal and transaction repositories**

```go
type UploadSessionRepository interface {
	Create(context.Context, *model.UploadSession) error
	FindByID(context.Context, string) (*model.UploadSession, error)
	ClaimFinalization(context.Context, string, string, time.Time, time.Time) (bool, error)
	CompleteFinalization(context.Context, string, string, string, time.Time) (bool, error)
	ReleaseFinalization(context.Context, string, string) (bool, error)
	FindForCleanup(context.Context, time.Time, time.Time, int) ([]*model.UploadSession, error)
	ClaimExpiration(context.Context, string, string, time.Time, time.Time) (bool, error)
	CompleteExpiration(context.Context, string, string, time.Time) (bool, error)
	ReopenExpiration(context.Context, string, string) (bool, error)
}

type AssetRepository interface {
	Create(context.Context, *model.Asset) error
	FindByID(context.Context, string) (*model.Asset, error)
	FindOwnedByID(context.Context, string, string) (*model.Asset, error)
}
```

`ClaimFinalization` must update only an unexpired `pending` row or a `finalizing` row whose claim time is at or before `claimStaleBefore`. `CompleteFinalization` must match both session ID and token. `ReleaseFinalization` returns the same token-owned row to `pending`. Add `UploadSessions()` and `Assets()` fields/accessors to `Repository`, `repository`, and `txRepository`; use `newUploadSessionRepository(tx)` and `newAssetRepository(tx)` inside `newTxRepository` so asset creation plus session completion can share `WithTx`.

- [ ] **Step 5: Run model and repository tests to verify GREEN**

Run: `go test ./server/model ./server/repository -count=1`

Expected: PASS.

- [ ] **Step 6: Commit the persistence layer**

```bash
git add server/model/upload_session.go server/model/asset.go server/model/model.go server/repository/upload_session.go server/repository/upload_session_test.go server/repository/asset.go server/repository/asset_test.go server/repository/repository.go server/repository/repository_test.go
git commit -m "feat(storage): add upload sessions and immutable assets"
```

### Task 2: Finalize Upload Sessions Into Immutable Assets

**Files:**
- Modify: `server/service/direct_upload.go`
- Modify: `server/service/direct_upload_test.go`
- Modify: `server/service/direct_upload_cleanup.go`
- Modify: `server/handler/upload.go`
- Modify: `server/handler/upload_finalize.go`
- Modify: `server/handler/upload_prepare_test.go`
- Modify: `server/handler/upload_finalize_test.go`
- Modify: `server/handler/ai_entry.go`
- Modify: `server/handler/ai_entry_test.go`
- Modify: `server/handler/designer.go`
- Modify: `server/handler/designer_test.go`
- Modify: `server/handler/file.go`
- Modify: `server/handler/file_test.go`
- Modify: `server/handler/input_attachment.go`
- Modify: `server/handler/input_attachment_test.go`
- Modify: `server/handler/plan.go`
- Modify: `server/handler/plan_test.go`
- Modify: `server/handler/project.go`
- Modify: `server/handler/project_test.go`
- Modify: `server/handler/channel_test.go`
- Modify: `server/handler/task.go`
- Modify: `server/handler/task_test.go`
- Modify: `server/handler/template.go`
- Modify: `server/handler/template_test.go`
- Modify: `server/main.go`

- [ ] **Step 1: Write failing service tests for OSS-only, idempotent finalization**

```go
func TestFinalizeUploadSessionCreatesAssetWithoutReadingBody(t *testing.T) {
	now := time.Date(2026, 7, 17, 9, 0, 0, 0, time.UTC)
	session := uploadSessionFixture(now)
	repo := finalizedAssetTestRepository(t, session)
	store := newFakeFinalizationStore(map[string]*storage.ObjectInfo{
		session.StagingKey: {Key: session.StagingKey, Size: session.Size, ContentType: session.ContentType, ETag: "etag-1"},
	})

	asset, err := FinalizeUploadSession(context.Background(), store, repo, FinalizeUploadRequest{
		SessionID: session.ID, UserID: session.UserID,
		AllowedPurposes: []string{DirectUploadPurposeProjectReference}, Now: now,
	})
	require.NoError(t, err)
	require.Equal(t, session.ID, asset.ID)
	require.Equal(t, "assets/users/user-1/session-1/ref.png", asset.StorageKey)
	require.Equal(t, 0, store.readCalls)
	require.Equal(t, []promoteCall{{session.StagingKey, asset.StorageKey, "etag-1"}}, store.promoteCalls)
}

func TestFinalizeUploadSessionCompletesAfterCopyBeforeDatabaseFailure(t *testing.T) {
	now := time.Date(2026, 7, 17, 9, 0, 0, 0, time.UTC)
	session := uploadSessionFixture(now)
	repo := finalizedAssetTestRepository(t, session)
	repo.failFirstTransaction = true
	store := newFakeFinalizationStore(map[string]*storage.ObjectInfo{
		session.StagingKey: {Key: session.StagingKey, Size: session.Size, ContentType: session.ContentType, ETag: "etag-1"},
	})

	_, firstErr := FinalizeUploadSession(context.Background(), store, repo, finalizationRequest(session, now))
	require.Error(t, firstErr)
	asset, retryErr := FinalizeUploadSession(context.Background(), store, repo, finalizationRequest(session, now.Add(time.Second)))
	require.NoError(t, retryErr)
	require.Equal(t, session.ID, asset.ID)
	require.Len(t, store.promoteCalls, 1)
}
```

Add a table test named `TestFinalizeUploadSessionRejectsInvalidState` with explicit cases and expected sentinel errors for: expired session, foreign user, disallowed purpose, staging size mismatch, normalized content-type mismatch, missing ETag, source ETag precondition failure, and a pre-existing final object whose metadata differs from the prepared session.

- [ ] **Step 2: Run the service tests to verify RED**

Run: `go test ./server/service -run 'TestFinalizeUploadSession' -count=1`

Expected: FAIL because the finalization API and test helpers do not exist.

- [ ] **Step 3: Move prepare writes to `UploadSession` while preserving non-reference transport metadata**

```go
type DirectUploadPrepareResult struct {
	UploadSessionID   string            `json:"upload_session_id"`
	UploadID          string            `json:"upload_id"` // Existing attachment transport identity.
	StagingKey        string            `json:"key"`
	PreviewURL        string            `json:"preview_url"`
	PublicURL         string            `json:"public_url"` // Existing non-reference consumers only.
	UploadURL         string            `json:"upload_url,omitempty"`
	Method            string            `json:"method"`
	Headers           map[string]string `json:"headers"`
	Region            string            `json:"region"`
	Bucket            string            `json:"bucket"`
	Endpoint          string            `json:"endpoint"`
	STSAccessKeyID     string            `json:"sts_access_key_id"`
	STSAccessKeySecret string            `json:"sts_access_key_secret"`
	STSSecurityToken   string            `json:"sts_security_token"`
	ExpiresAt          time.Time         `json:"expires_at"`
	MaxSize            int64             `json:"max_size"`
}
```

`PrepareDirectUpload` creates `model.UploadSession` at `uploads/pending/{user_id}/{session_id}/{sanitized_filename}` and uses the existing exact-key STS policy. Keep the default at `15 * time.Minute`. `UploadID` and `UploadSessionID` carry the same UUID so current input-attachment, video, ecommerce, montage, designer, template, and avatar transports continue to identify the new session without becoming reference-image business fields.

- [ ] **Step 4: Implement claimed, idempotent finalization using the existing storage primitives**

```go
type FinalizeUploadRequest struct {
	SessionID       string
	UserID          string
	AllowedPurposes []string
	Now             time.Time
}

type DirectUploadFinalizationStorage interface {
	storage.ObjectStatProvider
	storage.ConditionalObjectPromoter
	Delete(context.Context, string) error
}

func finalizedAssetKey(session *model.UploadSession) string {
	return path.Join("assets/users", session.UserID, session.ID, session.FileName)
}

func FinalizeUploadSession(
	ctx context.Context,
	store DirectUploadFinalizationStorage,
	repo repository.Repository,
	req FinalizeUploadRequest,
) (*model.Asset, error)
```

Use `session.ID` as the asset ID. This keeps destination identity deterministic across a copy/database split failure while the separate tables retain distinct lifetime semantics. The function must: load and validate owner/purpose/expiry; return the linked asset for an already-finalized session; acquire a one-minute finalization lease; HEAD staging; validate exact size and normalized media type; call the existing `PromoteObject(staging, final, etag)`; HEAD final; and use `repo.WithTx` to create or verify the immutable asset and complete the token-matched session. Release the claim best-effort on pre-commit failure, then delete only the staging object best-effort after database success. Never call `Provider.Read`, `ReadObject`, or an application-host copy loop.

The existing `server/storage/oss.go` already implements `CopyObject` with `X-Oss-Copy-Source-If-Match` and `X-Oss-Forbid-Overwrite`; reuse it unchanged. Treat `storage.ErrObjectAlreadyExists` as a retry path only after final metadata exactly matches the proposed asset.

- [ ] **Step 5: Adapt non-reference direct-upload consumers to the new session and asset result**

Keep `EntryAttachment.UploadID`, caller-asserted keys, and URL-backed non-reference fields in this change. Rename the internal repository/service types from pending-upload to upload-session terminology, and have the current `ResolveDirectUploadAttachment`/URL finalizers call `FinalizeUploadSession`, returning the finalized `Asset.StorageKey` and URL for those consumers. Update AI entry, designer, file preview, attachment validation, video, montage, ecommerce, template thumbnail, avatar, and their test fakes to load `UploadSession`; do not make them persist reference-image URLs.

- [ ] **Step 6: Change cleanup to delete abandoned staging only**

`CleanupExpiredUploadSessions` claims expired `pending` rows and stale expired `finalizing` rows, deletes only `UploadSession.StagingKey`, and marks the session expired. It must never derive or delete an `assets/users/...` key. Add `TestCleanupExpiredUploadSessionsNeverDeletesFinalAsset` asserting the fake store receives exactly the staging key.

- [ ] **Step 7: Run direct-upload and handler tests to verify GREEN**

Run: `go test ./server/service ./server/handler ./server/storage -run 'Upload|Finalize|Cleanup|Attachment' -count=1`

Expected: PASS, including the existing conditional-copy storage tests.

- [ ] **Step 8: Commit session finalization**

```bash
git add server/service/direct_upload* server/handler server/main.go
git commit -m "feat(storage): finalize upload sessions as immutable assets"
```

### Task 3: Add The Shared Reference Asset Contract

**Files:**
- Create: `server/service/reference_asset.go`
- Create: `server/service/reference_asset_test.go`
- Create: `server/handler/reference_asset.go`
- Create: `server/handler/reference_asset_test.go`

- [ ] **Step 1: Write failing selection, ownership, and presentation tests**

```go
func TestReferenceImageSelectionValidate(t *testing.T) {
	tests := []struct {
		name string
		in   ReferenceImageSelection
		ok   bool
	}{
		{"asset", ReferenceImageSelection{AssetID: "asset-1"}, true},
		{"session", ReferenceImageSelection{UploadSessionID: "session-1"}, true},
		{"both", ReferenceImageSelection{AssetID: "asset-1", UploadSessionID: "session-1"}, false},
		{"empty", ReferenceImageSelection{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.ok, tt.in.Validate() == nil)
		})
	}
}

func TestReferenceAssetServicePresentSignsRepositoryKey(t *testing.T) {
	svc, store := referenceAssetServiceFixture(t, ownedAsset("asset-1", "user-1", DirectUploadPurposeProjectReference))
	view, err := svc.Present(context.Background(), "user-1", "asset-1", []string{DirectUploadPurposeProjectReference})
	require.NoError(t, err)
	require.Equal(t, "asset-1", view.AssetID)
	require.Equal(t, []string{"assets/users/user-1/asset-1/ref.png"}, store.signedKeys)
}
```

Also add `TestReferenceAssetServiceResolveSelection` with subtests for session finalization, existing owned asset reuse, wrong purpose, foreign asset, and missing asset. Assert wrong-purpose is 400-class, while foreign and missing IDs produce the same service-level forbidden sentinel.

- [ ] **Step 2: Run the tests to verify RED**

Run: `go test ./server/service ./server/handler -run 'ReferenceAsset|ReferenceImageSelection' -count=1`

Expected: FAIL because the shared contract does not exist.

- [ ] **Step 3: Implement tagged selection and service methods**

```go
type ReferenceImageSelection struct {
	AssetID        string `json:"asset_id,omitempty"`
	UploadSessionID string `json:"upload_session_id,omitempty"`
}

type ReferenceAssetService struct {
	repo  repository.Repository
	store storage.Provider
	now   func() time.Time
}

func (s *ReferenceAssetService) ResolveSelection(ctx context.Context, userID string, in ReferenceImageSelection, allowed []string) (string, error)
func (s *ReferenceAssetService) RequireOwned(ctx context.Context, userID, assetID string, allowed []string) (*model.Asset, error)
func (s *ReferenceAssetService) Present(ctx context.Context, userID, assetID string, allowed []string) (*model.AssetView, error)
```

`ResolveSelection` validates exactly one discriminator, finalizes a session or loads an existing asset, checks exact owner/purpose and image metadata, and returns only the asset ID. `Present` signs only `Asset.StorageKey` for `DefaultSignedURLTTL`; it never accepts a URL or key. Define service sentinels for invalid selection, forbidden identity, purpose mismatch, concurrent finalization, expired session, invalid metadata, and unavailable storage.

`NewReferenceAssetService` must verify or cast the configured provider to `DirectUploadFinalizationStorage` only on the upload-session branch. Existing-asset validation and preview signing continue to work through `storage.Provider`; a missing conditional promotion capability returns the unavailable sentinel instead of panicking.

- [ ] **Step 4: Implement one handler error mapper and legacy-field detector**

```go
func rejectLegacyReferenceImageURL(body []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return err
	}
	if _, exists := raw["reference_image_url"]; exists {
		return errors.New("reference_image_url is no longer supported; use reference_image")
	}
	return nil
}
```

Map malformed selection, purpose, and metadata to 400; opaque asset/session ownership to 403; active incompatible claim to 409; expiry to 410; and storage metadata/copy/signing dependency failures to 503. All project, plan, task, and preview handlers must call this same mapper.

- [ ] **Step 5: Run tests to verify GREEN**

Run: `go test ./server/service ./server/handler -run 'ReferenceAsset|ReferenceImageSelection|LegacyReference' -count=1`

Expected: PASS.

- [ ] **Step 6: Commit the shared contract**

```bash
git add server/service/reference_asset.go server/service/reference_asset_test.go server/handler/reference_asset.go server/handler/reference_asset_test.go
git commit -m "feat(storage): add reference asset selection contract"
```

### Task 4: Cut Projects Over To Asset Identity

**Files:**
- Modify: `server/model/project.go`
- Modify: `server/model/platform.go`
- Modify: `server/service/project.go`
- Modify: `server/service/project_test.go`
- Modify: `server/handler/project.go`
- Modify: `server/handler/project_test.go`
- Modify: `server/main.go`

- [ ] **Step 1: Write failing project request and persistence tests**

```go
func TestProjectCreateFinalizesReferenceSessionAndPersistsAssetID(t *testing.T) {
	app, repo := projectHandlerAppWithReferenceSession(t, "session-1", "user-1")
	resp := postJSON(t, app, "/projects", `{"platform":"seednote","name":"brand","reference_image":{"upload_session_id":"session-1"}}`)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	project := firstProject(t, repo)
	require.Equal(t, "session-1", project.ReferenceImageAssetID)
	require.NotContains(t, responseBody(t, resp), "reference_image_url")
}

func TestProjectRejectsLegacyReferenceImageURL(t *testing.T) {
	app, _ := projectHandlerApp(t)
	resp := postJSON(t, app, "/projects", `{"platform":"seednote","name":"brand","reference_image_url":"https://example.com/ref.png"}`)
	require.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
}

func TestProjectUpdateReferenceNullClearsAndOmissionPreserves(t *testing.T) {
	app, repo, project := projectHandlerAppWithAsset(t, "asset-1")
	require.Equal(t, fiber.StatusOK, putJSON(t, app, "/projects/"+project.ID, `{"name":"renamed"}`).StatusCode)
	require.Equal(t, "asset-1", reloadProject(t, repo, project.ID).ReferenceImageAssetID)
	require.Equal(t, fiber.StatusOK, putJSON(t, app, "/projects/"+project.ID, `{"reference_image":null}`).StatusCode)
	require.Empty(t, reloadProject(t, repo, project.ID).ReferenceImageAssetID)
}
```

- [ ] **Step 2: Run project tests to verify RED**

Run: `go test ./server/handler ./server/service -run 'Project.*Reference' -count=1`

Expected: FAIL because project requests and rows still use URLs.

- [ ] **Step 3: Replace project persistence and request fields**

```go
// model.Project
ReferenceImageAssetID string           `gorm:"type:char(36);index" json:"-"`
ReferenceImage        *AssetView       `gorm:"-" json:"reference_image,omitempty"`
ReferenceImageSet     bool             `gorm:"-" json:"-"`

// projectRequest
ReferenceImage *service.ReferenceImageSelection `json:"reference_image"`
ReferenceImageSet bool                           `json:"-"`
```

Set `ReferenceImageSet = hasJSONField(c.Body(), "reference_image")`. Reject any `reference_image_url` key before binding. For an explicit non-null selection, call `ResolveSelection` with only `project_reference`; for explicit null, use an empty asset ID; for omission, leave update state unchanged. `ProjectService.Update` assigns `ReferenceImageAssetID` only when `ReferenceImageSet` is true. Remove reference signing from `signProjectURLs`; avatar URL handling remains unchanged.

- [ ] **Step 4: Present asset views after project reads without persisting URLs**

For create/update/get/list, call `ReferenceAssetService.Present` using the authenticated owner and attach the returned transient `AssetView` to the response model. Empty asset ID yields `reference_image: null` or omission according to the existing response style. Remove `reference_image_url` from platform field metadata in `server/model/platform.go`.

- [ ] **Step 5: Run project tests to verify GREEN**

Run: `go test ./server/model ./server/service ./server/handler -run 'Project' -count=1`

Expected: PASS.

- [ ] **Step 6: Commit the project cutover**

```bash
git add server/model/project.go server/model/platform.go server/service/project.go server/service/project_test.go server/handler/project.go server/handler/project_test.go server/main.go
git commit -m "feat(projects): persist reference assets by identity"
```

### Task 5: Cut Plans, Tasks, And AI Entry Over Before Billing

**Files:**
- Modify: `server/model/plan.go`
- Modify: `server/model/task.go`
- Modify: `server/service/plan.go`
- Modify: `server/service/plan_test.go`
- Modify: `server/service/task.go`
- Modify: `server/service/task_test.go`
- Modify: `server/service/task_retry.go`
- Modify: `server/service/ai_entry.go`
- Modify: `server/service/ai_entry_test.go`
- Modify: `server/handler/plan.go`
- Modify: `server/handler/plan_test.go`
- Modify: `server/handler/task.go`
- Modify: `server/handler/task_test.go`
- Modify: `server/handler/ai_entry.go`
- Modify: `server/handler/ai_entry_test.go`
- Modify: `server/handler/video_split_contract.go`
- Modify: `server/scheduler/plan_checker_test.go`
- Modify: `server/main.go`

- [ ] **Step 1: Write failing billing-order and snapshot tests**

```go
func TestTaskCreateRejectsInvalidReferenceBeforePersistenceOrCredits(t *testing.T) {
	app, repo, user := taskHandlerAppWithForeignAsset(t, "asset-foreign")
	before := user.Balance
	resp := postJSON(t, app, "/tasks", `{"project_id":"project-1","type":"seednote","reference_image":{"asset_id":"asset-foreign"}}`)
	require.Equal(t, fiber.StatusForbidden, resp.StatusCode)
	count, err := repo.Tasks().CountByUserID(context.Background(), user.ID, "", "")
	require.NoError(t, err)
	require.Zero(t, count)
	require.Equal(t, before, reloadUser(t, repo, user.ID).Balance)
}

func TestTaskSnapshotStoresProjectReferenceAssetID(t *testing.T) {
	svc, project := taskServiceWithProjectAsset(t, "asset-project")
	tasks, err := svc.CreateManual(context.Background(), CreateManualParams{UserID: project.UserID, ProjectID: project.ID, Prompt: "write"})
	require.NoError(t, err)
	require.Equal(t, "asset-project", tasks[0].ProjectSnapshot.Data().ReferenceImageAssetID)
}

func TestCreateFromPlanCopiesPlanReferenceAssetID(t *testing.T) {
	svc, plan := taskServiceWithPlanAsset(t, "asset-plan")
	task, err := svc.CreateFromPlan(context.Background(), plan)
	require.NoError(t, err)
	require.Equal(t, "asset-plan", task.ReferenceImageAssetID)
}
```

Add `TestTaskCloneValidatesReferenceBeforeCreditDeduction` and `TestAIEntryUsesFinalizedAttachmentAssetAsTaskReference`; the first corrupts the source task asset ID and asserts no credit transaction, while the second supplies an image attachment with an upload-session ID and asserts the created article/moments task stores the linked asset ID instead of the attachment URL.

- [ ] **Step 2: Run plan/task/AI entry tests to verify RED**

Run: `go test ./server/handler ./server/service -run 'Plan.*Reference|Task.*Reference|AIEntry.*Reference|InvalidReferenceBefore' -count=1`

Expected: FAIL because these paths still copy and persist URLs.

- [ ] **Step 3: Replace all plan/task persisted reference fields**

```go
// model.Plan and model.Task
ReferenceImageAssetID string           `gorm:"type:char(36);index" json:"-"`
ReferenceImage        *AssetView       `gorm:"-" json:"reference_image,omitempty"`

// model.ProjectSnapshot
ReferenceImageAssetID string `json:"reference_image_asset_id,omitempty"`

type CreatePlanParams struct {
	// existing fields
	ReferenceImageAssetID string
}

type CreateManualParams struct {
	// existing fields
	ReferenceImageAssetID string
}

type UpdatePlanParams struct {
	// existing fields
	// nil = unchanged, pointer to empty string = clear.
	ReferenceImageAssetID *string
}
```

Change `SnapshotProject` and `ProjectFromSnapshot` to copy `ReferenceImageAssetID`. Change plan-to-task, manual create, retry, clone, and bulk-clone paths to copy immutable IDs. `TaskService.CreateManual`, `CreateFromPlan`, and `Clone` must load any non-empty task/project/plan asset through `repo.Assets().FindOwnedByID` and check allowed purpose before task creation and before `DeductForTaskCreation`. Scheduler-created tasks consume already persisted IDs but still receive the same service validation.

- [ ] **Step 4: Resolve handler selections before calling billed services**

Plan create/update accepts `reference_image` and permits `task_reference`. Task create accepts `reference_image` and permits `task_reference`. Both reject `reference_image_url`, preserve null/omitted update semantics, pass only `ReferenceImageAssetID` into services, and attach signed `AssetView` responses through updated `planAPIResponse`/`taskAPIResponse` helpers in `video_split_contract.go`. Keep URL finalization for video, ecommerce, montage, and generic input attachments isolated from this field.

- [ ] **Step 5: Derive AI-entry task references from attachment sessions**

Inject `ReferenceAssetService` into `AIEntryService`. For article/moments, select the first image attachment's `UploadID`, resolve it as `ReferenceImageSelection{UploadSessionID: uploadID}` with `ai_entry_attachment`, and set `CreateManualParams.ReferenceImageAssetID`. Do not call `firstImageAttachmentURL` for the task reference. Seednote continues to use its attachment list, and ecommerce/video behavior stays URL-backed under their existing non-reference contracts.

- [ ] **Step 6: Run plan/task/AI entry tests to verify GREEN**

Run: `go test ./server/model ./server/service ./server/handler ./server/scheduler -run 'Plan|Task|Clone|AIEntry|Reference' -count=1`

Expected: PASS.

- [ ] **Step 7: Commit the business cutover**

```bash
git add server/model/plan.go server/model/task.go server/service/plan* server/service/task* server/service/ai_entry* server/handler/plan* server/handler/task* server/handler/ai_entry* server/handler/video_split_contract.go server/scheduler/plan_checker_test.go server/main.go
git commit -m "feat(tasks): freeze finalized reference asset identities"
```

### Task 6: Resolve Reference Assets In Every Runtime And MCP Profile

**Files:**
- Modify: `server/service/reference_asset.go`
- Modify: `server/service/agent_bootstrap.go`
- Modify: `server/service/agent_bootstrap_test.go`
- Modify: `server/service/task_execution.go`
- Modify: `server/service/task_test.go`
- Modify: `server/agent/config_builder.go`
- Modify: `server/agent/config_builder_test.go`
- Modify: `server/agent/executor.go`
- Modify: `server/agent/executor_test.go`
- Modify: `server/agent/docker_executor.go`
- Modify: `server/mcp/tools.go`
- Modify: `server/mcp/tools_test.go`

- [ ] **Step 1: Write failing precedence and runtime parity tests**

```go
func TestEffectiveReferenceAssetID(t *testing.T) {
	task := &model.Task{ReferenceImageAssetID: "task-asset"}
	task.SetProjectSnapshot(model.ProjectSnapshot{ReferenceImageAssetID: "project-asset"})
	require.Equal(t, "task-asset", EffectiveReferenceAssetID(task))
	task.ReferenceImageAssetID = ""
	require.Equal(t, "project-asset", EffectiveReferenceAssetID(task))
	task.SkipReferenceImage = true
	require.Empty(t, EffectiveReferenceAssetID(task))
}

func TestBootstrapSignsOwnedReferenceAsset(t *testing.T) {
	svc, task, execution, project, store := bootstrapFixtureWithAsset(t, "asset-1")
	response, err := svc.buildResponse(context.Background(), execution, task, project, time.Now().Add(time.Minute))
	require.NoError(t, err)
	require.Contains(t, response.Files, BootstrapFile{Path: ".anban-creator/reference.png", DownloadURL: store.lastSignedURL, Mode: 0644})
	require.Equal(t, []string{"assets/users/user-1/asset-1/ref.png"}, store.signedKeys)
}

func TestMaterializeReferenceAssetUsesBoundedRepositoryKeyRead(t *testing.T) {
	store := &boundedAssetStore{objects: map[string][]byte{"assets/users/u/a/ref.png": []byte("image")}}
	asset := &model.Asset{StorageKey: "assets/users/u/a/ref.png", Size: 5}
	require.NoError(t, MaterializeReferenceAsset(context.Background(), store, t.TempDir(), asset))
	require.Equal(t, []string{asset.StorageKey}, store.readKeys)
	require.Zero(t, store.httpCalls)
}
```

Add bootstrap table cases for foreign owner and disallowed purpose. Add local and Docker executor tests asserting both receive the same `ExecutionOptions.ReferenceAsset`, write `.anban-creator/reference.png`, and fail closed rather than attempting HTTP when storage resolution fails.

- [ ] **Step 2: Run runtime tests to verify RED**

Run: `go test ./server/service ./server/agent ./server/mcp -run 'ReferenceAsset|EffectiveReference|BootstrapSigns|MaterializeReference' -count=1`

Expected: FAIL because runtimes still infer ownership from URLs.

- [ ] **Step 3: Centralize effective asset precedence and repository validation**

```go
func EffectiveReferenceAssetID(task *model.Task) string {
	if task == nil {
		return ""
	}
	if task.ReferenceImageAssetID != "" {
		return task.ReferenceImageAssetID
	}
	if task.SkipReferenceImage {
		return ""
	}
	return task.ProjectSnapshot.Data().ReferenceImageAssetID
}

type ExecutionOptions struct {
	Task           *model.Task
	Project        *model.Project
	ReferenceAsset *model.Asset
	// existing fields remain unchanged
}
```

`TaskService.HandleExecution` loads the effective asset from `repo.Assets()` with exact task user and allowed purposes before invoking local/Docker executors. `AgentBootstrapService` performs the same lookup independently before signing. Allowed runtime purposes are `project_reference`, `task_reference`, and `ai_entry_attachment`; the stored task/project precedence determines which ID is selected.

- [ ] **Step 4: Materialize by storage key and simplify app config**

Change `BuildAppConfig(..., hasReference bool)` so it sets `.anban-creator/reference.png` only from the boolean. Replace `DownloadReferenceImage(imageURL)` with:

```go
func MaterializeReferenceAsset(ctx context.Context, store storage.Provider, workDir string, asset *model.Asset) error {
	if asset == nil {
		return nil
	}
	data, err := storage.ReadObject(ctx, store, asset.StorageKey, maxReferenceImageBytes)
	if err != nil {
		return err
	}
	dir := filepath.Join(workDir, appconfig.ConfigDir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "reference.png"), data, 0600)
}
```

Local and Docker executors call this helper with `opts.ReferenceAsset`. Remove URL parsing, `IsOwnedURL`, direct HTTP fallback, and URL logging from the reference-image path. Kubernetes bootstrap signs only the repository key and writes the same local path.

- [ ] **Step 5: Remove asset URLs from MCP profile output**

In `server/mcp/tools.go`, remove `reference_image_url` from `resolved_profile` and `image_config`. When `EffectiveReferenceAssetID(task)` is non-empty, expose `reference_image_path: ".anban-creator/reference.png"`; otherwise omit it. Update MCP tests to assert no storage URL or key is returned.

- [ ] **Step 6: Run runtime and contract tests to verify GREEN**

Run: `go test ./server/agent ./server/service ./server/mcp -run 'Bootstrap|Executor|Reference|ProjectProfile' -count=1`

Expected: PASS.

- [ ] **Step 7: Commit runtime resolution**

```bash
git add server/service/reference_asset.go server/service/agent_bootstrap* server/service/task_execution.go server/service/task_test.go server/agent server/mcp/tools.go server/mcp/tools_test.go
git commit -m "feat(agent): resolve reference images from asset identity"
```

### Task 7: Sign Preview Downloads By Asset ID

**Files:**
- Modify: `server/handler/upload.go`
- Modify: `server/handler/upload_resolve_test.go`
- Modify: `server/router/router.go`
- Modify: `server/router/router_test.go`

- [ ] **Step 1: Write failing asset-only preview tests**

```go
func TestResolveAssetDownloadURLUsesRepositoryKey(t *testing.T) {
	app, store := uploadHandlerAppWithAsset(t, ownedAsset("asset-1", "user-1", DirectUploadPurposeProjectReference))
	resp := postJSON(t, app, "/uploads/resolve-asset-url", `{"asset_id":"asset-1"}`)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	require.Equal(t, []string{"assets/users/user-1/asset-1/ref.png"}, store.signedKeys)
}

func TestResolveAssetDownloadURLRejectsCallerKeyAndForeignAsset(t *testing.T) {
	app, _ := uploadHandlerAppWithAsset(t, ownedAsset("asset-1", "other-user", DirectUploadPurposeProjectReference))
	require.Equal(t, fiber.StatusBadRequest, postJSON(t, app, "/uploads/resolve-asset-url", `{"asset_id":"asset-1","key":"assets/users/other-user/asset-1/ref.png"}`).StatusCode)
	require.Equal(t, fiber.StatusForbidden, postJSON(t, app, "/uploads/resolve-asset-url", `{"asset_id":"asset-1"}`).StatusCode)
}
```

- [ ] **Step 2: Run preview tests to verify RED**

Run: `go test ./server/handler ./server/router -run 'ResolveAssetDownloadURL|ResolveAssetRoute' -count=1`

Expected: FAIL because the endpoint does not exist.

- [ ] **Step 3: Add the asset-only endpoint**

```go
type resolveAssetURLRequest struct {
	AssetID string `json:"asset_id"`
}

func (h *UploadHandler) ResolveAssetDownloadURL(c fiber.Ctx) error
```

Before binding, unmarshal into `map[string]json.RawMessage` and require exactly one key named `asset_id`; this rejects `key`, URL, `owner_type`, `owner_id`, and any other caller ownership assertion. Call `ReferenceAssetService.Present` with allowed reference purposes and return `model.AssetView`. Register `POST /api/v1/uploads/resolve-asset-url`. Keep `/uploads/resolve-download-url` only for existing non-reference attachment consumers.

- [ ] **Step 4: Run preview tests to verify GREEN**

Run: `go test ./server/handler ./server/router -run 'ResolveAssetDownloadURL|Upload' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit preview signing**

```bash
git add server/handler/upload.go server/handler/upload_resolve_test.go server/router/router.go server/router/router_test.go
git commit -m "feat(storage): sign reference previews from asset identity"
```

### Task 8: Add Studio Session/Asset Reference State

**Files:**
- Create: `studio/src/types/asset.ts`
- Create: `studio/src/lib/reference-image.ts`
- Create: `studio/src/lib/reference-image.test.ts`
- Create: `studio/src/components/projects/ReferenceAssetUpload.tsx`
- Create: `studio/src/components/projects/ReferenceAssetUpload.test.tsx`
- Modify: `studio/src/lib/direct-upload.ts`
- Modify: `studio/src/lib/direct-upload.test.ts`
- Keep unchanged for template thumbnails: `studio/src/components/projects/ReferenceImageUpload.tsx`
- Keep unchanged for template thumbnails: `studio/src/components/templates/TemplateCreateDialog.tsx`

- [ ] **Step 1: Write failing identity-separation tests**

```ts
it('submits a session without its preview URL', () => {
  const upload = { uploadSessionId: 'session-1', previewUrl: 'blob:preview' }
  expect(referenceSelectionFromUpload(upload)).toEqual({ upload_session_id: 'session-1' })
  expect(JSON.stringify(referenceSelectionFromUpload(upload))).not.toContain('preview')
})

it('submits an existing asset by asset id', () => {
  expect(referenceSelectionFromValue({
    asset_id: 'asset-1', file_name: 'ref.png', content_type: 'image/png', size: 3,
    download_url: 'https://signed.example/ref.png', download_expires_at: '2026-07-17T09:15:00Z',
  })).toEqual({ asset_id: 'asset-1' })
})
```

Component tests must assert upload completion calls `onChange({upload_session_id: ...})`, the `<img>` uses a component-local blob URL, clearing calls `onChange(null)`, and `onUploadingChange(true/false)` brackets the browser upload.

- [ ] **Step 2: Run Studio state tests to verify RED**

Run: `cd studio && bun run test -- src/lib/reference-image.test.ts src/lib/direct-upload.test.ts src/components/projects/ReferenceAssetUpload.test.tsx`

Expected: FAIL because the types, helpers, and component do not exist.

- [ ] **Step 3: Add discriminated identity and view types**

```ts
export type ReferenceImageSelection =
  | { asset_id: string; upload_session_id?: never }
  | { upload_session_id: string; asset_id?: never }

export interface ReferenceAssetView {
  asset_id: string
  file_name: string
  content_type: string
  size: number
  download_url: string
  download_expires_at: string
}

export type ReferenceImageValue = ReferenceImageSelection | ReferenceAssetView
```

Extend `PrepareUploadResponse` with `upload_session_id` and `preview_url`. Extend `UploadToOSSResult` with `uploadSessionId` and `previewUrl`, while retaining `uploadId`, `key`, and `publicUrl` for non-reference attachment transports. `referenceSelectionFromUpload` returns only the session discriminator.

- [ ] **Step 4: Build a dedicated business-reference component**

`ReferenceAssetUpload` accepts `value: ReferenceImageValue | null`, `onChange`, direct-upload purpose, and `onUploadingChange`. Existing assets preview `download_url`; new uploads preview a local `URL.createObjectURL(file)` and store only `{upload_session_id}`. Revoke blob URLs on replacement/unmount. Do not change the URL-based `ReferenceImageUpload` used by template thumbnail/style analysis; template thumbnails are outside the reference-asset contract.

- [ ] **Step 5: Run Studio state tests to verify GREEN**

Run: `cd studio && bun run test -- src/lib/reference-image.test.ts src/lib/direct-upload.test.ts src/components/projects/ReferenceAssetUpload.test.tsx`

Expected: PASS.

- [ ] **Step 6: Commit Studio identity state**

```bash
git add studio/src/types/asset.ts studio/src/lib/reference-image* studio/src/lib/direct-upload* studio/src/components/projects/ReferenceAssetUpload*
git commit -m "feat(studio): track references by session or asset"
```

### Task 9: Cut Studio Project, Plan, And Task Payloads Over

**Files:**
- Modify: `studio/src/types/project.ts`
- Modify: `studio/src/types/plan.ts`
- Modify: `studio/src/types/task.ts`
- Modify: `studio/src/lib/schemas.ts`
- Modify: `studio/src/pages/ProjectsPage.tsx`
- Modify: `studio/src/pages/ProjectsPage.test.tsx`
- Modify: `studio/src/pages/PlansPage.tsx`
- Modify: `studio/src/pages/PlansPage.test.tsx`
- Modify: `studio/src/pages/TasksPage.tsx`
- Modify: `studio/src/pages/TasksPage.test.tsx`
- Modify: `studio/src/pages/TaskDetailPage.tsx`
- Modify: `studio/src/pages/TaskDetailPage.test.tsx`
- Modify: `studio/src/components/ProjectCard.test.tsx`
- Modify: `studio/src/components/tasks/TaskContextSummary.test.tsx`
- Modify: `studio/src/components/tasks/TaskDetailsSheet.test.tsx`
- Modify: `studio/src/components/GlobalCommandPalette.actions.test.tsx`
- Modify: `studio/src/lib/command-center.test.ts`
- Modify: `studio/src/lib/studio-ux.test.ts`
- Modify: `studio/src/pages/DashboardPage.ai-entry.test.tsx`
- Modify: `studio/src/test/mocks/handlers.ts`

- [ ] **Step 1: Write failing payload tests with exact forbidden-field assertions**

```ts
it('creates a project with a reference session only', async () => {
  await selectUploadedReference('session-project')
  await submitProject()
  expect(projectsApi.create).toHaveBeenCalledWith(expect.objectContaining({
    reference_image: { upload_session_id: 'session-project' },
  }))
  expect(projectsApi.create.mock.calls[0][0]).not.toHaveProperty('reference_image_url')
})

it('updates a plan with an existing reference asset', async () => {
  await openPlanWithReference(assetView('asset-plan'))
  await submitPlan()
  expect(plansApi.update).toHaveBeenCalledWith(expect.any(String), expect.objectContaining({
    reference_image: { asset_id: 'asset-plan' },
  }))
})

it('creates a task without a URL-backed reference field', async () => {
  await selectUploadedTaskReference('session-task')
  await submitTask()
  const payload = tasksApi.create.mock.calls[0][0]
  expect(payload.reference_image).toEqual({ upload_session_id: 'session-task' })
  expect(payload).not.toHaveProperty('reference_image_url')
})
```

- [ ] **Step 2: Run page tests to verify RED**

Run: `cd studio && bun run test -- src/pages/ProjectsPage.test.tsx src/pages/PlansPage.test.tsx src/pages/TasksPage.test.tsx src/pages/TaskDetailPage.test.tsx`

Expected: FAIL because pages still store and submit URLs.

- [ ] **Step 3: Replace request, response, and schema fields**

```ts
const referenceImageSelectionSchema = z.union([
  z.object({ asset_id: z.string().uuid(), upload_session_id: z.never().optional() }),
  z.object({ upload_session_id: z.string().uuid(), asset_id: z.never().optional() }),
])

// Request types
reference_image?: ReferenceImageSelection | null

// Project/Plan/Task response types
reference_image?: ReferenceAssetView | null
```

`ProjectSnapshot` retains only `reference_image_asset_id?: string`. Remove every editable `reference_image_url` declaration and fixture. Project update sends `null` only when the user explicitly clears; untouched forms either omit the field or submit the current asset ID according to existing dirty-field behavior.

- [ ] **Step 4: Use `ReferenceAssetUpload` in operational forms**

Project, plan, and task form state stores `ReferenceImageValue | null`. Mutations call `referenceSelectionFromValue`; details render only `ReferenceAssetView.download_url`. Save is disabled only while the browser upload is active and becomes enabled when an upload session ID exists. On a server finalization error, keep the form and selection open and render the returned actionable message.

- [ ] **Step 5: Run all affected Studio tests to verify GREEN**

Run: `cd studio && bun run test -- src/pages/ProjectsPage.test.tsx src/pages/PlansPage.test.tsx src/pages/TasksPage.test.tsx src/pages/TaskDetailPage.test.tsx src/components/ProjectCard.test.tsx src/components/tasks/TaskContextSummary.test.tsx src/components/tasks/TaskDetailsSheet.test.tsx`

Expected: PASS.

- [ ] **Step 6: Commit Studio payload cutover**

```bash
git add studio/src
git commit -m "feat(studio): submit reference asset identities"
```

### Task 10: Remove Legacy URL Contracts, Add Forward Migration, And Verify

**Files:**
- Delete: `server/model/pending_upload.go`
- Delete: `server/model/pending_upload_test.go`
- Delete: `server/repository/pending_upload.go`
- Delete: `server/repository/pending_upload_test.go`
- Modify: `server/model/model.go`
- Modify: `server/agent/config_builder.go`
- Modify: `server/handler/plan.go`
- Modify: `server/handler/project.go`
- Modify: `server/handler/task.go`
- Modify: `server/handler/video_split_contract.go`
- Modify: `server/mcp/tools.go`
- Modify: `server/service/agent_bootstrap.go`
- Modify: `server/service/ai_entry.go`
- Modify: `server/service/plan.go`
- Modify: `server/service/project.go`
- Modify: `server/service/task.go`
- Modify: `server/service/task_retry.go`
- Modify: `server/agent/config_builder_test.go`
- Modify: `server/agent/executor_test.go`
- Modify: `server/handler/plan_test.go`
- Modify: `server/handler/project_test.go`
- Modify: `server/handler/task_test.go`
- Modify: `server/handler/upload_resolve_test.go`
- Modify: `server/mcp/tools_test.go`
- Modify: `server/model/task_execution_test.go`
- Modify: `server/scheduler/plan_checker_test.go`
- Modify: `server/service/agent_bootstrap_test.go`
- Modify: `server/service/ai_entry_test.go`
- Modify: `server/service/plan_test.go`
- Modify: `server/service/project_test.go`
- Modify: `server/service/task_test.go`
- Modify: `studio/src/components/GlobalCommandPalette.actions.test.tsx`
- Modify: `studio/src/components/ProjectCard.test.tsx`
- Modify: `studio/src/components/tasks/TaskContextSummary.test.tsx`
- Modify: `studio/src/components/tasks/TaskDetailsSheet.test.tsx`
- Modify: `studio/src/lib/command-center.test.ts`
- Modify: `studio/src/lib/studio-ux.test.ts`
- Modify: `studio/src/pages/DashboardPage.ai-entry.test.tsx`
- Modify: `studio/src/pages/PlansPage.test.tsx`
- Modify: `studio/src/pages/ProjectsPage.test.tsx`
- Modify: `studio/src/pages/TaskDetailPage.test.tsx`
- Modify: `studio/src/pages/TasksPage.test.tsx`
- Modify: `studio/src/test/mocks/handlers.ts`
- Create: `server/migrations/20260717_finalized_reference_assets.sql`
- Modify: `server/migrations/migrations_test.go`
- Modify: API/config documentation that mentions the removed reference request field

- [ ] **Step 1: Add failing source-contract and migration tests**

```go
func TestReferenceImageURLContractRemoved(t *testing.T) {
	root := repositoryRoot(t)
	for _, dir := range []string{"server/model", "server/service", "server/handler", "server/agent", "server/mcp"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry fs.DirEntry, err error) error {
			require.NoError(t, err)
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			raw, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			require.NotContains(t, string(raw), "ReferenceImageURL", path)
			require.NotContains(t, string(raw), `json:"reference_image_url`, path)
			return nil
		})
		require.NoError(t, err)
	}
}
```

Add migration assertions that the SQL creates `upload_sessions` and `assets`, adds `reference_image_asset_id` to `projects`, `plans`, and `tasks`, drops their `reference_image_url` columns, and contains no `INSERT ... SELECT`, URL parsing, or historical backfill.

- [ ] **Step 2: Run removal tests to verify RED**

Run: `go test ./server/migrations ./server/model -run 'ReferenceImageURLContractRemoved|FinalizedReferenceAssetsMigration' -count=1`

Expected: FAIL with remaining production URL fields and missing migration SQL.

- [ ] **Step 3: Remove old pending and URL code, then add the forward-only schema**

Delete the old pending-upload models/repositories after all callers use `UploadSession`. Remove production `ReferenceImageURL`, `reference_image_url`, URL validation, response key enrichment, and reference-specific `IsOwnedURL` branches. Keep generic non-reference attachment URL logic. The migration must create both new tables with the same indexes/unique constraints as GORM, add nullable indexed asset-ID columns, and drop URL columns without backfill. Old snapshot JSON may still contain historical keys in the database, but new code neither reads nor interprets them.

Use this migration shape, expanding index names only where the target database requires globally unique names:

```sql
CREATE TABLE `upload_sessions` (
  `id` char(36) NOT NULL,
  `user_id` char(36) NOT NULL,
  `purpose` varchar(50) NOT NULL,
  `staging_key` varchar(500) NOT NULL,
  `file_name` varchar(255) NOT NULL,
  `content_type` varchar(120) NOT NULL,
  `size` bigint NOT NULL,
  `status` varchar(20) NOT NULL DEFAULT 'pending',
  `expires_at` datetime(3) NOT NULL,
  `finalization_token` char(36) DEFAULT NULL,
  `finalization_claimed_at` datetime(3) DEFAULT NULL,
  `asset_id` char(36) DEFAULT NULL,
  `finalized_at` datetime(3) DEFAULT NULL,
  `cleanup_claim_id` char(36) DEFAULT NULL,
  `cleanup_claimed_at` datetime(3) DEFAULT NULL,
  `expired_at` datetime(3) DEFAULT NULL,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uidx_upload_sessions_staging_key` (`staging_key`),
  KEY `idx_upload_sessions_user_id` (`user_id`),
  KEY `idx_upload_sessions_purpose` (`purpose`),
  KEY `idx_upload_sessions_status` (`status`),
  KEY `idx_upload_sessions_expires_at` (`expires_at`),
  KEY `idx_upload_sessions_finalization_token` (`finalization_token`),
  KEY `idx_upload_sessions_finalization_claimed_at` (`finalization_claimed_at`),
  KEY `idx_upload_sessions_asset_id` (`asset_id`),
  KEY `idx_upload_sessions_cleanup_claim_id` (`cleanup_claim_id`),
  KEY `idx_upload_sessions_cleanup_claimed_at` (`cleanup_claimed_at`)
);

CREATE TABLE `assets` (
  `id` char(36) NOT NULL,
  `user_id` char(36) NOT NULL,
  `purpose` varchar(50) NOT NULL,
  `storage_key` varchar(500) NOT NULL,
  `file_name` varchar(255) NOT NULL,
  `content_type` varchar(120) NOT NULL,
  `size` bigint NOT NULL,
  `etag` varchar(255) NOT NULL,
  `created_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uidx_assets_storage_key` (`storage_key`),
  KEY `idx_assets_user_id` (`user_id`),
  KEY `idx_assets_purpose` (`purpose`)
);

ALTER TABLE `projects` ADD COLUMN `reference_image_asset_id` char(36) DEFAULT NULL, ADD KEY `idx_projects_reference_image_asset_id` (`reference_image_asset_id`), DROP COLUMN `reference_image_url`;
ALTER TABLE `plans` ADD COLUMN `reference_image_asset_id` char(36) DEFAULT NULL, ADD KEY `idx_plans_reference_image_asset_id` (`reference_image_asset_id`), DROP COLUMN `reference_image_url`;
ALTER TABLE `tasks` ADD COLUMN `reference_image_asset_id` char(36) DEFAULT NULL, ADD KEY `idx_tasks_reference_image_asset_id` (`reference_image_asset_id`), DROP COLUMN `reference_image_url`;
DROP TABLE `pending_uploads`;
```

- [ ] **Step 4: Run deterministic removal scans**

```bash
rg -n 'ReferenceImageURL|json:"reference_image_url|reference_image_url' server --glob '*.go' --glob '!**/*_test.go'
rg -n 'reference_image_url' studio/src --glob '*.ts' --glob '*.tsx' --glob '!**/*.test.*'
rg -n 'PendingUpload|pending_uploads' server --glob '*.go'
```

Expected: no production reference URL hits; no old pending-upload type/table hits. Test names that explicitly verify legacy rejection may remain.

- [ ] **Step 5: Run focused backend and Studio suites**

```bash
go test ./server/model ./server/repository ./server/storage ./server/service ./server/handler ./server/agent ./server/mcp ./server/router ./server/scheduler -count=1
cd studio && bun run test
```

Expected: PASS.

- [ ] **Step 6: Run full verification**

When all plugin submodules are available:

```bash
go test ./... -count=1
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
cd studio && bun run build
git diff --check
```

Expected: every command exits 0. If GitHub is still unavailable and missing submodule files prevent `go test ./...`, record that external limitation while still requiring all available core package tests and both builds to pass.

- [ ] **Step 7: Review the security and ordering invariants**

Confirm from the final diff and tests that: reference APIs accept only asset/session IDs; staging credentials cannot write `assets/`; no application host proxies copy bytes; foreign and missing identities are opaque; project/plan/task writes and credit deductions are not reached after finalization failure; all runtimes use the same effective ID precedence; preview signing reads keys only from `Asset`; cleanup cannot delete finalized assets; and no plugin distribution file changed.

- [ ] **Step 8: Commit the completed cutover**

```bash
git add server studio docs
git commit -m "feat(storage): complete finalized reference asset cutover"
```
