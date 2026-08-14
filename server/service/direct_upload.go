package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	credential "github.com/aliyun/credentials-go/credentials"
	"github.com/google/uuid"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
)

const (
	DirectUploadPurposeProjectReference  = "project_reference"
	DirectUploadPurposeTaskReference     = "task_reference"
	DirectUploadPurposeEcommercePhoto    = "ecommerce_product_photo"
	DirectUploadPurposeMontageAsset      = "montage_asset"
	DirectUploadPurposeAIEntryAttachment = "ai_entry_attachment"
	DirectUploadPurposeTaskArtifact      = "task_artifact"

	defaultDirectUploadTTLSeconds = 15 * 60
	uploadSessionCleanupLease     = 5 * time.Minute
	uploadFinalizationLease       = time.Minute
	uploadFinalizationTimeout     = 45 * time.Second
	uploadFinalizationCleanupTTL  = 5 * time.Second
	// Task-artifact staging remains cleanup-eligible for one hour after expiry
	// so scheduler sweeps can remove objects recreated by delayed signed PUTs.
	taskArtifactCleanupGrace      = time.Hour
	taskArtifactCleanupRetryDelay = 30 * time.Minute
)

var (
	ErrUploadSessionNotFound      = model.ErrUploadSessionNotFound
	ErrUploadSessionInvalidURL    = errors.New("upload session URL is invalid")
	ErrUploadSessionAccessDenied  = errors.New("upload session access denied")
	ErrUploadSessionExpired       = errors.New("upload session has expired")
	ErrUploadSessionStateConflict = errors.New("upload session state conflict")
	ErrUploadSessionObjectInvalid = errors.New("upload session object metadata is invalid")
	ErrUploadSessionUnavailable   = errors.New("upload session dependency is unavailable")
)

type directUploadStorage interface {
	Name() string
	UploadURL(ctx context.Context, key string, contentType string, expirySeconds int) (string, error)
	DownloadURL(ctx context.Context, key string, expirySeconds int) (string, error)
	GetURL(key string) string
	Delete(ctx context.Context, key string) error
}

