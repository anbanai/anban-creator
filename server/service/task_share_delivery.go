package service

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/anbanai/anban-creator/server/agentpack"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/storage"
	"gorm.io/gorm"
)

type TaskShareImage struct {
	ID   string `json:"id"`
	URL  string `json:"url"`
	Alt  string `json:"alt"`
	Role string `json:"role"`
}

type TaskShareContent struct {
	Type      string           `json:"type"`
	Title     string           `json:"title"`
	Format    string           `json:"format"`
	Body      string           `json:"body"`
	Images    []TaskShareImage `json:"images"`
	ExpiresAt time.Time        `json:"expires_at"`
}

type TaskShareImageStream struct {
	Body     io.ReadCloser
	MIMEType string
	Size     int64
}

type shareDeliveryFile struct {
	file *model.TaskFile
	role string
}

type taskShareDelivery struct {
	body   *model.TaskFile
	images []shareDeliveryFile
}

func shareImageURL(token, fileID string) string {
	return "/api/v1/public/shares/" + url.PathEscape(token) + "/images/" + url.PathEscape(fileID)
}

func shareStorageError(err error) error {
	if errors.Is(err, storage.ErrObjectNotFound) || errors.Is(err, storage.ErrObjectExceedsMaxSize) {
		return ErrTaskShareUnavailable
	}
	return err
}

