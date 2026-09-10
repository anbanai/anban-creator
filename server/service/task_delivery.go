package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	stdimage "image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/webp"

	"github.com/anbanai/anban-creator/server/agentpack"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/storage"
	"gorm.io/gorm"
)

var ErrTaskFileDownloadNotAllowed = errors.New("task file is not a declared delivery file")
var ErrNoDownloadableDeliveryFiles = errors.New("no downloadable delivery files found")
var ErrTaskDeliveryContractUnavailable = errors.New("task delivery contract is unavailable")
var ErrTaskDeliveryObjectInvalid = errors.New("task delivery object is invalid")

const (
	maxTaskDeliveryImageBytes     int64 = 25 << 20
	maxTaskDeliveryJSONBytes      int64 = 8 << 20
	maxTaskDeliveryImageDimension       = 16384
	maxTaskDeliveryImagePixels    int64 = 40_000_000
)

func normalizedMediaType(value string) string {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	return strings.ToLower(mediaType)
}

func deliveryMIMEForExtension(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	if value := mimeTypes[ext]; value != "" {
		return normalizedMediaType(value)
	}
	return normalizedMediaType(contentTypeForUploadExt(ext))
}

func deliveryImageFormatMIME(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	default:
		return ""
	}
}

func (s *TaskService) validateExecutionDelivery(ctx context.Context, taskID string, execution *model.TaskExecution, files []*model.TaskFile) error {
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("load task for delivery validation: %w", err)
	}
	contract, err := resolveFrozenExecutionDeliveryContract(execution)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTaskDeliveryContractUnavailable, err)
	}
	requiredArtifacts, err := resolveFrozenExecutionRequiredArtifactContract(execution)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTaskDeliveryContractUnavailable, err)
	}
	matched := 0
	for _, file := range files {
		if file == nil || file.TaskID != taskID || file.ExecutionID != execution.ID ||
			(file.State != model.TaskFileStatePending && file.State != model.TaskFileStatePublished) {
			continue
		}
		spec, pathMatched := agentpack.MatchDeliverySpec(contract, file.FilePath)
		if !pathMatched {
			continue
		}
		matched++
		if normalizedMediaType(file.MimeType) != normalizedMediaType(spec.MIMEType) {
			return fmt.Errorf("%w: %s MIME %q does not match frozen contract %q", ErrTaskDeliveryObjectInvalid, file.FilePath, file.MimeType, spec.MIMEType)
		}
		if err := s.validateStoredDeliveryObject(ctx, task, execution.ID, file, spec); err != nil {
			return err
		}
	}
	if matched == 0 {
		return fmt.Errorf("%w: execution %s has no declared delivery files", ErrTaskDeliveryObjectInvalid, execution.ID)
	}
	for _, required := range requiredArtifacts {
		requiredDelivery := agentpack.DeliverySpec{
			Role:     required.Role,
			Path:     required.Path,
			MIMEType: required.MIMEType,
		}
		var matchedFile *model.TaskFile
		for _, file := range files {
			if file == nil || file.TaskID != taskID || file.ExecutionID != execution.ID ||
				(file.State != model.TaskFileStatePending && file.State != model.TaskFileStatePublished) {
				continue
			}
			if _, ok := agentpack.MatchDeliverySpec([]agentpack.DeliverySpec{requiredDelivery}, file.FilePath); ok {
				matchedFile = file
				break
			}
		}
		if matchedFile == nil {
			return fmt.Errorf("%w: required artifact %s is missing from execution %s", ErrTaskDeliveryObjectInvalid, required.Path, execution.ID)
		}
		if normalizedMediaType(matchedFile.MimeType) != normalizedMediaType(required.MIMEType) {
			return fmt.Errorf("%w: required artifact %s MIME %q does not match frozen contract %q", ErrTaskDeliveryObjectInvalid, required.Path, matchedFile.MimeType, required.MIMEType)
		}
		if err := s.validateStoredDeliveryObject(ctx, task, execution.ID, matchedFile, requiredDelivery); err != nil {
			return err
		}
	}
	return nil
}

