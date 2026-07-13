package handler

import (
	"context"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

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

func validateInputAttachments(ctx context.Context, pending service.PendingUploadRepository, userID string, attachments []model.EntryAttachment, options InputAttachmentValidationOptions) ([]model.EntryAttachment, error) {
	if options.MaxCount > 0 && len(attachments) > options.MaxCount {
		return nil, fmt.Errorf("at most %d attachments are allowed", options.MaxCount)
	}
	normalized := make([]model.EntryAttachment, len(attachments))
	urls := make([]string, 0, len(attachments))
	for i, raw := range attachments {
		a := normalizeHandlerEntryAttachment(raw)
		if utf8.RuneCountInString(a.Instruction) > inputAttachmentInstructionMaxRunes {
			return nil, fmt.Errorf("attachment instruction must not exceed %d characters", inputAttachmentInstructionMaxRunes)
		}
		if err := validateHandlerEntryAttachment(&a); err != nil {
			return nil, fmt.Errorf("attachment %d: %w", i+1, err)
		}
		if len(options.AllowedTypes) > 0 && !options.AllowedTypes[a.Type] {
			return nil, fmt.Errorf("attachment type %s is not allowed", a.Type)
		}
		if a.URL != "" {
			if !validAIEntryAttachmentURL(a.URL, pending != nil) {
				return nil, fmt.Errorf("attachment URLs must be internal file URLs or registered pending-upload URLs")
			}
			urls = append(urls, a.URL)
		}
		normalized[i] = a
	}
	if pending != nil {
		if err := finalizePendingURLs(ctx, pending, userID, service.DirectUploadPurposeAIEntryAttachment, urls); err != nil {
			return nil, err
		}
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
	if typ != "text" && a.URL == "" {
		return fmt.Errorf("attachment url is required")
	}
	if typ == "text" && a.URL == "" && a.Text == "" {
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
