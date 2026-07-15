package handler

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/service"
)

const (
	aiEntryMediaAttachmentMaxBytes     int64 = 50 * 1024 * 1024
	aiEntryDocumentAttachmentMaxBytes  int64 = 25 * 1024 * 1024
	inputAttachmentInstructionMaxRunes       = 1000
)

type InputAttachmentValidationOptions struct {
	MaxCount     int
	AllowedTypes map[string]bool
}

var allAgentAttachmentTypes = map[string]bool{
	"image":    true,
	"audio":    true,
	"video":    true,
	"document": true,
	"text":     true,
}

var errInputAttachmentValidation = errors.New("input attachment validation failed")

type inputAttachmentValidationError struct {
	err error
}

func (e *inputAttachmentValidationError) Error() string { return e.err.Error() }
func (e *inputAttachmentValidationError) Unwrap() error { return e.err }
func (e *inputAttachmentValidationError) Is(target error) bool {
	return target == errInputAttachmentValidation
}

func inputAttachmentValidationErrorf(format string, args ...any) error {
	return &inputAttachmentValidationError{err: fmt.Errorf(format, args...)}
}

func inputAttachmentServiceError(err error) error {
	if errors.Is(err, service.ErrPendingUploadAccessDenied) || errors.Is(err, service.ErrPendingUploadExpired) || errors.Is(err, service.ErrPendingUploadNotPending) {
		return &inputAttachmentValidationError{err: err}
	}
	return err
}