func (s *TaskService) validateStoredDeliveryObject(ctx context.Context, task *model.Task, executionID string, file *model.TaskFile, spec agentpack.DeliverySpec) error {
	if s.store == nil {
		return fmt.Errorf("delivery storage provider is unavailable")
	}
	if strings.TrimSpace(file.OSSKey) == "" || strings.TrimSpace(file.StorageProvider) != s.store.Name() {
		return fmt.Errorf("%w: %s has no object owned by the active storage provider", ErrTaskDeliveryObjectInvalid, file.FilePath)
	}
	owned, err := s.taskDeliveryObjectKeyOwned(ctx, task, executionID, file)
	if err != nil {
		return err
	}
	if !owned {
		return fmt.Errorf("%w: %s storage object is outside its task execution namespace", ErrTaskDeliveryObjectInvalid, file.FilePath)
	}
	if file.FileSize <= 0 || file.FileSize > maxTaskArtifactUploadBytes {
		return fmt.Errorf("%w: %s has invalid size %d", ErrTaskDeliveryObjectInvalid, file.FilePath, file.FileSize)
	}
	expectedMIME := normalizedMediaType(spec.MIMEType)
	if expectedMIME == "" || deliveryMIMEForExtension(file.FilePath) != expectedMIME {
		return fmt.Errorf("%w: %s extension does not match MIME %q", ErrTaskDeliveryObjectInvalid, file.FilePath, spec.MIMEType)
	}
	statProvider, ok := s.store.(storage.ObjectStatProvider)
	if !ok {
		return storage.ErrObjectStatUnsupported
	}
	info, err := statProvider.StatObject(ctx, file.OSSKey)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotFound) {
			return fmt.Errorf("%w: %s storage object is missing", ErrTaskDeliveryObjectInvalid, file.FilePath)
		}
		return fmt.Errorf("stat delivery object %s: %w", file.FilePath, err)
	}
	if info == nil || info.Size != file.FileSize || info.Size <= 0 {
		actualSize := int64(-1)
		if info != nil {
			actualSize = info.Size
		}
		return fmt.Errorf("%w: %s size metadata %d does not match stored size %d", ErrTaskDeliveryObjectInvalid, file.FilePath, file.FileSize, actualSize)
	}
	if objectMIME := normalizedMediaType(firstNonEmptyString(info.ContentType, info.MimeType)); objectMIME != "" && objectMIME != expectedMIME {
		return fmt.Errorf("%w: %s stored MIME %q does not match %q", ErrTaskDeliveryObjectInvalid, file.FilePath, objectMIME, expectedMIME)
	}
	if file.ContentHash != "" && info.SHA256 != "" && !strings.EqualFold(file.ContentHash, info.SHA256) {
		return fmt.Errorf("%w: %s content hash does not match stored object", ErrTaskDeliveryObjectInvalid, file.FilePath)
	}
	if expectedMIME == "application/json" {
		return s.validateStoredDeliveryJSON(ctx, file, info)
	}
	if expectedMIME == "video/mp4" {
		return s.validateStoredDeliveryMP4(ctx, file, info)
	}
	if !strings.HasPrefix(expectedMIME, "image/") {
		return nil
	}
	if file.FileSize > maxTaskDeliveryImageBytes {
		return fmt.Errorf("%w: %s image exceeds %d bytes", ErrTaskDeliveryObjectInvalid, file.FilePath, maxTaskDeliveryImageBytes)
	}
	data, err := storage.ReadObject(ctx, s.store, file.OSSKey, maxTaskDeliveryImageBytes)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotFound) || errors.Is(err, storage.ErrObjectExceedsMaxSize) {
			return fmt.Errorf("%w: %s cannot be read within image limits: %v", ErrTaskDeliveryObjectInvalid, file.FilePath, err)
		}
		return fmt.Errorf("read delivery image %s: %w", file.FilePath, err)
	}
	if int64(len(data)) != info.Size {
		return fmt.Errorf("%w: %s changed while being validated", ErrTaskDeliveryObjectInvalid, file.FilePath)
	}
	actualMIME := normalizedMediaType(http.DetectContentType(data))
	config, format, err := stdimage.DecodeConfig(bytes.NewReader(data))
	if err != nil || actualMIME != expectedMIME || deliveryImageFormatMIME(format) != expectedMIME {
		return fmt.Errorf("%w: %s does not contain a decodable %s image", ErrTaskDeliveryObjectInvalid, file.FilePath, expectedMIME)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > maxTaskDeliveryImageDimension || config.Height > maxTaskDeliveryImageDimension ||
		int64(config.Width)*int64(config.Height) > maxTaskDeliveryImagePixels {
		return fmt.Errorf("%w: %s image dimensions %dx%d exceed safety limits", ErrTaskDeliveryObjectInvalid, file.FilePath, config.Width, config.Height)
	}
	if file.ContentHash != "" {
		digest := fmt.Sprintf("%x", sha256.Sum256(data))
		if !strings.EqualFold(file.ContentHash, digest) {
			return fmt.Errorf("%w: %s content hash does not match image bytes", ErrTaskDeliveryObjectInvalid, file.FilePath)
		}
	}
	return nil
}

