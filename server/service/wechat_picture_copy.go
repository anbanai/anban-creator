package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"strings"

	"github.com/anbanai/anban-creator/server/model"
)

var ErrManualWechatPictureCopyUnavailable = errors.New("manual WeChat picture copy is unavailable")

// ManualWechatPictureHTML returns the complete server-rendered rich document
// for a finished manual picture task. It is intentionally computed from the
// frozen execution artifacts and never falls back to caption-only output.
func (s *TaskService) ManualWechatPictureHTML(ctx context.Context, taskID string) (string, error) {
	if s == nil || s.repo == nil {
		return "", fmt.Errorf("%w: task service is unavailable", ErrManualWechatPictureCopyUnavailable)
	}
	task, err := s.repo.Tasks().FindByID(ctx, strings.TrimSpace(taskID))
	if err != nil || task == nil || !taskUsesChannel(task, model.ChannelWechatPicture) {
		return "", fmt.Errorf("%w: task is not a WeChat picture task", ErrManualWechatPictureCopyUnavailable)
	}
	if !wechatDeliveryDecision(task).IsUserAssisted() {
		return "", fmt.Errorf("%w: automatic picture drafts are copied by the Server", ErrManualWechatPictureCopyUnavailable)
	}
	if task.Outcome != nil && task.Outcome.Visual.Status == model.TaskVisualPartial {
		return "", fmt.Errorf("%w: visual delivery is partial", ErrManualWechatPictureCopyUnavailable)
	}
	if task.CurrentExecutionID == nil || strings.TrimSpace(*task.CurrentExecutionID) == "" {
		return "", fmt.Errorf("%w: current execution is unavailable", ErrManualWechatPictureCopyUnavailable)
	}
	body, found, err := s.readExecutionArtifact(ctx, *task.CurrentExecutionID, "output/publish-package.json", maxTaskDeliveryJSONBytes)
	if err != nil {
		return "", fmt.Errorf("%w: read publication package: %v", ErrManualWechatPictureCopyUnavailable, err)
	}
	if !found {
		return "", fmt.Errorf("%w: publication package is missing", ErrManualWechatPictureCopyUnavailable)
	}
	var pkg picturePublicationPackage
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&pkg); err != nil || !validPicturePublicationPackage(&pkg) || pkg.Status != "ready" {
		return "", fmt.Errorf("%w: publication package is incomplete", ErrManualWechatPictureCopyUnavailable)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return "", fmt.Errorf("%w: publication package contains trailing data", ErrManualWechatPictureCopyUnavailable)
	}
	pkg.Title = normalizePicturePublicationText(pkg.Title)
	pkg.Content = normalizePicturePublicationText(pkg.Content)
	if !validPicturePublicationCopy(pkg.Title, pkg.Content) {
		return "", fmt.Errorf("%w: publication package copy fields are invalid", ErrManualWechatPictureCopyUnavailable)
	}
	files, err := s.repo.TaskFiles().FindByExecutionID(ctx, *task.CurrentExecutionID)
	if err != nil {
		return "", fmt.Errorf("%w: load picture files: %v", ErrManualWechatPictureCopyUnavailable, err)
	}
	htmlBody, err := renderManualWechatPictureHTML(&pkg, files)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrManualWechatPictureCopyUnavailable, err)
	}
	return htmlBody, nil
}

// renderManualWechatPictureHTML builds the single rich document used by the
// manual copy flow. It deliberately accepts only stable public article-image
// URLs, so a copied document never depends on a local path or expiring URL.
func renderManualWechatPictureHTML(pkg *picturePublicationPackage, files []*model.TaskFile) (string, error) {
	if pkg == nil || !validPicturePublicationCopy(pkg.Title, pkg.Content) {
		return "", fmt.Errorf("picture publication package is incomplete")
	}
	byPath := make(map[string]*model.TaskFile, len(files))
	for _, file := range files {
		if file != nil {
			byPath[file.FilePath] = file
		}
	}
	coverPath := strings.TrimSpace(pkg.CoverPath)
	if !validPicturePublicationPath(coverPath) || !strings.EqualFold(filepathBase(coverPath), "cover.png") {
		return "", fmt.Errorf("picture cover path is invalid")
	}
	paths := append([]string{coverPath}, pkg.ImagePaths...)
	if len(paths) > 20 {
		return "", fmt.Errorf("picture publication package exceeds the image limit")
	}
	var out strings.Builder
	out.WriteString(`<section style="font-family: sans-serif; line-height: 1.7;">`)
	fmt.Fprintf(&out, `<h1>%s</h1><p>`, html.EscapeString(strings.TrimSpace(pkg.Title)))
	out.WriteString(strings.ReplaceAll(strings.ReplaceAll(html.EscapeString(strings.TrimSpace(pkg.Content)), "\r\n", "\n"), "\n", "<br>"))
	out.WriteString(`</p>`)
	seenPaths := make(map[string]struct{}, len(paths))
	for index, path := range paths {
		path = strings.TrimSpace(path)
		if !validPicturePublicationPath(path) || (index > 0 && !isConcretePictureContentPath(path)) {
			return "", fmt.Errorf("picture image path %q is invalid", path)
		}
		if _, exists := seenPaths[path]; exists {
			return "", fmt.Errorf("picture image path %q is duplicated", path)
		}
		seenPaths[path] = struct{}{}
		file := byPath[strings.TrimSpace(path)]
		if !validPicturePublicationImageFile(file, index == 0) ||
			!model.IsPublicArticleImageURL(file.PlatformImageURL) {
			return "", fmt.Errorf("picture image %q is missing a stable public URL", path)
		}
		fmt.Fprintf(&out, `<p><img src="%s" alt="%s" style="display:block;max-width:100%%;height:auto;margin:16px auto;"></p>`, html.EscapeString(file.PlatformImageURL), html.EscapeString(file.FileName))
	}
	out.WriteString(`</section>`)
	return out.String(), nil
}

func filepathBase(path string) string {
	if index := strings.LastIndex(path, "/"); index >= 0 {
		return path[index+1:]
	}
	return path
}
