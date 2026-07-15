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
	"github.com/anbanai/anban-creator/server/storage"
)

const (
	DirectUploadPurposeProjectReference  = "project_reference"
	DirectUploadPurposeTaskReference     = "task_reference"
	DirectUploadPurposeEcommercePhoto    = "ecommerce_product_photo"
	DirectUploadPurposeVideoReference    = "video_reference"
	DirectUploadPurposeMontageAsset      = "montage_asset"
	DirectUploadPurposeDesignerReference = "designer_reference"
	DirectUploadPurposeAIEntryAttachment = "ai_entry_attachment"
	DirectUploadPurposeTaskArtifact      = "task_artifact"

	defaultDirectUploadTTLSeconds = 15 * 60
	pendingUploadCleanupLease     = 5 * time.Minute
)

var (
	ErrPendingUploadNotFound      = model.ErrPendingUploadNotFound
	ErrPendingUploadInvalidURL    = errors.New("pending upload URL is invalid")
	ErrPendingUploadAccessDenied  = errors.New("pending upload access denied")
	ErrPendingUploadNotPending    = errors.New("pending upload is not pending")
	ErrPendingUploadExpired       = errors.New("pending upload has expired")
	ErrPendingUploadObjectInvalid = errors.New("pending upload object metadata is invalid")
)

type directUploadStorage interface {
	Name() string
	UploadURL(ctx context.Context, key string, contentType string, expirySeconds int) (string, error)
	DownloadURL(ctx context.Context, key string, expirySeconds int) (string, error)
	GetURL(key string) string
	Delete(ctx context.Context, key string) error
}

type PendingUploadRepository interface {
	CreatePendingUpload(ctx context.Context, upload *model.PendingUpload) error
	FindPendingUploadByID(ctx context.Context, id string) (*model.PendingUpload, error)
	FinalizePendingUploadClaims(ctx context.Context, claims []model.PendingUploadClaim, finalizedAt time.Time) error
	FindPendingUploadsForCleanup(ctx context.Context, expiredBefore, claimStaleBefore time.Time, limit int) ([]*model.PendingUpload, error)
	ClaimPendingUploadExpiration(ctx context.Context, id, claimID string, claimedAt, claimStaleBefore time.Time) (bool, error)
	CompletePendingUploadExpiration(ctx context.Context, id, claimID string, expiredAt time.Time) (bool, error)
	ReopenPendingUploadExpiration(ctx context.Context, id, claimID string) (bool, error)
}