func (s *TaskService) taskDeliveryObjectKeyOwned(ctx context.Context, task *model.Task, executionID string, file *model.TaskFile) (bool, error) {
	if task == nil || file == nil || strings.TrimSpace(task.ID) == "" || strings.TrimSpace(task.UserID) == "" ||
		strings.TrimSpace(task.ProjectID) == "" || strings.TrimSpace(executionID) == "" ||
		file.TaskID != task.ID || file.ExecutionID != executionID {
		return false, nil
	}
	cleanPath, err := CleanTaskFileRelativePath(file.FilePath)
	if err != nil {
		return false, nil
	}
	relPath := filepath.ToSlash(cleanPath)
	key := filepath.ToSlash(strings.TrimSpace(file.OSSKey))
	mcpPrefix := buildTaskMCPArtifactStoragePrefix(task, executionID)
	if key == mcpPrefix+relPath {
		return true, nil
	}

	hash := strings.ToLower(strings.TrimSpace(file.ContentHash))
	if isExactLowerHex(hash, 64) {
		if key == buildTaskArtifactFinalStorageKey(task, executionID, hash, relPath) {
			return true, nil
		}
		ext := filepath.Ext(relPath)
		stem := strings.TrimSuffix(mcpPrefix+relPath, ext)
		prefix := stem + "-" + hash + "-"
		if strings.HasPrefix(key, prefix) && strings.HasSuffix(key, ext) {
			nonce := strings.TrimSuffix(strings.TrimPrefix(key, prefix), ext)
			if isExactLowerHex(nonce, 32) {
				return true, nil
			}
		}
	}

	operationPrefix := mcpPrefix + "operation-objects/"
	if !strings.HasPrefix(key, operationPrefix) || !isImmutableOperationObjectKey(key) {
		return false, nil
	}
	settlements, err := s.repo.Billing().ListSettlementsByTask(ctx, task.ID)
	if err != nil {
		return false, fmt.Errorf("validate delivery operation object ownership: %w", err)
	}
	for _, settlement := range settlements {
		if settlement.ResourceID != file.ID || settlement.TaskID == nil || *settlement.TaskID != task.ID ||
			settlement.AttemptID == nil || *settlement.AttemptID != executionID {
			continue
		}
		var snapshot struct {
			OperationObject taskFileOperationObjectSnapshot `json:"operation_object"`
		}
		if err := json.Unmarshal(settlement.ResultSnapshot, &snapshot); err != nil {
			return false, fmt.Errorf("validate delivery operation object settlement %s: %w", settlement.ID, err)
		}
		object := snapshot.OperationObject
		return object.StorageProvider == file.StorageProvider && object.OSSKey == file.OSSKey &&
			object.FilePath == file.FilePath && object.FileName == file.FileName &&
			normalizedMediaType(object.MimeType) == normalizedMediaType(file.MimeType) &&
			object.FileSize == file.FileSize && strings.EqualFold(object.ContentHash, file.ContentHash), nil
	}
	return false, nil
}

func isExactLowerHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func (s *TaskService) validateStoredDeliveryJSON(ctx context.Context, file *model.TaskFile, info *storage.ObjectInfo) error {
	if file.FileSize > maxTaskDeliveryJSONBytes {
		return fmt.Errorf("%w: %s JSON exceeds %d bytes", ErrTaskDeliveryObjectInvalid, file.FilePath, maxTaskDeliveryJSONBytes)
	}
	data, err := storage.ReadObject(ctx, s.store, file.OSSKey, maxTaskDeliveryJSONBytes)
	if err != nil {
		return fmt.Errorf("%w: %s JSON cannot be read within limits: %v", ErrTaskDeliveryObjectInvalid, file.FilePath, err)
	}
	if int64(len(data)) != info.Size {
		return fmt.Errorf("%w: %s changed while being validated", ErrTaskDeliveryObjectInvalid, file.FilePath)
	}
	if !json.Valid(data) {
		return fmt.Errorf("%w: %s does not contain valid JSON", ErrTaskDeliveryObjectInvalid, file.FilePath)
	}
	if file.ContentHash != "" {
		digest := fmt.Sprintf("%x", sha256.Sum256(data))
		if !strings.EqualFold(file.ContentHash, digest) {
			return fmt.Errorf("%w: %s content hash does not match JSON bytes", ErrTaskDeliveryObjectInvalid, file.FilePath)
		}
	}
	return nil
}