type DirectUploadPrepareRequest struct {
	UserID      string `json:"-"`
	Purpose     string `json:"purpose"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

type DirectUploadPrepareResult struct {
	UploadRequired     bool              `json:"upload_required"`
	ETag               string            `json:"etag,omitempty"`
	UploadSessionID    string            `json:"upload_session_id"`
	UploadID           string            `json:"upload_id"`
	StagingKey         string            `json:"key"`
	Key                string            `json:"-"`
	PreviewURL         string            `json:"preview_url"`
	PublicURL          string            `json:"public_url"`
	UploadURL          string            `json:"upload_url,omitempty"`
	Method             string            `json:"method"`
	Headers            map[string]string `json:"headers"`
	Region             string            `json:"region"`
	Bucket             string            `json:"bucket"`
	Endpoint           string            `json:"endpoint"`
	STSAccessKeyID     string            `json:"sts_access_key_id"`
	STSAccessKeySecret string            `json:"sts_access_key_secret"`
	STSSecurityToken   string            `json:"sts_security_token"`
	ExpiresAt          time.Time         `json:"expires_at"`
	MaxSize            int64             `json:"max_size"`
}

type VerifiedDirectUpload struct {
	UploadID    string
	Key         string
	FileName    string
	ContentType string
	Size        int64
	Purpose     string
}

type DirectUploadFinalizationStorage interface {
	storage.ObjectStatProvider
	storage.ConditionalObjectPromoter
	Delete(ctx context.Context, key string) error
	GetURL(key string) string
}

type FinalizeUploadRequest struct {
	SessionID       string
	UserID          string
	AllowedPurposes []string
	Now             time.Time
}

type UploadCredentialRequest struct {
	RoleArn     string
	SessionName string
	Policy      string
	Expires     int
	STSEndpoint string
}

type UploadCredential struct {
	AccessKeyID     string
	AccessKeySecret string
	SecurityToken   string
	ExpiresAt       time.Time
}

type UploadCredentialIssuer interface {
	IssueUploadCredential(ctx context.Context, req UploadCredentialRequest) (*UploadCredential, error)
}

type StaticUploadCredentialIssuer func(context.Context, UploadCredentialRequest) (*UploadCredential, error)

func (fn StaticUploadCredentialIssuer) IssueUploadCredential(ctx context.Context, req UploadCredentialRequest) (*UploadCredential, error) {
	return fn(ctx, req)
}

type DirectUploadConfig struct {
	Storage          config.StorageConfig
	CredentialIssuer UploadCredentialIssuer
	Now              func() time.Time
}

type directUploadPurposePolicy struct {
	maxSize    int64
	maxSizeFor func(contentType, ext string) int64
	validate   func(contentType, ext string) bool
}

var directUploadPolicies = map[string]directUploadPurposePolicy{
	DirectUploadPurposeProjectReference:  {maxSize: maxUploadImageBytes, validate: isDirectUploadImage},
	DirectUploadPurposeTaskReference:     {maxSize: maxUploadImageBytes, validate: isDirectUploadImage},
	DirectUploadPurposeEcommercePhoto:    {maxSize: maxUploadImageBytes, validate: isDirectUploadImage},
	DirectUploadPurposeMontageAsset:      {maxSize: 50 * 1024 * 1024, validate: isDirectUploadMontageAsset},
	DirectUploadPurposeAIEntryAttachment: {maxSize: 50 * 1024 * 1024, maxSizeFor: aiEntryAttachmentMaxSize, validate: isDirectUploadAIEntryAttachment},
}

const maxUploadImageBytes = 10 * 1024 * 1024

var unsafeFilenameRunes = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func PrepareDirectUpload(ctx context.Context, store directUploadStorage, repo repository.UploadSessionRepository, cfg DirectUploadConfig, req DirectUploadPrepareRequest) (*DirectUploadPrepareResult, error) {
	if store == nil {
		return nil, fmt.Errorf("storage provider is not available")
	}
	if store.Name() != "oss" {
		return nil, fmt.Errorf("direct uploads require OSS storage")
	}
	if repo == nil {
		return nil, fmt.Errorf("upload session repository is not available")
	}
	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		return nil, fmt.Errorf("user_id is required")
	}
	purpose := strings.TrimSpace(req.Purpose)
	policy, ok := directUploadPolicies[purpose]
	if !ok {
		return nil, fmt.Errorf("unsupported upload purpose %q", purpose)
	}
	if req.Size <= 0 {
		return nil, fmt.Errorf("file size is required")
	}
	filename := sanitizeUploadFilename(req.Filename)
	if filename == "" {
		return nil, fmt.Errorf("filename is required")
	}
	ext := strings.ToLower(filepath.Ext(filename))
	contentType := strings.TrimSpace(req.ContentType)
	if contentType == "" || strings.EqualFold(contentType, "application/octet-stream") {
		if inferred := contentTypeForUploadExt(ext); inferred != "" && inferred != "application/octet-stream" {
			contentType = inferred
		} else if contentType == "" {
			contentType = "application/octet-stream"
		}
	}
	if !policy.validate(contentType, ext) {
		return nil, fmt.Errorf("content type %q is not allowed for %s uploads", contentType, purpose)
	}
	maxSize := policy.maxSize
	if policy.maxSizeFor != nil {
		maxSize = policy.maxSizeFor(contentType, ext)
	}
	if maxSize <= 0 {
		return nil, fmt.Errorf("content type %q is not allowed for %s uploads", contentType, purpose)
	}
	if req.Size > maxSize {
		return nil, fmt.Errorf("file size exceeds the %d MB limit", maxSize/(1024*1024))
	}
	if ext == "" {
		ext = canonicalDirectUploadExtension(contentType)
	}
	if ext == "" {
		ext = ".bin"
	}
	if strings.HasSuffix(strings.ToLower(filename), ext) {
		filename = strings.TrimSuffix(filename, filepath.Ext(filename)) + ext
	} else {
		filename += ext
	}

	now := time.Now
	if cfg.Now != nil {
		now = cfg.Now
	}
	expiresSeconds := cfg.Storage.DirectUploadExpiresSeconds
	if expiresSeconds <= 0 {
		expiresSeconds = defaultDirectUploadTTLSeconds
	}
	uploadID := uuid.NewString()
	key := path.Join("uploads/pending", userID, uploadID, filename)
	expiresAt := now().Add(time.Duration(expiresSeconds) * time.Second)
	publicURL := store.GetURL(key)
	uploadURL, err := store.UploadURL(ctx, key, contentType, expiresSeconds)
	if err != nil {
		return nil, fmt.Errorf("create signed upload URL: %w", err)
	}

	issuer := cfg.CredentialIssuer
	if issuer == nil {
		issuer = NewAliyunUploadCredentialIssuer(cfg.Storage)
	}
	cred, err := issuer.IssueUploadCredential(ctx, UploadCredentialRequest{
		RoleArn:     cfg.Storage.STSRoleArn,
		SessionName: firstNonEmptyString(cfg.Storage.STSSessionName, "studio-direct-upload"),
		Policy:      directUploadPolicyJSON(cfg.Storage.BucketName, key),
		Expires:     expiresSeconds,
		STSEndpoint: cfg.Storage.STSEndpoint,
	})
	if err != nil {
		return nil, fmt.Errorf("issue upload credential: %w", err)
	}
	if cred == nil {
		return nil, fmt.Errorf("upload credential issuer returned no credential")
	}
	if cred.ExpiresAt.IsZero() {
		cred.ExpiresAt = expiresAt
	}
	session := &model.UploadSession{
		ID:          uploadID,
		UserID:      userID,
		Purpose:     purpose,
		StagingKey:  key,
		FileName:    filename,
		ContentType: contentType,
		Size:        req.Size,
		Status:      model.UploadSessionPending,
		ExpiresAt:   expiresAt,
	}
	if err := repo.Create(ctx, session); err != nil {
		return nil, fmt.Errorf("record upload session: %w", err)
	}
	return &DirectUploadPrepareResult{
		UploadRequired:     true,
		UploadSessionID:    uploadID,
		UploadID:           uploadID,
		StagingKey:         key,
		Key:                key,
		PreviewURL:         publicURL,
		PublicURL:          publicURL,
		UploadURL:          uploadURL,
		Method:             "PUT",
		Headers:            map[string]string{"Content-Type": contentType},
		Region:             ossBrowserRegion(cfg.Storage.Region, cfg.Storage.Endpoint),
		Bucket:             cfg.Storage.BucketName,
		Endpoint:           cfg.Storage.Endpoint,
		STSAccessKeyID:     cred.AccessKeyID,
		STSAccessKeySecret: cred.AccessKeySecret,
		STSSecurityToken:   cred.SecurityToken,
		ExpiresAt:          minTime(expiresAt, cred.ExpiresAt),
		MaxSize:            maxSize,
	}, nil
}

func FinalizeUploadSession(ctx context.Context, store DirectUploadFinalizationStorage, repo repository.Repository, req FinalizeUploadRequest) (*model.Asset, error) {
	if store == nil || repo == nil {
		return nil, ErrUploadSessionUnavailable
	}
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.UserID = strings.TrimSpace(req.UserID)
	if req.SessionID == "" || req.UserID == "" {
		return nil, ErrUploadSessionAccessDenied
	}
	if req.Now.IsZero() {
		req.Now = time.Now()
	}
	operationCtx, operationCancel := context.WithTimeout(ctx, uploadFinalizationTimeout)
	defer operationCancel()
	ctx = operationCtx

	session, err := repo.UploadSessions().FindByID(ctx, req.SessionID)
	if err != nil {
		if errors.Is(err, model.ErrUploadSessionNotFound) {
			return nil, ErrUploadSessionAccessDenied
		}
		return nil, fmt.Errorf("%w: find upload session: %v", ErrUploadSessionUnavailable, err)
	}
	if session.UserID != req.UserID || !directUploadPurposeAllowed(session.Purpose, req.AllowedPurposes) {
		return nil, ErrUploadSessionAccessDenied
	}
	if session.Status == model.UploadSessionFinalized {
		return loadFinalizedUploadSessionAsset(ctx, store, repo, session)
	}

	finalKey, err := finalizedUploadSessionKey(session)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUploadSessionObjectInvalid, err)
	}

	finalInfo, statErr := store.StatObject(ctx, finalKey)
	finalExists := statErr == nil
	switch {
	case finalExists:
		if err := validateUploadSessionObjectInfo(session, finalKey, finalInfo); err != nil {
			return nil, err
		}
		if strings.TrimSpace(session.FinalizationETag) == "" && strings.TrimSpace(session.PromotionSourceETag) == "" {
			return nil, fmt.Errorf("%w: final object has no persisted fingerprint", ErrUploadSessionObjectInvalid)
		}
		if strings.TrimSpace(session.FinalizationETag) != "" {
			if err := validateFinalUploadSessionObject(session, finalKey, session.FinalizationETag, finalInfo); err != nil {
				return nil, err
			}
		}
	case errors.Is(statErr, storage.ErrObjectNotFound):
	default:
		return nil, fmt.Errorf("%w: stat final upload object: %v", ErrUploadSessionUnavailable, statErr)
	}

	token := uuid.NewString()
	claimStaleBefore := req.Now.Add(-uploadFinalizationLease)
	var claimed bool
	if finalExists && !(session.Status == model.UploadSessionPending && session.ExpiresAt.After(req.Now)) {
		claimed, err = repo.UploadSessions().ClaimFinalizationRecovery(ctx, session.ID, token, req.Now, claimStaleBefore)
	} else {
		if session.Status == model.UploadSessionExpired || session.Status == model.UploadSessionExpiring || !session.ExpiresAt.After(req.Now) {
			return nil, ErrUploadSessionExpired
		}
		if session.Status != model.UploadSessionPending && session.Status != model.UploadSessionFinalizing {
			return nil, ErrUploadSessionStateConflict
		}
		claimed, err = repo.UploadSessions().ClaimFinalization(ctx, session.ID, token, req.Now, claimStaleBefore)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: claim upload finalization: %v", ErrUploadSessionUnavailable, err)
	}
	if !claimed {
		latest, findErr := repo.UploadSessions().FindByID(ctx, session.ID)
		if findErr == nil && latest.Status == model.UploadSessionFinalized {
			return loadFinalizedUploadSessionAsset(ctx, store, repo, latest)
		}
		return nil, ErrUploadSessionStateConflict
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		releaseCtx, releaseCancel := context.WithTimeout(context.WithoutCancel(ctx), uploadFinalizationCleanupTTL)
		defer releaseCancel()
		_, _ = repo.UploadSessions().ReleaseFinalization(releaseCtx, session.ID, token)
	}()
	if finalExists && strings.TrimSpace(session.FinalizationETag) == "" {
		stagingInfo, err := statUploadSessionObject(ctx, store, session.StagingKey, "staging recovery")
		if err != nil {
			return nil, err
		}
		if err := validateUploadSessionObjectInfo(session, session.StagingKey, stagingInfo); err != nil {
			return nil, err
		}
		if strings.TrimSpace(stagingInfo.ETag) == "" || strings.TrimSpace(stagingInfo.ETag) != strings.TrimSpace(session.PromotionSourceETag) {
			return nil, fmt.Errorf("%w: staging object changed after promotion", ErrUploadSessionObjectInvalid)
		}
		targetETag := strings.TrimSpace(finalInfo.ETag)
		if targetETag == "" {
			return nil, fmt.Errorf("%w: final object has no ETag", ErrUploadSessionObjectInvalid)
		}
		recorded, recordErr := repo.UploadSessions().RecordFinalizationETag(ctx, session.ID, token, targetETag)
		if recordErr != nil {
			return nil, fmt.Errorf("%w: record upload fingerprint: %v", ErrUploadSessionUnavailable, recordErr)
		}
		if !recorded {
			return nil, ErrUploadSessionStateConflict
		}
		session.FinalizationETag = targetETag
	}

	if !finalExists {
		stagingInfo, err := statUploadSessionObject(ctx, store, session.StagingKey, "staging")
		if err != nil {
			return nil, err
		}
		if err := validateUploadSessionObjectInfo(session, session.StagingKey, stagingInfo); err != nil {
			return nil, err
		}
		verifiedETag := strings.TrimSpace(stagingInfo.ETag)
		if verifiedETag == "" {
			return nil, fmt.Errorf("%w: staging object has no ETag", ErrUploadSessionObjectInvalid)
		}
		recorded, recordErr := repo.UploadSessions().RecordPromotionSourceETag(ctx, session.ID, token, verifiedETag)
		if recordErr != nil {
			return nil, fmt.Errorf("%w: record promotion source fingerprint: %v", ErrUploadSessionUnavailable, recordErr)
		}
		if !recorded {
			return nil, ErrUploadSessionStateConflict
		}
		session.PromotionSourceETag = verifiedETag
		promotedInfo, promoteErr := store.PromoteObject(ctx, session.StagingKey, finalKey, verifiedETag)
		if promoteErr != nil {
			switch {
			case errors.Is(promoteErr, storage.ErrPromotionPreconditionFailed):
				return nil, fmt.Errorf("%w: staging object changed during finalization", ErrUploadSessionObjectInvalid)
			case errors.Is(promoteErr, storage.ErrObjectAlreadyExists):
			default:
				return nil, fmt.Errorf("%w: promote upload session object: %v", ErrUploadSessionUnavailable, promoteErr)
			}
		}
		finalInfo, err = statUploadSessionObject(ctx, store, finalKey, "final")
		if err != nil {
			return nil, err
		}
		targetETag := strings.TrimSpace(finalInfo.ETag)
		if promoteErr == nil {
			if promotedInfo == nil || promotedInfo.Key != finalKey || strings.TrimSpace(promotedInfo.ETag) == "" {
				return nil, fmt.Errorf("%w: promotion target identity is unavailable", ErrUploadSessionObjectInvalid)
			}
			targetETag = strings.TrimSpace(promotedInfo.ETag)
		}
		if err := validateFinalUploadSessionObject(session, finalKey, targetETag, finalInfo); err != nil {
			return nil, err
		}
		recorded, recordErr = repo.UploadSessions().RecordFinalizationETag(ctx, session.ID, token, targetETag)
		if recordErr != nil {
			return nil, fmt.Errorf("%w: record upload fingerprint: %v", ErrUploadSessionUnavailable, recordErr)
		}
		if !recorded {
			return nil, ErrUploadSessionStateConflict
		}
		session.FinalizationETag = targetETag
	}

	asset := &model.Asset{
		ID: session.ID, UserID: session.UserID, Purpose: session.Purpose,
		StorageKey: finalKey, FileName: session.FileName, ContentType: normalizeDirectUploadContentType(session.ContentType),
		Size: session.Size, ETag: strings.TrimSpace(finalInfo.ETag),
	}
	err = repo.WithTx(ctx, func(txRepo repository.Repository) error {
		if createErr := txRepo.Assets().Create(ctx, asset); createErr != nil {
			existing, findErr := txRepo.Assets().FindByID(ctx, asset.ID)
			if findErr != nil {
				return createErr
			}
			if err := verifyUploadSessionAsset(session, existing); err != nil {
				return err
			}
			if existing.StorageKey != asset.StorageKey || existing.ETag != asset.ETag {
				return ErrUploadSessionObjectInvalid
			}
			asset = existing
		}
		completed, completeErr := txRepo.UploadSessions().CompleteFinalization(ctx, session.ID, token, asset.ID, req.Now)
		if completeErr != nil {
			return completeErr
		}
		if !completed {
			return ErrUploadSessionStateConflict
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrUploadSessionStateConflict) || errors.Is(err, ErrUploadSessionObjectInvalid) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: persist finalized upload: %v", ErrUploadSessionUnavailable, err)
	}
	committed = true
	deleteCtx, deleteCancel := context.WithTimeout(context.WithoutCancel(ctx), uploadFinalizationCleanupTTL)
	defer deleteCancel()
	_ = store.Delete(deleteCtx, session.StagingKey)
	return asset, nil
}

func loadFinalizedUploadSessionAsset(ctx context.Context, store DirectUploadFinalizationStorage, repo repository.Repository, session *model.UploadSession) (*model.Asset, error) {
	if strings.TrimSpace(session.AssetID) == "" || session.AssetID != session.ID {
		return nil, ErrUploadSessionStateConflict
	}
	asset, err := repo.Assets().FindByID(ctx, session.AssetID)
	if err != nil {
		if errors.Is(err, model.ErrAssetNotFound) {
			return nil, ErrUploadSessionStateConflict
		}
		return nil, fmt.Errorf("%w: find finalized asset: %v", ErrUploadSessionUnavailable, err)
	}
	if err := verifyUploadSessionAsset(session, asset); err != nil {
		return nil, err
	}
	info, err := statUploadSessionObject(ctx, store, asset.StorageKey, "final")
	if err != nil {
		return nil, err
	}
	if err := validateFinalUploadSessionObject(session, asset.StorageKey, asset.ETag, info); err != nil {
		return nil, err
	}
	return asset, nil
}

func verifyUploadSessionAsset(session *model.UploadSession, asset *model.Asset) error {
	finalKey, err := finalizedUploadSessionKey(session)
	if err != nil || asset == nil || asset.ID != session.ID || asset.UserID != session.UserID || asset.Purpose != session.Purpose ||
		asset.StorageKey != finalKey || asset.FileName != session.FileName || normalizeDirectUploadContentType(asset.ContentType) != normalizeDirectUploadContentType(session.ContentType) || asset.Size != session.Size || strings.TrimSpace(session.FinalizationETag) == "" || strings.TrimSpace(asset.ETag) != strings.TrimSpace(session.FinalizationETag) {
		return ErrUploadSessionObjectInvalid
	}
	return nil
}

func statUploadSessionObject(ctx context.Context, store storage.ObjectStatProvider, key, kind string) (*storage.ObjectInfo, error) {
	info, err := store.StatObject(ctx, key)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotFound) {
			return nil, fmt.Errorf("%w: %s object not found", ErrUploadSessionObjectInvalid, kind)
		}
		return nil, fmt.Errorf("%w: stat %s upload object: %v", ErrUploadSessionUnavailable, kind, err)
	}
	if info == nil {
		return nil, fmt.Errorf("%w: %s object metadata is unavailable", ErrUploadSessionObjectInvalid, kind)
	}
	return info, nil
}

func validateUploadSessionObjectInfo(session *model.UploadSession, objectKey string, info *storage.ObjectInfo) error {
	if session == nil || info == nil || strings.TrimSpace(objectKey) == "" || info.Size != session.Size {
		return fmt.Errorf("%w: %s size mismatch", ErrUploadSessionObjectInvalid, objectKey)
	}
	policy, ok := directUploadPolicies[session.Purpose]
	if !ok {
		return fmt.Errorf("%w: unsupported upload purpose %q", ErrUploadSessionObjectInvalid, session.Purpose)
	}
	expectedType := normalizeDirectUploadContentType(session.ContentType)
	actualType := normalizeDirectUploadContentType(firstNonEmptyString(info.ContentType, info.MimeType))
	ext := strings.ToLower(filepath.Ext(session.FileName))
	maxSize := policy.maxSize
	if policy.maxSizeFor != nil {
		maxSize = policy.maxSizeFor(session.ContentType, ext)
	}
	if maxSize <= 0 || info.Size > maxSize || expectedType == "" || actualType == "" || expectedType != actualType {
		return fmt.Errorf("%w: %s metadata does not match prepared upload", ErrUploadSessionObjectInvalid, objectKey)
	}
	return nil
}

func validateFinalUploadSessionObject(session *model.UploadSession, finalKey, expectedETag string, info *storage.ObjectInfo) error {
	if err := validateUploadSessionObjectInfo(session, finalKey, info); err != nil {
		return err
	}
	if strings.TrimSpace(info.ETag) == "" || strings.TrimSpace(info.ETag) != strings.TrimSpace(expectedETag) {
		return fmt.Errorf("%w: final object ETag does not match", ErrUploadSessionObjectInvalid)
	}
	return nil
}

func finalizedUploadSessionKey(session *model.UploadSession) (string, error) {
	if session == nil {
		return "", errors.New("upload session is required")
	}
	userID := strings.TrimSpace(session.UserID)
	sessionID := strings.TrimSpace(session.ID)
	fileName := path.Base(strings.TrimSpace(session.FileName))
	if userID == "" || sessionID == "" || fileName == "" || fileName == "." || fileName == "/" || path.Base(userID) != userID || path.Base(sessionID) != sessionID || fileName != session.FileName {
		return "", errors.New("upload session identity is invalid")
	}
	return path.Join("assets/users", userID, sessionID, fileName), nil
}

func FinalizeUploadSessionURLs(ctx context.Context, store DirectUploadFinalizationStorage, repo repository.Repository, userID, purpose string, urls []string, now time.Time, ownedURLChecks ...func(string) bool) (map[string]string, error) {
	rewrites := make(map[string]string)
	if repo == nil || len(urls) == 0 {
		return rewrites, nil
	}
	var isOwnedURL func(string) bool
	if len(ownedURLChecks) > 0 {
		isOwnedURL = ownedURLChecks[0]
	}
	for _, raw := range urls {
		key, ok := uploadSessionKeyFromURL(raw)
		if !ok {
			continue
		}
		matchesStoreURL := store != nil && directUploadFinalURLMatches(raw, store.GetURL(key), key)
		if isOwnedURL != nil && !isOwnedURL(raw) && !matchesStoreURL && !strings.HasPrefix(raw, "/api/v1/files/") && !strings.HasPrefix(raw, "/files/") {
			continue
		}
		sessionID := uploadSessionIDFromKey(key)
		if sessionID == "" {
			return nil, ErrUploadSessionInvalidURL
		}
		if store == nil {
			return nil, ErrUploadSessionUnavailable
		}
		session, err := repo.UploadSessions().FindByID(ctx, sessionID)
		if err != nil {
			if errors.Is(err, model.ErrUploadSessionNotFound) {
				return nil, ErrUploadSessionAccessDenied
			}
			return nil, fmt.Errorf("%w: find upload session: %v", ErrUploadSessionUnavailable, err)
		}
		if session.UserID != userID || session.Purpose != purpose {
			return nil, ErrUploadSessionAccessDenied
		}
		if key != session.StagingKey {
			if session.Status != model.UploadSessionFinalized || session.AssetID == "" {
				return nil, ErrUploadSessionAccessDenied
			}
			asset, err := repo.Assets().FindByID(ctx, session.AssetID)
			if err != nil || asset.StorageKey != key {
				return nil, ErrUploadSessionAccessDenied
			}
		}
		asset, err := FinalizeUploadSession(ctx, store, repo, FinalizeUploadRequest{
			SessionID: session.ID, UserID: userID, AllowedPurposes: []string{purpose}, Now: now,
		})
		if err != nil {
			return nil, err
		}
		rewrites[raw] = store.GetURL(asset.StorageKey)
	}
	return rewrites, nil
}

func ValidateUploadSessionURL(ctx context.Context, repo repository.UploadSessionRepository, userID string, allowedPurposes []string, rawURL string, now time.Time) (string, error) {
	if repo == nil {
		return "", ErrUploadSessionUnavailable
	}
	key, ok := uploadSessionKeyFromURL(rawURL)
	if !ok || !strings.HasPrefix(key, "uploads/pending/") {
		return "", ErrUploadSessionInvalidURL
	}
	sessionID := uploadSessionIDFromKey(key)
	session, err := repo.FindByID(ctx, sessionID)
	if err != nil {
		if errors.Is(err, model.ErrUploadSessionNotFound) {
			return "", ErrUploadSessionAccessDenied
		}
		return "", fmt.Errorf("%w: find upload session: %v", ErrUploadSessionUnavailable, err)
	}
	if session.UserID != userID || !directUploadPurposeAllowed(session.Purpose, allowedPurposes) || session.StagingKey != key {
		return "", ErrUploadSessionAccessDenied
	}
	if session.Status != model.UploadSessionPending {
		return "", ErrUploadSessionStateConflict
	}
	if !session.ExpiresAt.After(now) {
		return "", ErrUploadSessionExpired
	}
	return session.StagingKey, nil
}

func ResolveDirectUploadSessionAttachment(ctx context.Context, store DirectUploadFinalizationStorage, repo repository.Repository, userID string, allowedPurposes []string, uploadID, assertedKey string, now time.Time) (*VerifiedDirectUpload, error) {
	verified, err := VerifyDirectUploadSessionAttachment(ctx, repo, userID, allowedPurposes, uploadID, assertedKey, now)
	if err != nil {
		return nil, err
	}
	asset, err := FinalizeUploadSession(ctx, store, repo, FinalizeUploadRequest{
		SessionID: uploadID, UserID: userID, AllowedPurposes: allowedPurposes, Now: now,
	})
	if err != nil {
		return nil, err
	}
	verified.Key = asset.StorageKey
	verified.FileName = asset.FileName
	verified.ContentType = asset.ContentType
	verified.Size = asset.Size
	return verified, nil
}

func VerifyDirectUploadSessionAttachment(ctx context.Context, repo repository.Repository, userID string, allowedPurposes []string, uploadID, assertedKey string, now time.Time) (*VerifiedDirectUpload, error) {
	if repo == nil || userID == "" || uploadID == "" || assertedKey == "" {
		return nil, ErrUploadSessionAccessDenied
	}
	session, err := repo.UploadSessions().FindByID(ctx, uploadID)
	if err != nil {
		if errors.Is(err, model.ErrUploadSessionNotFound) {
			return nil, ErrUploadSessionAccessDenied
		}
		return nil, fmt.Errorf("%w: find upload session: %v", ErrUploadSessionUnavailable, err)
	}
	if session.UserID != userID || !directUploadPurposeAllowed(session.Purpose, allowedPurposes) {
		return nil, ErrUploadSessionAccessDenied
	}
	if session.Status != model.UploadSessionFinalized {
		if session.Status != model.UploadSessionPending || !session.ExpiresAt.After(now) {
			if !session.ExpiresAt.After(now) {
				return nil, ErrUploadSessionExpired
			}
			return nil, ErrUploadSessionStateConflict
		}
	}
	if assertedKey != session.StagingKey {
		if session.Status != model.UploadSessionFinalized || session.AssetID == "" {
			return nil, ErrUploadSessionAccessDenied
		}
		asset, findErr := repo.Assets().FindByID(ctx, session.AssetID)
		if findErr != nil || asset.StorageKey != assertedKey {
			return nil, ErrUploadSessionAccessDenied
		}
	}
	finalKey, err := finalizedUploadSessionKey(session)
	if err != nil {
		return nil, ErrUploadSessionAccessDenied
	}
	return &VerifiedDirectUpload{UploadID: session.ID, Key: finalKey, FileName: session.FileName, ContentType: session.ContentType, Size: session.Size, Purpose: session.Purpose}, nil
}

func uploadSessionKeyFromURL(raw string) (string, bool) {
	key, ok := storage.StorageKeyFromURL(strings.TrimSpace(raw))
	if !ok {
		return "", false
	}
	key = strings.TrimPrefix(key, "/")
	if path.Clean(key) != key || (!strings.HasPrefix(key, "uploads/pending/") && !strings.HasPrefix(key, "assets/users/")) {
		return "", false
	}
	return key, true
}

func uploadSessionIDFromKey(key string) string {
	parts := strings.Split(strings.TrimPrefix(key, "/"), "/")
	if len(parts) < 5 {
		return ""
	}
	if parts[0] == "uploads" && parts[1] == "pending" || parts[0] == "assets" && parts[1] == "users" {
		return parts[3]
	}
	return ""
}

func normalizeDirectUploadContentType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err == nil {
		return strings.ToLower(strings.TrimSpace(mediaType))
	}
	return strings.ToLower(strings.TrimSpace(strings.SplitN(value, ";", 2)[0]))
}

func directUploadPurposeAllowed(purpose string, allowed []string) bool {
	for _, candidate := range allowed {
		if purpose == candidate {
			return true
		}
	}
	return false
}

func CleanupExpiredUploadSessions(ctx context.Context, store DirectUploadFinalizationStorage, repo repository.Repository, before time.Time, limit int) (int, error) {
	if store == nil || repo == nil {
		return 0, nil
	}
	if limit <= 0 {
		limit = 100
	}
	claimStaleBefore := before.Add(-uploadSessionCleanupLease)
	sessions, err := repo.UploadSessions().FindForCleanup(ctx, before, claimStaleBefore, limit)
	if err != nil {
		return 0, err
	}
	cleaned := 0
	for _, session := range sessions {
		if session == nil {
			continue
		}
		if session.Purpose != DirectUploadPurposeTaskArtifact && (strings.TrimSpace(session.FinalizationETag) != "" || strings.TrimSpace(session.PromotionSourceETag) != "") {
			finalKey, keyErr := finalizedUploadSessionKey(session)
			if keyErr != nil {
				return cleaned, fmt.Errorf("%w: %v", ErrUploadSessionObjectInvalid, keyErr)
			}
			statCtx, statCancel := context.WithTimeout(ctx, uploadFinalizationTimeout)
			_, finalErr := store.StatObject(statCtx, finalKey)
			statCancel()
			switch {
			case finalErr == nil:
				if _, finalizeErr := FinalizeUploadSession(ctx, store, repo, FinalizeUploadRequest{
					SessionID: session.ID, UserID: session.UserID, AllowedPurposes: []string{session.Purpose}, Now: before,
				}); finalizeErr != nil {
					return cleaned, finalizeErr
				}
				continue
			case errors.Is(finalErr, storage.ErrObjectNotFound):
			default:
				return cleaned, fmt.Errorf("%w: stat final upload object during cleanup: %v", ErrUploadSessionUnavailable, finalErr)
			}
		}
		claimID := uuid.NewString()
		claimed, err := repo.UploadSessions().ClaimExpiration(ctx, session.ID, claimID, before, claimStaleBefore)
		if err != nil {
			return cleaned, err
		}
		if !claimed {
			continue
		}
		if err := store.Delete(ctx, session.StagingKey); err != nil {
			reopened, reopenErr := repo.UploadSessions().ReopenExpiration(ctx, session.ID, claimID)
			if reopenErr != nil {
				return cleaned, fmt.Errorf("delete expired upload session staging object: %w; reopen cleanup claim: %v", err, reopenErr)
			}
			if !reopened {
				return cleaned, fmt.Errorf("delete expired upload session staging object: %w; cleanup claim was not reopened", err)
			}
			return cleaned, err
		}
		if session.Purpose == DirectUploadPurposeTaskArtifact && before.Before(session.ExpiresAt.Add(taskArtifactCleanupGrace)) {
			nextCleanupAt := minTime(before.Add(taskArtifactCleanupRetryDelay), session.ExpiresAt.Add(taskArtifactCleanupGrace))
			reopened, err := repo.UploadSessions().RescheduleExpiration(ctx, session.ID, claimID, nextCleanupAt)
			if err != nil {
				return cleaned, err
			}
			if !reopened {
				return cleaned, ErrUploadSessionStateConflict
			}
			cleaned++
			continue
		}
		completed, err := repo.UploadSessions().CompleteExpiration(ctx, session.ID, claimID, before)
		if err != nil {
			return cleaned, err
		}
		if !completed {
			return cleaned, ErrUploadSessionStateConflict
		}
		cleaned++
	}
	return cleaned, nil
}

func directUploadFinalURLMatches(raw, finalURL, finalKey string) bool {
	candidate := stripURLQueryAndFragment(strings.TrimSpace(raw))
	key := strings.TrimPrefix(strings.TrimSpace(finalKey), "/")
	if candidate == key || strings.TrimPrefix(candidate, "/") == key {
		return true
	}
	candidateURL, candidateIsAbsolute := parseAbsoluteURL(candidate)
	finalParsed, finalIsAbsolute := parseAbsoluteURL(stripURLQueryAndFragment(finalURL))
	if !candidateIsAbsolute || !finalIsAbsolute || !strings.EqualFold(candidateURL.Host, finalParsed.Host) {
		return false
	}
	return strings.TrimPrefix(candidateURL.Path, "/") == key && strings.TrimPrefix(finalParsed.Path, "/") == key
}

func stripURLQueryAndFragment(raw string) string {
	if raw == "" {
		return ""
	}
	cut := strings.SplitN(raw, "#", 2)[0]
	return strings.SplitN(cut, "?", 2)[0]
}

func parseAbsoluteURL(raw string) (*url.URL, bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, false
	}
	return parsed, true
}

type aliyunUploadCredentialIssuer struct {
	cfg config.StorageConfig
}

func NewAliyunUploadCredentialIssuer(cfg config.StorageConfig) UploadCredentialIssuer {
	return &aliyunUploadCredentialIssuer{cfg: cfg}
}

func (i *aliyunUploadCredentialIssuer) IssueUploadCredential(_ context.Context, req UploadCredentialRequest) (*UploadCredential, error) {
	if strings.TrimSpace(req.RoleArn) == "" {
		return nil, fmt.Errorf("storage.sts_role_arn is required for browser direct uploads")
	}
	if strings.TrimSpace(i.cfg.AccessKeyID) == "" || strings.TrimSpace(i.cfg.AccessKeySecret) == "" {
		return nil, fmt.Errorf("storage access key is required for browser direct uploads")
	}
	expires := req.Expires
	if expires <= 0 {
		expires = defaultDirectUploadTTLSeconds
	}
	credCfg := credential.Config{
		Type:                  ptrString("ram_role_arn"),
		AccessKeyId:           ptrString(i.cfg.AccessKeyID),
		AccessKeySecret:       ptrString(i.cfg.AccessKeySecret),
		RoleArn:               ptrString(req.RoleArn),
		RoleSessionName:       ptrString(firstNonEmptyString(req.SessionName, "studio-direct-upload")),
		RoleSessionExpiration: &expires,
		Policy:                ptrString(req.Policy),
	}
	if req.STSEndpoint != "" {
		credCfg.STSEndpoint = ptrString(req.STSEndpoint)
	}
	cred, err := credential.NewCredential(&credCfg)
	if err != nil {
		return nil, err
	}
	model, err := cred.GetCredential()
	if err != nil {
		return nil, err
	}
	return &UploadCredential{
		AccessKeyID:     derefCredentialString(model.AccessKeyId),
		AccessKeySecret: derefCredentialString(model.AccessKeySecret),
		SecurityToken:   derefCredentialString(model.SecurityToken),
		ExpiresAt:       time.Now().Add(time.Duration(expires) * time.Second),
	}, nil
}

func directUploadPolicyJSON(bucket, key string) string {
	// RAM scopes PUT and multipart actions to the exact object key. The current
	// OSS STS/PUT flow has no reliably enforceable content-length condition, so
	// finalization HEAD checks and bounded reads remain the authoritative limits.
	policy := map[string]any{
		"Version": "1",
		"Statement": []map[string]any{{
			"Effect": "Allow",
			"Action": []string{
				"oss:PutObject",
				"oss:InitiateMultipartUpload",
				"oss:UploadPart",
				"oss:CompleteMultipartUpload",
				"oss:AbortMultipartUpload",
				"oss:ListParts",
			},
			"Resource": []string{fmt.Sprintf("acs:oss:*:*:%s/%s", bucket, key)},
		}},
	}
	raw, _ := json.Marshal(policy)
	return string(raw)
}

type directUploadFileRule struct {
	kind         string
	contentTypes []string
}

var directUploadFileRules = map[string]directUploadFileRule{
	".jpg":      {kind: "image", contentTypes: []string{"image/jpeg", "image/jpg", "image/pjpeg"}},
	".jpeg":     {kind: "image", contentTypes: []string{"image/jpeg", "image/jpg", "image/pjpeg"}},
	".png":      {kind: "image", contentTypes: []string{"image/png", "image/x-png"}},
	".webp":     {kind: "image", contentTypes: []string{"image/webp"}},
	".gif":      {kind: "image", contentTypes: []string{"image/gif"}},
	".bmp":      {kind: "image", contentTypes: []string{"image/bmp", "image/x-ms-bmp"}},
	".mp3":      {kind: "audio", contentTypes: []string{"audio/mpeg", "audio/mp3"}},
	".wav":      {kind: "audio", contentTypes: []string{"audio/wav", "audio/x-wav"}},
	".m4a":      {kind: "audio", contentTypes: []string{"audio/mp4", "audio/x-m4a"}},
	".aac":      {kind: "audio", contentTypes: []string{"audio/aac"}},
	".ogg":      {kind: "audio", contentTypes: []string{"audio/ogg", "application/ogg"}},
	".mp4":      {kind: "video", contentTypes: []string{"video/mp4"}},
	".mov":      {kind: "video", contentTypes: []string{"video/quicktime"}},
	".webm":     {kind: "video", contentTypes: []string{"video/webm"}},
	".pdf":      {kind: "document", contentTypes: []string{"application/pdf"}},
	".doc":      {kind: "document", contentTypes: []string{"application/msword"}},
	".docx":     {kind: "document", contentTypes: []string{"application/vnd.openxmlformats-officedocument.wordprocessingml.document"}},
	".ppt":      {kind: "document", contentTypes: []string{"application/vnd.ms-powerpoint"}},
	".pptx":     {kind: "document", contentTypes: []string{"application/vnd.openxmlformats-officedocument.presentationml.presentation"}},
	".xls":      {kind: "document", contentTypes: []string{"application/vnd.ms-excel"}},
	".xlsx":     {kind: "document", contentTypes: []string{"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"}},
	".json":     {kind: "document", contentTypes: []string{"application/json", "text/json"}},
	".csv":      {kind: "text", contentTypes: []string{"text/csv", "application/csv"}},
	".txt":      {kind: "text", contentTypes: []string{"text/plain"}},
	".md":       {kind: "text", contentTypes: []string{"text/markdown", "text/plain"}},
	".markdown": {kind: "text", contentTypes: []string{"text/markdown", "text/plain"}},
}

var directUploadCanonicalExtensionOrder = []string{
	".jpg", ".png", ".webp", ".gif", ".bmp",
	".mp3", ".wav", ".m4a", ".aac", ".ogg",
	".mp4", ".mov", ".webm",
	".pdf", ".doc", ".docx", ".ppt", ".pptx", ".xls", ".xlsx",
	".json", ".csv", ".txt", ".md",
}

func canonicalDirectUploadExtension(contentType string) string {
	ct := normalizeDirectUploadContentType(contentType)
	for _, ext := range directUploadCanonicalExtensionOrder {
		rule := directUploadFileRules[ext]
		for _, allowed := range rule.contentTypes {
			if ct == allowed {
				return ext
			}
		}
	}
	return ""
}

func directUploadFileKind(contentType, ext string) string {
	ct := normalizeDirectUploadContentType(contentType)
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext != "" {
		rule, ok := directUploadFileRules[ext]
		if !ok {
			return ""
		}
		for _, allowed := range rule.contentTypes {
			if ct == allowed {
				return rule.kind
			}
		}
		return ""
	}
	for _, rule := range directUploadFileRules {
		for _, allowed := range rule.contentTypes {
			if ct == allowed {
				return rule.kind
			}
		}
	}
	return ""
}

// ClassifyDirectUploadFile returns the canonical attachment kind for an exact
// content-type and extension pair accepted by the direct-upload boundary.
func ClassifyDirectUploadFile(contentType, ext string) string {
	return directUploadFileKind(contentType, ext)
}

func isDirectUploadImage(contentType, ext string) bool {
	return directUploadFileKind(contentType, ext) == "image"
}

func isDirectUploadAIEntryAttachment(contentType, ext string) bool {
	return aiEntryAttachmentMaxSize(contentType, ext) > 0
}

func isDirectUploadMontageAsset(contentType, ext string) bool {
	switch directUploadFileKind(contentType, ext) {
	case "image", "audio", "video":
		return true
	default:
		return false
	}
}

func aiEntryAttachmentMaxSize(contentType, ext string) int64 {
	switch directUploadFileKind(contentType, ext) {
	case "image", "audio", "video":
		return 50 * 1024 * 1024
	case "document", "text":
		return 25 * 1024 * 1024
	default:
		return 0
	}
}

func sanitizeUploadFilename(raw string) string {
	base := filepath.Base(strings.TrimSpace(raw))
	base = strings.Trim(base, ".")
	if base == "" || base == "/" {
		return ""
	}
	base = unsafeFilenameRunes.ReplaceAllString(base, "-")
	return strings.Trim(base, "-")
}

func contentTypeForUploadExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".bmp":
		return "image/bmp"
	case ".mp4":
		return "video/mp4"
	case ".mov":
		return "video/quicktime"
	case ".webm":
		return "video/webm"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".m4a":
		return "audio/mp4"
	case ".aac":
		return "audio/aac"
	case ".ogg":
		return "audio/ogg"
	case ".pdf":
		return "application/pdf"
	case ".doc":
		return "application/msword"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".ppt":
		return "application/vnd.ms-powerpoint"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".xls":
		return "application/vnd.ms-excel"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".csv":
		return "text/csv"
	case ".txt":
		return "text/plain"
	case ".md", ".markdown":
		return "text/markdown"
	case ".json":
		return "application/json"
	default:
		return "application/octet-stream"
	}
}

func ossBrowserRegion(region, endpoint string) string {
	region = strings.TrimSpace(region)
	if region == "" {
		host := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(endpoint), "https://"), "http://")
		host = strings.TrimSuffix(host, "/")
		if strings.HasSuffix(host, ".aliyuncs.com") {
			region = strings.TrimSuffix(host, ".aliyuncs.com")
		}
	}
	if region != "" && !strings.HasPrefix(region, "oss-") {
		region = "oss-" + region
	}
	return region
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func ptrString(value string) *string { return &value }

func derefCredentialString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func minTime(a, b time.Time) time.Time {
	if a.IsZero() {
		return b
	}
	if b.IsZero() || a.Before(b) {
		return a
	}
	return b
}