func respondInputAttachmentError(c fiber.Ctx, logger *zerolog.Logger, err error) error {
	if errors.Is(err, errInputAttachmentValidation) {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	if logger != nil {
		logger.Error().Err(err).Msg("input attachment validation failed")
	}
	return Error(c, fiber.StatusInternalServerError, "failed to validate attachments")
}

func validateInputAttachments(ctx context.Context, pending service.PendingUploadRepository, userID string, attachments []model.EntryAttachment, options InputAttachmentValidationOptions) ([]model.EntryAttachment, error) {
	if options.MaxCount > 0 && len(attachments) > options.MaxCount {
		return nil, inputAttachmentValidationErrorf("at most %d attachments are allowed", options.MaxCount)
	}
	now := time.Now()
	normalized := make([]model.EntryAttachment, len(attachments))
	verifiedUploads := make([]*service.VerifiedDirectUpload, 0, len(attachments))
	for i, raw := range attachments {
		a := normalizeHandlerEntryAttachment(raw)
		if utf8.RuneCountInString(a.Instruction) > inputAttachmentInstructionMaxRunes {
			return nil, inputAttachmentValidationErrorf("attachment instruction must not exceed %d characters", inputAttachmentInstructionMaxRunes)
		}
		if (a.UploadID == "") != (a.Key == "") {
			return nil, inputAttachmentValidationErrorf("attachment %d: storage attachment requires upload_id and key", i+1)
		}
		if a.UploadID != "" {
			verified, err := service.VerifyDirectUploadAttachment(ctx, pending, userID, []string{
				service.DirectUploadPurposeAIEntryAttachment,
			}, a.UploadID, a.Key, now)
			if err != nil {
				return nil, inputAttachmentServiceError(fmt.Errorf("attachment %d: %w", i+1, err))
			}
			a = model.EntryAttachment{
				UploadID:    verified.UploadID,
				Key:         verified.Key,
				FileName:    verified.FileName,
				ContentType: verified.ContentType,
				Size:        verified.Size,
				Instruction: a.Instruction,
			}
			verifiedUploads = append(verifiedUploads, verified)
		}
		if err := validateHandlerEntryAttachment(&a); err != nil {
			return nil, &inputAttachmentValidationError{err: fmt.Errorf("attachment %d: %w", i+1, err)}
		}
		if len(options.AllowedTypes) > 0 && !options.AllowedTypes[a.Type] {
			return nil, inputAttachmentValidationErrorf("attachment type %s is not allowed", a.Type)
		}
		if a.URL != "" {
			if !validAIEntryAttachmentURL(a.URL, false) {
				return nil, inputAttachmentValidationErrorf("attachment URLs must be internal file URLs or registered pending-upload URLs")
			}
		}
		normalized[i] = a
	}
	if err := service.FinalizeVerifiedDirectUploads(ctx, pending, verifiedUploads, now); err != nil {
		return nil, inputAttachmentServiceError(err)
	}
	return normalized, nil
}

func normalizeHandlerEntryAttachment(a model.EntryAttachment) model.EntryAttachment {
	a.Type = strings.TrimSpace(a.Type)
	a.URL = strings.TrimSpace(a.URL)
	a.Text = strings.TrimSpace(a.Text)
	a.FileName = strings.TrimSpace(a.FileName)
	a.ContentType = strings.TrimSpace(a.ContentType)
	a.Role = strings.TrimSpace(a.Role)
	a.UploadID = strings.TrimSpace(a.UploadID)
	a.Key = strings.TrimSpace(a.Key)
	a.Instruction = strings.TrimSpace(a.Instruction)
	return a
}

func validateHandlerEntryAttachment(a *model.EntryAttachment) error {
	if a == nil {
		return nil
	}
	typ := classifyHandlerEntryAttachment(*a)
	if typ == "" {
		return fmt.Errorf("unsupported attachment type")
	}
	a.Type = typ
	if typ != "text" && a.URL == "" && a.Key == "" {
		return fmt.Errorf("attachment url is required")
	}
	if typ == "text" && a.URL == "" && a.Text == "" && a.Key == "" {
		return fmt.Errorf("text attachment requires text or url")
	}
	limit := aiEntryMediaAttachmentMaxBytes
	if typ == "document" || typ == "text" {
		limit = aiEntryDocumentAttachmentMaxBytes
	}
	if a.Size > limit {
		return fmt.Errorf("attachment %s exceeds the %d MB limit", firstNonEmptyString(a.FileName, a.URL, "file"), limit/(1024*1024))
	}
	if !handlerAttachmentMetadataAllowed(typ, a.ContentType, attachmentExt(*a)) {
		return fmt.Errorf("attachment %s has unsupported content type or extension", firstNonEmptyString(a.FileName, a.URL, "file"))
	}
	return nil
}

func classifyHandlerEntryAttachment(a model.EntryAttachment) string {
	typ := strings.ToLower(strings.TrimSpace(a.Type))
	switch typ {
	case "image", "audio", "video", "document", "text":
		return typ
	case "":
	default:
		return ""
	}
	ct := strings.ToLower(strings.TrimSpace(a.ContentType))
	ext := attachmentExt(a)
	switch {
	case strings.HasPrefix(ct, "image/") || isExt(ext, ".jpg", ".jpeg", ".png", ".webp", ".gif", ".bmp"):
		return "image"
	case strings.HasPrefix(ct, "audio/") || isExt(ext, ".mp3", ".wav", ".m4a", ".aac", ".ogg"):
		return "audio"
	case strings.HasPrefix(ct, "video/") || isExt(ext, ".mp4", ".mov", ".webm"):
		return "video"
	case strings.HasPrefix(ct, "text/") || isExt(ext, ".csv", ".txt", ".md", ".markdown"):
		return "text"
	case isExt(ext, ".pdf", ".doc", ".docx", ".ppt", ".pptx", ".xls", ".xlsx", ".json") || isDocumentContentType(ct):
		return "document"
	default:
		return ""
	}
}

func handlerAttachmentMetadataAllowed(typ, contentType, ext string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	switch typ {
	case "image":
		return metadataMatches(ct, ext, []string{"image/"}, []string{".jpg", ".jpeg", ".png", ".webp", ".gif", ".bmp"})
	case "audio":
		return metadataMatches(ct, ext, []string{"audio/"}, []string{".mp3", ".wav", ".m4a", ".aac", ".ogg"})
	case "video":
		return metadataMatches(ct, ext, []string{"video/"}, []string{".mp4", ".mov", ".webm"})
	case "text":
		return metadataMatches(ct, ext, []string{"text/"}, []string{".csv", ".txt", ".md", ".markdown", ".json"})
	case "document":
		if ext != "" && !isExt(ext, ".pdf", ".doc", ".docx", ".ppt", ".pptx", ".xls", ".xlsx", ".csv", ".txt", ".md", ".markdown", ".json") {
			return false
		}
		return ct == "" || isDocumentContentType(ct) || strings.HasPrefix(ct, "text/")
	default:
		return false
	}
}

func metadataMatches(contentType, ext string, prefixes []string, exts []string) bool {
	if ext != "" && !isExt(ext, exts...) {
		return false
	}
	if contentType == "" {
		return true
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(contentType, prefix) {
			return true
		}
	}
	return false
}

func isDocumentContentType(ct string) bool {
	switch strings.ToLower(strings.TrimSpace(ct)) {
	case "",
		"application/pdf",
		"application/msword",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/vnd.ms-powerpoint",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation",
		"application/vnd.ms-excel",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/csv",
		"application/json",
		"application/octet-stream":
		return true
	default:
		return false
	}
}

func attachmentExt(a model.EntryAttachment) string {
	for _, raw := range []string{a.FileName, a.URL} {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		cut := strings.SplitN(strings.SplitN(raw, "#", 2)[0], "?", 2)[0]
		if ext := strings.ToLower(path.Ext(cut)); ext != "" {
			return ext
		}
	}
	return ""
}

func isExt(ext string, allowed ...string) bool {
	for _, item := range allowed {
		if ext == item {
			return true
		}
	}
	return false
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func validAIEntryAttachmentURL(raw string, allowPendingUploadURL bool) bool {
	if raw == "" {
		return true
	}
	if len(raw) > 2048 {
		return false
	}
	if strings.HasPrefix(raw, "/api/v1/files/") || strings.HasPrefix(raw, "/files/") {
		return true
	}
	if !allowPendingUploadURL {
		return false
	}
	cut := strings.SplitN(strings.SplitN(raw, "#", 2)[0], "?", 2)[0]
	return pendingUploadIDFromAIEntryURL(cut) != ""
}

func pendingUploadIDFromAIEntryURL(raw string) string {
	parts := strings.Split(raw, "/uploads/pending/")
	if len(parts) < 2 {
		return ""
	}
	segments := strings.Split(strings.Trim(parts[1], "/"), "/")
	if len(segments) < 2 {
		return ""
	}
	return strings.TrimSpace(segments[1])
}