func (s *TaskService) validateStoredDeliveryMP4(ctx context.Context, file *model.TaskFile, info *storage.ObjectInfo) error {
	opener, ok := s.store.(storage.ObjectStreamProvider)
	if !ok {
		return fmt.Errorf("%w: %s cannot be streamed for MP4 validation", ErrTaskDeliveryObjectInvalid, file.FilePath)
	}
	stream, err := opener.OpenObject(ctx, file.OSSKey)
	if err != nil {
		return fmt.Errorf("%w: open %s for MP4 validation: %v", ErrTaskDeliveryObjectInvalid, file.FilePath, err)
	}
	defer stream.Close()
	header := make([]byte, 24)
	n, readErr := io.ReadFull(stream, header)
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return fmt.Errorf("%w: read %s MP4 header: %v", ErrTaskDeliveryObjectInvalid, file.FilePath, readErr)
	}
	header = header[:n]
	if !validMP4FileTypeHeader(header, info.Size) {
		return fmt.Errorf("%w: %s does not contain a valid MP4 file-type box", ErrTaskDeliveryObjectInvalid, file.FilePath)
	}
	return nil
}

func validMP4FileTypeHeader(header []byte, objectSize int64) bool {
	if len(header) < 16 || objectSize < 16 || string(header[4:8]) != "ftyp" {
		return false
	}
	boxSize := uint64(binary.BigEndian.Uint32(header[:4]))
	brandOffset := 8
	if boxSize == 1 {
		if len(header) < 24 {
			return false
		}
		boxSize = binary.BigEndian.Uint64(header[8:16])
		brandOffset = 16
	}
	minimumSize := uint64(16)
	if brandOffset == 16 {
		minimumSize = 24
	}
	if boxSize < minimumSize || boxSize > uint64(objectSize) || len(header) < brandOffset+4 {
		return false
	}
	brand := header[brandOffset : brandOffset+4]
	nonSpace := false
	for _, value := range brand {
		if value < 0x20 || value > 0x7e {
			return false
		}
		if value != ' ' {
			nonSpace = true
		}
	}
	return nonSpace
}

func (s *TaskService) deliveryContractForExecution(ctx context.Context, taskID, executionID string) ([]agentpack.DeliverySpec, error) {
	executionID = strings.TrimSpace(executionID)
	if executionID == "" {
		return nil, fmt.Errorf("%w: task file has no owning execution", ErrTaskDeliveryContractUnavailable)
	}
	execution, err := s.repo.TaskExecutions().FindByID(ctx, executionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: execution %s was not found", ErrTaskDeliveryContractUnavailable, executionID)
		}
		return nil, fmt.Errorf("find execution for delivery contract: %w", err)
	}
	if execution.TaskID != taskID {
		return nil, fmt.Errorf("%w: execution %s does not belong to task %s", ErrTaskDeliveryContractUnavailable, executionID, taskID)
	}
	contract, err := resolveFrozenExecutionDeliveryContract(execution)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTaskDeliveryContractUnavailable, err)
	}
	return contract, nil
}

func (s *TaskService) deliveryContractsForFiles(ctx context.Context, taskID string, files []*model.TaskFile) (map[string][]agentpack.DeliverySpec, error) {
	contracts := make(map[string][]agentpack.DeliverySpec)
	resolved := make(map[string]struct{})
	for _, file := range files {
		if file == nil || file.State != model.TaskFileStatePublished || file.TaskID != taskID {
			continue
		}
		executionID := strings.TrimSpace(file.ExecutionID)
		if _, ok := resolved[executionID]; ok {
			continue
		}
		resolved[executionID] = struct{}{}
		contract, err := s.deliveryContractForExecution(ctx, taskID, executionID)
		if err != nil {
			if errors.Is(err, ErrTaskDeliveryContractUnavailable) {
				continue
			}
			return nil, err
		}
		contracts[executionID] = contract
	}
	return contracts, nil
}