type DirectUploadPrepareRequest struct {
	UserID      string `json:"-"`
	Purpose     string `json:"purpose"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

type DirectUploadPrepareResult struct {
	UploadID           string            `json:"upload_id"`
	Key                string            `json:"key"`
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
	sourceKey   string
	promote     bool
	claim       *model.PendingUploadClaim
}

type DirectUploadFinalizationStorage interface {
	storage.ObjectStatProvider
	storage.ConditionalObjectPromoter
	GetURL(key string) string
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
	DirectUploadPurposeDesignerReference: {maxSize: maxUploadImageBytes, validate: isDirectUploadImage},
	DirectUploadPurposeVideoReference:    {maxSize: 50 * 1024 * 1024, validate: isDirectUploadVideoReference},
	DirectUploadPurposeMontageAsset:      {maxSize: 50 * 1024 * 1024, validate: isDirectUploadMontageAsset},
	DirectUploadPurposeAIEntryAttachment: {maxSize: 50 * 1024 * 1024, maxSizeFor: aiEntryAttachmentMaxSize, validate: isDirectUploadAIEntryAttachment},
}

const maxUploadImageBytes = 10 * 1024 * 1024

var unsafeFilenameRunes = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func PrepareDirectUpload(ctx context.Context, store directUploadStorage, repo PendingUploadRepository, cfg DirectUploadConfig, req DirectUploadPrepareRequest) (*DirectUploadPrepareResult, error) {
	if store == nil {
		return nil, fmt.Errorf("storage provider is not available")
	}
	if store.Name() != "oss" {
		return nil, fmt.Errorf("direct uploads require OSS storage")
	}
	if repo == nil {
		return nil, fmt.Errorf("pending upload repository is not available")
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
		if exts, _ := mime.ExtensionsByType(contentType); len(exts) > 0 {
			ext = strings.ToLower(exts[0])
		}
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
	upload := &model.PendingUpload{
		ID:          uploadID,
		UserID:      userID,
		Purpose:     purpose,
		Key:         key,
		PublicURL:   publicURL,
		FileName:    filename,
		ContentType: contentType,
		Size:        req.Size,
		Status:      model.PendingUploadStatusPending,
		ExpiresAt:   expiresAt,
	}
	if err := repo.CreatePendingUpload(ctx, upload); err != nil {
		return nil, fmt.Errorf("record pending upload: %w", err)
	}
	return &DirectUploadPrepareResult{
		UploadID:           uploadID,
		Key:                key,
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

func FinalizePendingUploadURLs(ctx context.Context, store DirectUploadFinalizationStorage, repo PendingUploadRepository, userID, purpose string, urls []string, now time.Time) (map[string]string, error) {
	rewrites := make(map[string]string)
	if repo == nil || len(urls) == 0 {
		return rewrites, nil
	}
	hasDirectUpload := false
	for _, raw := range urls {
		if directUploadIDFromURL(raw) != "" {
			hasDirectUpload = true
			break
		}
	}
	if !hasDirectUpload {
		return rewrites, nil
	}
	if store == nil {
		return nil, storage.ErrObjectStatUnsupported
	}
	verifiedByID := make(map[string]*VerifiedDirectUpload, len(urls))
	verifiedByURL := make(map[string]*VerifiedDirectUpload, len(urls))
	for _, raw := range urls {
		id := directUploadIDFromURL(raw)
		if id == "" {
			continue
		}
		upload, err := repo.FindPendingUploadByID(ctx, id)
		if err != nil {
			if errors.Is(err, ErrPendingUploadNotFound) {
				return nil, fmt.Errorf("%w: pending upload not found", ErrPendingUploadAccessDenied)
			}
			return nil, err
		}
		if upload.UserID != userID {
			return nil, fmt.Errorf("%w: pending upload %s does not belong to current user", ErrPendingUploadAccessDenied, id)
		}
		if upload.Purpose != purpose {
			return nil, fmt.Errorf("%w: pending upload %s has purpose %s, want %s", ErrPendingUploadAccessDenied, id, upload.Purpose, purpose)
		}
		assertedKey, ok := storage.StorageKeyFromURL(raw)
		if !ok {
			return nil, ErrPendingUploadAccessDenied
		}
		assertedKey = strings.TrimPrefix(assertedKey, "/")
		switch assertedKey {
		case upload.Key:
			if !pendingUploadURLMatches(raw, upload) {
				return nil, ErrPendingUploadAccessDenied
			}
		case upload.FinalizedKey:
			if upload.FinalizedKey == "" || !directUploadFinalURLMatches(raw, store.GetURL(upload.FinalizedKey), upload.FinalizedKey) {
				return nil, ErrPendingUploadAccessDenied
			}
		default:
			return nil, ErrPendingUploadAccessDenied
		}
		verified := verifiedByID[id]
		if verified == nil {
			verified, err = VerifyDirectUploadAttachment(ctx, repo, userID, []string{purpose}, id, assertedKey, now)
			if err != nil {
				return nil, err
			}
			verifiedByID[id] = verified
		}
		verifiedByURL[raw] = verified
	}
	if len(verifiedByID) == 0 {
		return rewrites, nil
	}
	verified := make([]*VerifiedDirectUpload, 0, len(verifiedByID))
	for _, item := range verifiedByID {
		verified = append(verified, item)
	}
	if err := FinalizeVerifiedDirectUploads(ctx, store, repo, verified, now); err != nil {
		return nil, err
	}
	for raw, item := range verifiedByURL {
		rewrites[raw] = store.GetURL(item.Key)
	}
	return rewrites, nil
}

func ValidatePendingUploadURL(ctx context.Context, repo PendingUploadRepository, userID string, allowedPurposes []string, rawURL string, now time.Time) (string, error) {
	if repo == nil {
		return "", fmt.Errorf("pending upload repository is not available")
	}
	if strings.TrimSpace(userID) == "" {
		return "", ErrPendingUploadAccessDenied
	}
	id := pendingUploadIDFromURL(rawURL)
	if id == "" {
		return "", ErrPendingUploadInvalidURL
	}
	upload, err := repo.FindPendingUploadByID(ctx, id)
	if err != nil {
		if errors.Is(err, ErrPendingUploadNotFound) {
			return "", ErrPendingUploadAccessDenied
		}
		return "", err
	}
	if upload.UserID != userID {
		return "", ErrPendingUploadAccessDenied
	}
	if !directUploadPurposeAllowed(upload.Purpose, allowedPurposes) {
		return "", ErrPendingUploadAccessDenied
	}
	if upload.Status != model.PendingUploadStatusPending {
		return "", ErrPendingUploadNotPending
	}
	if !upload.ExpiresAt.After(now) {
		return "", ErrPendingUploadExpired
	}
	if strings.TrimSpace(upload.Key) == "" || !pendingUploadURLMatches(rawURL, upload) {
		return "", ErrPendingUploadAccessDenied
	}
	return upload.Key, nil
}

func ResolveDirectUploadAttachment(ctx context.Context, store DirectUploadFinalizationStorage, repo PendingUploadRepository, userID string, allowedPurposes []string, uploadID, assertedKey string, now time.Time) (*VerifiedDirectUpload, error) {
	verified, err := VerifyDirectUploadAttachment(ctx, repo, userID, allowedPurposes, uploadID, assertedKey, now)
	if err != nil {
		return nil, err
	}
	if err := FinalizeVerifiedDirectUploads(ctx, store, repo, []*VerifiedDirectUpload{verified}, now); err != nil {
		return nil, err
	}
	verified.claim = nil
	verified.sourceKey = ""
	verified.promote = false
	return verified, nil
}

func VerifyDirectUploadAttachment(ctx context.Context, repo PendingUploadRepository, userID string, allowedPurposes []string, uploadID, assertedKey string, now time.Time) (*VerifiedDirectUpload, error) {
	if repo == nil {
		return nil, fmt.Errorf("pending upload repository is not available")
	}
	if userID == "" || uploadID == "" || assertedKey == "" {
		return nil, ErrPendingUploadAccessDenied
	}
	upload, err := repo.FindPendingUploadByID(ctx, uploadID)
	if err != nil {
		if errors.Is(err, ErrPendingUploadNotFound) {
			return nil, ErrPendingUploadAccessDenied
		}
		return nil, err
	}
	if upload.UserID != userID || !directUploadPurposeAllowed(upload.Purpose, allowedPurposes) {
		return nil, ErrPendingUploadAccessDenied
	}
	finalKey, err := finalizedDirectUploadKey(upload)
	if err != nil {
		return nil, ErrPendingUploadAccessDenied
	}
	promote := false
	switch upload.Status {
	case model.PendingUploadStatusPending:
		if !upload.ExpiresAt.After(now) {
			return nil, ErrPendingUploadExpired
		}
		if assertedKey != upload.Key {
			return nil, ErrPendingUploadAccessDenied
		}
		promote = true
	case model.PendingUploadStatusFinalized:
		if upload.FinalizedKey == "" || upload.FinalizedKey != finalKey {
			return nil, ErrPendingUploadNotPending
		}
		if assertedKey != upload.Key && assertedKey != upload.FinalizedKey {
			return nil, ErrPendingUploadAccessDenied
		}
	default:
		return nil, ErrPendingUploadNotPending
	}
	return &VerifiedDirectUpload{
		UploadID:    upload.ID,
		Key:         finalKey,
		FileName:    upload.FileName,
		ContentType: upload.ContentType,
		Size:        upload.Size,
		Purpose:     upload.Purpose,
		sourceKey:   upload.Key,
		promote:     promote,
		claim: &model.PendingUploadClaim{
			UploadID:        upload.ID,
			UserID:          upload.UserID,
			Key:             upload.Key,
			FinalizedKey:    finalKey,
			AllowedPurposes: append([]string(nil), allowedPurposes...),
		},
	}, nil
}

func FinalizeVerifiedDirectUploads(ctx context.Context, store DirectUploadFinalizationStorage, repo PendingUploadRepository, uploads []*VerifiedDirectUpload, now time.Time) error {
	claims := make([]model.PendingUploadClaim, 0, len(uploads))
	seen := make(map[string]struct{}, len(uploads))
	for _, upload := range uploads {
		if upload == nil || upload.claim == nil || upload.claim.UploadID == "" {
			continue
		}
		if _, ok := seen[upload.claim.UploadID]; ok {
			continue
		}
		seen[upload.claim.UploadID] = struct{}{}
		if upload.promote {
			if err := ensureFinalizedDirectUploadObject(ctx, store, upload); err != nil {
				return err
			}
		}
		claims = append(claims, *upload.claim)
	}
	if len(claims) == 0 {
		return nil
	}
	if repo == nil {
		return fmt.Errorf("pending upload repository is not available")
	}
	if err := repo.FinalizePendingUploadClaims(ctx, claims, now); err != nil {
		if errors.Is(err, model.ErrPendingUploadClaimRejected) {
			return ErrPendingUploadNotPending
		}
		return err
	}
	return nil
}

func ensureFinalizedDirectUploadObject(ctx context.Context, store DirectUploadFinalizationStorage, upload *VerifiedDirectUpload) error {
	if store == nil {
		return storage.ErrObjectStatUnsupported
	}
	pending := &model.PendingUpload{
		ID: upload.UploadID, UserID: upload.claim.UserID, Purpose: upload.Purpose, Key: upload.sourceKey,
		FileName: upload.FileName, ContentType: upload.ContentType, Size: upload.Size,
	}
	if info, err := store.StatObject(ctx, upload.Key); err == nil {
		return validateDirectUploadObjectInfo(pending, upload.Key, info)
	} else if !errors.Is(err, storage.ErrObjectNotFound) {
		return fmt.Errorf("stat finalized upload object %s: %w", upload.Key, err)
	}

	info, err := store.StatObject(ctx, upload.sourceKey)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotFound) {
			return fmt.Errorf("%w: object %s not found", ErrPendingUploadObjectInvalid, upload.sourceKey)
		}
		return fmt.Errorf("stat pending upload object %s: %w", upload.sourceKey, err)
	}
	if err := validateDirectUploadObjectInfo(pending, upload.sourceKey, info); err != nil {
		return err
	}
	if strings.TrimSpace(info.ETag) == "" {
		return fmt.Errorf("%w: object %s has no ETag", ErrPendingUploadObjectInvalid, upload.sourceKey)
	}
	if err := store.PromoteObject(ctx, upload.sourceKey, upload.Key, info.ETag); err != nil {
		switch {
		case errors.Is(err, storage.ErrPromotionPreconditionFailed):
			return fmt.Errorf("%w: object %s changed during finalization", ErrPendingUploadObjectInvalid, upload.sourceKey)
		case errors.Is(err, storage.ErrObjectAlreadyExists):
		default:
			return fmt.Errorf("promote pending upload object %s: %w", upload.sourceKey, err)
		}
	}
	finalInfo, err := store.StatObject(ctx, upload.Key)
	if err != nil {
		return fmt.Errorf("stat promoted upload object %s: %w", upload.Key, err)
	}
	return validateDirectUploadObjectInfo(pending, upload.Key, finalInfo)
}

func validateDirectUploadObject(ctx context.Context, store storage.ObjectStatProvider, upload *model.PendingUpload) error {
	if store == nil {
		return storage.ErrObjectStatUnsupported
	}
	if upload == nil || strings.TrimSpace(upload.Key) == "" {
		return fmt.Errorf("%w: object key is required", ErrPendingUploadObjectInvalid)
	}
	info, err := store.StatObject(ctx, upload.Key)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotFound) {
			return fmt.Errorf("%w: object %s not found", ErrPendingUploadObjectInvalid, upload.Key)
		}
		return fmt.Errorf("stat pending upload object %s: %w", upload.Key, err)
	}
	if info == nil {
		return fmt.Errorf("%w: stat %s returned no metadata", ErrPendingUploadObjectInvalid, upload.Key)
	}
	return validateDirectUploadObjectInfo(upload, upload.Key, info)
}

func validateDirectUploadObjectInfo(upload *model.PendingUpload, objectKey string, info *storage.ObjectInfo) error {
	if upload == nil || strings.TrimSpace(objectKey) == "" || info == nil {
		return fmt.Errorf("%w: object metadata is unavailable", ErrPendingUploadObjectInvalid)
	}
	if info.Size != upload.Size {
		return fmt.Errorf("%w: %s size mismatch: prepared=%d storage=%d", ErrPendingUploadObjectInvalid, objectKey, upload.Size, info.Size)
	}
	policy, ok := directUploadPolicies[upload.Purpose]
	if !ok {
		return fmt.Errorf("%w: unsupported upload purpose %q", ErrPendingUploadObjectInvalid, upload.Purpose)
	}
	ext := strings.ToLower(filepath.Ext(upload.FileName))
	maxSize := policy.maxSize
	if policy.maxSizeFor != nil {
		maxSize = policy.maxSizeFor(upload.ContentType, ext)
	}
	if maxSize <= 0 || info.Size > maxSize {
		return fmt.Errorf("%w: %s exceeds the allowed object size", ErrPendingUploadObjectInvalid, objectKey)
	}
	expectedType := normalizeDirectUploadContentType(upload.ContentType)
	actualType := normalizeDirectUploadContentType(firstNonEmptyString(info.ContentType, info.MimeType))
	if expectedType == "" || actualType == "" || expectedType != actualType {
		return fmt.Errorf("%w: %s content type mismatch: prepared=%q storage=%q", ErrPendingUploadObjectInvalid, objectKey, upload.ContentType, firstNonEmptyString(info.ContentType, info.MimeType))
	}
	return nil
}

func finalizedDirectUploadKey(upload *model.PendingUpload) (string, error) {
	if upload == nil {
		return "", errors.New("pending upload is required")
	}
	userID := strings.TrimSpace(upload.UserID)
	uploadID := strings.TrimSpace(upload.ID)
	sourceKey := strings.TrimPrefix(strings.TrimSpace(upload.Key), "/")
	if userID == "" || uploadID == "" || path.Base(userID) != userID || path.Base(uploadID) != uploadID {
		return "", errors.New("pending upload identity is invalid")
	}
	prefix := path.Join("uploads/pending", userID, uploadID) + "/"
	if !strings.HasPrefix(sourceKey, prefix) {
		return "", errors.New("pending upload source key is outside its owner path")
	}
	fileName := path.Base(sourceKey)
	if fileName == "." || fileName == "/" || fileName == "" {
		return "", errors.New("pending upload source filename is invalid")
	}
	return path.Join("uploads/finalized", userID, uploadID, fileName), nil
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

func CleanupExpiredPendingUploads(ctx context.Context, store directUploadStorage, repo PendingUploadRepository, before time.Time, limit int) (int, error) {
	if store == nil || repo == nil {
		return 0, nil
	}
	if limit <= 0 {
		limit = 100
	}
	claimStaleBefore := before.Add(-pendingUploadCleanupLease)
	uploads, err := repo.FindPendingUploadsForCleanup(ctx, before, claimStaleBefore, limit)
	if err != nil {
		return 0, err
	}
	cleaned := 0
	for _, upload := range uploads {
		if upload == nil {
			continue
		}
		claimID := uuid.NewString()
		claimed, err := repo.ClaimPendingUploadExpiration(ctx, upload.ID, claimID, before, claimStaleBefore)
		if err != nil {
			return cleaned, err
		}
		if !claimed {
			continue
		}
		finalKey, err := finalizedDirectUploadKey(upload)
		if err != nil {
			reopened, reopenErr := repo.ReopenPendingUploadExpiration(ctx, upload.ID, claimID)
			if reopenErr != nil {
				return cleaned, fmt.Errorf("resolve expired pending upload final key: %w; reopen cleanup claim: %v", err, reopenErr)
			}
			if !reopened {
				return cleaned, fmt.Errorf("resolve expired pending upload final key: %w; cleanup claim was not reopened", err)
			}
			return cleaned, err
		}
		var deleteErr error
		for _, key := range []string{upload.Key, finalKey} {
			if err := store.Delete(ctx, key); err != nil {
				deleteErr = errors.Join(deleteErr, fmt.Errorf("delete expired pending upload object %s: %w", key, err))
			}
		}
		if deleteErr != nil {
			reopened, reopenErr := repo.ReopenPendingUploadExpiration(ctx, upload.ID, claimID)
			if reopenErr != nil {
				return cleaned, fmt.Errorf("%w; reopen cleanup claim: %v", deleteErr, reopenErr)
			}
			if !reopened {
				return cleaned, fmt.Errorf("%w; cleanup claim was not reopened", deleteErr)
			}
			return cleaned, deleteErr
		}
		completed, err := repo.CompletePendingUploadExpiration(ctx, upload.ID, claimID, before)
		if err != nil {
			return cleaned, err
		}
		if !completed {
			return cleaned, fmt.Errorf("pending upload cleanup lease was lost before completion")
		}
		cleaned++
	}
	return cleaned, nil
}

func pendingUploadIDFromURL(raw string) string {
	return directUploadIDFromURLWithNamespace(raw, "pending")
}

func directUploadIDFromURL(raw string) string {
	if id := directUploadIDFromURLWithNamespace(raw, "pending"); id != "" {
		return id
	}
	return directUploadIDFromURLWithNamespace(raw, "finalized")
}

func directUploadIDFromURLWithNamespace(raw, namespace string) string {
	if raw == "" {
		return ""
	}
	cut := strings.SplitN(raw, "?", 2)[0]
	prefix := "uploads/" + namespace + "/"
	parts := strings.Split(cut, "/"+prefix)
	if len(parts) < 2 {
		if strings.HasPrefix(cut, prefix) {
			parts = []string{"", strings.TrimPrefix(cut, prefix)}
		} else {
			return ""
		}
	}
	segments := strings.Split(strings.Trim(parts[1], "/"), "/")
	if len(segments) < 2 {
		return ""
	}
	return segments[1]
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
	return strings.TrimPrefix(candidateURL.EscapedPath(), "/") == key && strings.TrimPrefix(finalParsed.EscapedPath(), "/") == key
}

func pendingUploadURLMatches(raw string, upload *model.PendingUpload) bool {
	if upload == nil || strings.TrimSpace(upload.Key) == "" {
		return false
	}
	candidate := stripURLQueryAndFragment(strings.TrimSpace(raw))
	key := strings.TrimPrefix(upload.Key, "/")
	if candidate == key || strings.TrimPrefix(candidate, "/") == key {
		return true
	}

	candidateURL, candidateIsAbsolute := parseAbsoluteURL(candidate)
	if !candidateIsAbsolute {
		return false
	}
	publicURL, publicIsAbsolute := parseAbsoluteURL(stripURLQueryAndFragment(upload.PublicURL))
	if !publicIsAbsolute {
		return false
	}
	if !strings.EqualFold(candidateURL.Host, publicURL.Host) {
		return false
	}
	candidatePath := strings.TrimPrefix(candidateURL.EscapedPath(), "/")
	publicPath := strings.TrimPrefix(publicURL.EscapedPath(), "/")
	return candidatePath == publicPath && candidatePath == key
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

func isDirectUploadImage(contentType, ext string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if strings.HasPrefix(ct, "image/") {
		switch strings.ToLower(ext) {
		case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".bmp", "":
			return true
		}
	}
	return false
}

func isDirectUploadVideoReference(contentType, ext string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if strings.HasPrefix(ct, "image/") || strings.HasPrefix(ct, "audio/") || strings.HasPrefix(ct, "video/") {
		switch strings.ToLower(ext) {
		case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".bmp", ".mp3", ".wav", ".m4a", ".aac", ".ogg", ".mp4", ".mov", ".webm", "":
			return true
		}
	}
	return false
}

func isDirectUploadAIEntryAttachment(contentType, ext string) bool {
	return aiEntryAttachmentMaxSize(contentType, ext) > 0
}

func isDirectUploadMontageAsset(contentType, ext string) bool {
	return aiEntryAttachmentMaxSize(contentType, ext) > 0
}

func aiEntryAttachmentMaxSize(contentType, ext string) int64 {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	ext = strings.ToLower(strings.TrimSpace(ext))
	if strings.HasPrefix(ct, "image/") || strings.HasPrefix(ct, "audio/") || strings.HasPrefix(ct, "video/") {
		switch ext {
		case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".bmp", ".mp3", ".wav", ".m4a", ".aac", ".ogg", ".mp4", ".mov", ".webm", "":
			return 50 * 1024 * 1024
		}
	}
	switch ext {
	case ".pdf", ".doc", ".docx", ".ppt", ".pptx", ".xls", ".xlsx", ".csv", ".txt", ".md", ".markdown", ".json":
		if isAIEntryDocumentContentType(ct, ext) {
			return 25 * 1024 * 1024
		}
	}
	return 0
}

func isAIEntryDocumentContentType(ct, ext string) bool {
	if strings.HasPrefix(ct, "text/") {
		switch ext {
		case ".csv", ".txt", ".md", ".markdown":
			return true
		}
	}
	switch ct {
	case "application/pdf",
		"application/msword",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/vnd.ms-powerpoint",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation",
		"application/vnd.ms-excel",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/csv",
		"application/json":
		return true
	case "application/octet-stream":
		// Some browsers report generic types for local files. Keep the allowlist
		// extension-bound so executable formats are still rejected.
		return ext != ""
	}
	return false
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