func (s *TaskShareService) delivery(ctx context.Context, task *model.Task) (*taskShareDelivery, error) {
	files, err := s.repo.TaskFiles().FindByTaskID(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 || files[0].ExecutionID == "" {
		return nil, ErrTaskShareUnavailable
	}
	executionID := files[0].ExecutionID
	execution, err := s.repo.TaskExecutions().FindByID(ctx, executionID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTaskShareUnavailable
	}
	if err != nil {
		return nil, err
	}
	if execution.TaskID != task.ID || execution.ManifestStatus != model.TaskExecutionManifestDelivered {
		return nil, ErrTaskShareUnavailable
	}
	contract, err := resolveFrozenExecutionDeliveryContract(execution)
	if err != nil {
		return nil, ErrTaskShareUnavailable
	}
	result := &taskShareDelivery{}
	bodyRole, bodyMIME := "final_markdown", "text/markdown"
	if task.Channel == model.ChannelArticle {
		bodyRole, bodyMIME = "html", "text/html"
	}
	for _, file := range files {
		if file.TaskID != task.ID || file.ExecutionID != executionID || file.State != model.TaskFileStateDelivered {
			return nil, ErrTaskShareUnavailable
		}
		spec, matched := agentpack.MatchDeliverySpec(contract, file.FilePath)
		if !matched || spec.InternalOnly {
			continue
		}
		isBody := spec.Role == bodyRole && normalizedMediaType(spec.MIMEType) == bodyMIME
		isImage := spec.Role == "image" || spec.Role == "cover" || spec.Role == "tail"
		if !isBody && !isImage {
			continue
		}
		if normalizedMediaType(file.MimeType) != normalizedMediaType(spec.MIMEType) {
			return nil, ErrTaskShareUnavailable
		}
		if isBody {
			if result.body != nil {
				return nil, ErrTaskShareUnavailable
			}
			result.body = file
		} else {
			switch normalizedMediaType(file.MimeType) {
			case "image/png", "image/jpeg", "image/gif", "image/webp":
			default:
				return nil, ErrTaskShareUnavailable
			}
			result.images = append(result.images, shareDeliveryFile{file: file, role: spec.Role})
		}
	}
	if result.body == nil {
		return nil, ErrTaskShareUnavailable
	}
	sort.Slice(result.images, func(i, j int) bool {
		a, b := result.images[i], result.images[j]
		rank := func(role string) int {
			if role == "cover" {
				return 0
			}
			if role == "tail" {
				return 2
			}
			return 1
		}
		if rank(a.role) != rank(b.role) {
			return rank(a.role) < rank(b.role)
		}
		ai, bi := shareImageNumber(a.file.FilePath), shareImageNumber(b.file.FilePath)
		if ai != bi {
			return ai < bi
		}
		return a.file.FilePath < b.file.FilePath
	})
	return result, nil
}

func shareImageNumber(filename string) int {
	name := strings.TrimSuffix(path.Base(filename), path.Ext(filename))
	start := len(name)
	for start > 0 && name[start-1] >= '0' && name[start-1] <= '9' {
		start--
	}
	n, _ := strconv.Atoi(name[start:])
	return n
}

func (s *TaskShareService) statFile(ctx context.Context, task *model.Task, file *model.TaskFile, limit int64) error {
	if s.store == nil {
		return errors.New("share storage is unavailable")
	}
	if file.OSSKey == "" || file.StorageProvider != s.store.Name() || file.FileSize <= 0 || file.FileSize > limit {
		return ErrTaskShareUnavailable
	}
	owned, err := (&TaskService{repo: s.repo}).taskDeliveryObjectKeyOwned(ctx, task, file.ExecutionID, file)
	if err != nil {
		return err
	}
	if !owned {
		return ErrTaskShareUnavailable
	}
	stat, ok := s.store.(storage.ObjectStatProvider)
	if !ok {
		return storage.ErrObjectStatUnsupported
	}
	info, err := stat.StatObject(ctx, file.OSSKey)
	if err != nil {
		return shareStorageError(err)
	}
	if info == nil || info.Size != file.FileSize || (normalizedMediaType(firstNonEmptyString(info.ContentType, info.MimeType)) != "" && normalizedMediaType(firstNonEmptyString(info.ContentType, info.MimeType)) != normalizedMediaType(file.MimeType)) {
		return ErrTaskShareUnavailable
	}
	if file.ContentHash != "" && info.SHA256 != "" && !strings.EqualFold(file.ContentHash, info.SHA256) {
		return ErrTaskShareUnavailable
	}
	return nil
}

type shareImageReader struct {
	io.Reader
	io.Closer
}

func (s *TaskShareService) openImage(ctx context.Context, task *model.Task, file *model.TaskFile) (*TaskShareImageStream, error) {
	if err := s.statFile(ctx, task, file, maxTaskDeliveryImageBytes); err != nil {
		return nil, err
	}
	var body io.ReadCloser
	var err error
	if opener, ok := s.store.(storage.ObjectStreamProvider); ok {
		body, err = opener.OpenObject(ctx, file.OSSKey)
	} else {
		var data []byte
		data, err = storage.ReadObject(ctx, s.store, file.OSSKey, maxTaskDeliveryImageBytes)
		if err == nil {
			body = io.NopCloser(bytes.NewReader(data))
		}
	}
	if err != nil {
		return nil, shareStorageError(err)
	}
	reader := bufio.NewReader(body)
	header, err := reader.Peek(512)
	if err != nil && err != io.EOF {
		_ = body.Close()
		return nil, shareStorageError(err)
	}
	mimeType := normalizedMediaType(file.MimeType)
	if http.DetectContentType(header) != mimeType {
		_ = body.Close()
		return nil, ErrTaskShareUnavailable
	}
	return &TaskShareImageStream{Body: &shareImageReader{Reader: io.LimitReader(reader, file.FileSize), Closer: body}, MIMEType: mimeType, Size: file.FileSize}, nil
}

func (s *TaskShareService) Public(ctx context.Context, token string) (*TaskShareContent, error) {
	share, task, err := s.publicTask(ctx, token)
	if err != nil {
		return nil, err
	}
	return s.content(ctx, task, token, share.ExpiresAt)
}

func (s *TaskShareService) content(ctx context.Context, task *model.Task, token string, expiresAt time.Time) (*TaskShareContent, error) {
	delivery, err := s.delivery(ctx, task)
	if err != nil {
		return nil, err
	}
	if err := s.statFile(ctx, task, delivery.body, maxTaskDeliveryHTMLBytes); err != nil {
		return nil, err
	}
	data, err := storage.ReadObject(ctx, s.store, delivery.body.OSSKey, maxTaskDeliveryHTMLBytes)
	if err != nil {
		return nil, shareStorageError(err)
	}
	if int64(len(data)) != delivery.body.FileSize || !utf8.Valid(data) || len(bytes.TrimSpace(data)) == 0 {
		return nil, ErrTaskShareUnavailable
	}
	result := &TaskShareContent{Type: task.Channel, Title: task.Title, Format: "markdown", Images: []TaskShareImage{}, ExpiresAt: expiresAt}
	for _, image := range delivery.images {
		stream, err := s.openImage(ctx, task, image.file)
		if err != nil {
			return nil, err
		}
		_ = stream.Body.Close()
		result.Images = append(result.Images, TaskShareImage{ID: image.file.ID, URL: shareImageURL(token, image.file.ID), Alt: image.file.FileName, Role: image.role})
	}
	resolve := s.imageResolver(token, delivery)
	if task.Channel == model.ChannelArticle {
		result.Format = "html"
		result.Body, err = rewriteShareHTML(data, resolve)
	} else {
		result.Body, err = rewriteShareMarkdown(data, resolve)
	}
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *TaskShareService) Image(ctx context.Context, token, fileID string) (*TaskShareImageStream, error) {
	_, task, err := s.publicTask(ctx, token)
	if err != nil {
		return nil, err
	}
	delivery, err := s.delivery(ctx, task)
	if err != nil {
		return nil, err
	}
	for _, image := range delivery.images {
		if image.file.ID == fileID {
			return s.openImage(ctx, task, image.file)
		}
	}
	return nil, ErrTaskShareNotFound
}

func (s *TaskShareService) imageResolver(token string, delivery *taskShareDelivery) func(string) (string, error) {
	return func(source string) (string, error) {
		source = strings.TrimSpace(source)
		parsed, err := url.Parse(source)
		if err != nil || source == "" {
			return "", ErrTaskShareUnavailable
		}
		for _, image := range delivery.images {
			file := image.file
			mapped := shareImageURL(token, file.ID)
			for _, known := range []string{file.PlatformImageURL, file.WechatURL, file.OSSURL, file.URL, mapped, taskFilePreviewURL(file.TaskID, file.ID), taskFileDownloadURL(file.TaskID, file.ID), s.store.GetURL(file.OSSKey)} {
				if known != "" && source == known {
					return mapped, nil
				}
			}
			if parsed.Scheme == "" && parsed.Host == "" && parsed.RawQuery == "" && parsed.Fragment == "" && !strings.HasPrefix(parsed.Path, "/") {
				if path.Clean(parsed.Path) == file.FilePath || path.Join(path.Dir(delivery.body.FilePath), parsed.Path) == file.FilePath {
					return mapped, nil
				}
			}
		}
		return "", ErrTaskShareUnavailable
	}
}