// DeliveryMetadata computes the authoritative user-facing status for a task
// file. Collected files from failed executions are always process artifacts.
func (s *TaskService) DeliveryMetadata(ctx context.Context, taskID string, file *model.TaskFile) (string, bool, error) {
	if file == nil || file.TaskID != taskID || file.State != model.TaskFileStatePublished {
		return "", false, nil
	}
	contract, err := s.deliveryContractForExecution(ctx, taskID, file.ExecutionID)
	if err != nil {
		if errors.Is(err, ErrTaskDeliveryContractUnavailable) {
			return "", false, nil
		}
		return "", false, err
	}
	role, deliverable := deliveryMetadataFromContract(contract, file)
	return role, deliverable, nil
}

func deliveryMetadataFromContract(contract []agentpack.DeliverySpec, file *model.TaskFile) (string, bool) {
	if file == nil || file.State != model.TaskFileStatePublished {
		return "", false
	}
	return agentpack.MatchDeliveryPath(contract, filepath.ToSlash(file.FilePath))
}

func taskFilePreviewURL(taskID, fileID string) string {
	return "/api/v1/tasks/" + url.PathEscape(taskID) + "/files/" + url.PathEscape(fileID) + "/preview"
}

func taskFileDownloadURL(taskID, fileID string) string {
	return "/api/v1/tasks/" + url.PathEscape(taskID) + "/files/" + url.PathEscape(fileID) + "/download"
}

func taskFileDirectPreviewURL(store storage.Provider, file *model.TaskFile) string {
	if file == nil || !(strings.HasPrefix(file.MimeType, "video/") || strings.HasPrefix(file.MimeType, "audio/")) {
		return ""
	}
	if !isHTTPSOwnedStorageURL(store, file.URL) {
		return ""
	}
	return strings.TrimSpace(file.URL)
}

func isHTTPSOwnedStorageURL(store storage.Provider, rawURL string) bool {
	if store == nil {
		return false
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	return err == nil && u.Scheme == "https" && u.Host != "" && store.IsOwnedURL(u.String())
}

// EnrichFilesWithDeliveryMetadata adds API URLs and delivery status without
// changing the persisted task-file record.
func (s *TaskService) EnrichFilesWithDeliveryMetadata(ctx context.Context, taskID string, files []*model.TaskFile) error {
	contracts, err := s.deliveryContractsForFiles(ctx, taskID, files)
	if err != nil {
		return err
	}
	for _, file := range files {
		if file == nil {
			continue
		}
		role, deliverable := deliveryMetadataFromContract(contracts[strings.TrimSpace(file.ExecutionID)], file)
		file.IsDeliverable = deliverable
		file.DeliveryRole = role
		file.PreviewURL = taskFilePreviewURL(taskID, file.ID)
		if deliverable {
			file.DownloadURL = taskFileDownloadURL(taskID, file.ID)
			if signer, ok := s.store.(storage.AttachmentDownloadURLProvider); ok && strings.TrimSpace(file.OSSKey) != "" {
				signedURL, signErr := signer.DownloadAttachmentURL(ctx, file.OSSKey, file.FileName, 3600)
				if signErr != nil {
					s.logger.Warn().Err(signErr).
						Str("file_id", file.ID).
						Str("oss_key", file.OSSKey).
						Msg("failed to sign direct delivery download URL, falling back to API")
				} else if isHTTPSOwnedStorageURL(s.store, signedURL) {
					file.DownloadURL = signedURL
				}
			}
			if directURL := taskFileDirectPreviewURL(s.store, file); directURL != "" {
				file.PreviewURL = directURL
			}
		} else {
			// Never expose a storage URL for process files: public/custom-domain
			// URLs would bypass the task-scoped preview/download policy.
			file.URL = ""
			file.DownloadURL = ""
			file.MediaID = ""
			file.WechatURL = ""
		}
	}
	return nil
}

func (s *TaskService) RequireDownloadableTaskFile(ctx context.Context, taskID string, file *model.TaskFile) error {
	_, deliverable, err := s.DeliveryMetadata(ctx, taskID, file)
	if err != nil {
		return err
	}
	if !deliverable {
		return ErrTaskFileDownloadNotAllowed
	}
	return nil
}

func (s *TaskService) FilterDeliverableFiles(ctx context.Context, taskID string, files []*model.TaskFile) ([]*model.TaskFile, error) {
	result := make([]*model.TaskFile, 0, len(files))
	contracts, err := s.deliveryContractsForFiles(ctx, taskID, files)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if file == nil || file.TaskID != taskID {
			continue
		}
		_, deliverable := deliveryMetadataFromContract(contracts[strings.TrimSpace(file.ExecutionID)], file)
		if deliverable {
			result = append(result, file)
		}
	}
	return result, nil
}
