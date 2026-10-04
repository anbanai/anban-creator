package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	appconfig "github.com/anbanai/anban-creator/server/app/config"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	wechatutil "github.com/silenceper/wechat/v2/util"
	"gorm.io/datatypes"
)

type imageUploadTestTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (r imageUploadTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host != "api.weixin.qq.com" {
		return nil, fmt.Errorf("unexpected upload host %q", req.URL.Host)
	}
	clone := req.Clone(req.Context())
	clone.URL.Scheme, clone.URL.Host = r.target.Scheme, r.target.Host
	return r.base.RoundTrip(clone)
}

func TestTaskImageUploadUsesTaskChannelCredentials(t *testing.T) {
	for _, tc := range []struct {
		name, platform, channel, taskType string
		missingCredentials                bool
		noImageConfig, emptyImageConfig   bool
	}{
		{name: "article with migrated credentials", platform: model.PlatformWechat, channel: model.ChannelArticle, taskType: model.TaskTypeWechatArticle},
		{name: "article without generation config", platform: model.PlatformWechat, channel: model.ChannelArticle, taskType: model.TaskTypeWechatArticle, noImageConfig: true},
		{name: "article with empty generation config", platform: model.PlatformWechat, channel: model.ChannelArticle, taskType: model.TaskTypeWechatArticle, emptyImageConfig: true},
		{name: "article on shared project", platform: model.PlatformSeednote, channel: model.ChannelArticle, taskType: model.TaskTypeWechatArticle},
		{name: "picture uses its own connector", platform: model.PlatformSeednote, channel: model.ChannelWechatPicture, taskType: model.TaskTypeWechatPicture},
		{name: "seednote uses storage on wechat project", platform: model.PlatformWechat, channel: model.ChannelSeednote, taskType: model.PlatformSeednote},
		{name: "picture cannot borrow article credentials", platform: model.PlatformSeednote, channel: model.ChannelWechatPicture, taskType: model.TaskTypeWechatPicture, missingCredentials: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			f := newTaskImageOperationsCropFixture(t)
			project, err := f.repo.Projects().FindByID(ctx, f.task.ProjectID)
			if err != nil {
				t.Fatal(err)
			}
			project.Platform = tc.platform
			if err := f.repo.Projects().Update(ctx, project); err != nil {
				t.Fatal(err)
			}
			f.task.Channel, f.task.Type = tc.channel, tc.taskType
			if err := f.repo.Tasks().Update(ctx, f.task); err != nil {
				t.Fatal(err)
			}
			for _, channel := range []string{model.ChannelArticle, model.ChannelWechatPicture} {
				if tc.missingCredentials && channel == tc.channel {
					continue
				}
				row := &model.ProjectChannelConfig{ID: uuid.NewString(), ProjectID: project.ID, Channel: channel,
					Config: datatypes.NewJSONType(map[string]any{"wechat_app_id": "test-" + channel, "wechat_secret": "secret-" + channel})}
				if err := f.repo.ProjectChannelConfigs().Upsert(ctx, row); err != nil {
					t.Fatal(err)
				}
			}

			var tokenCalls, uploadCalls int
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/cgi-bin/token":
					tokenCalls++
					if r.URL.Query().Get("appid") != "test-"+tc.channel || r.URL.Query().Get("secret") != "secret-"+tc.channel {
						t.Error("upload used credentials from the wrong channel")
					}
					io.WriteString(w, `{"access_token":"test-token","expires_in":7200}`)
				case "/cgi-bin/material/add_material":
					uploadCalls++
					if r.URL.Query().Get("access_token") != "test-token" {
						t.Error("missing upload token")
					}
					file, _, err := r.FormFile("media")
					if err != nil {
						t.Error(err)
					} else {
						file.Close()
					}
					io.WriteString(w, `{"media_id":"test-media","url":"https://wechat.example/image.png"}`)
				default:
					t.Errorf("unexpected WeChat endpoint %s", r.URL.Path)
					http.Error(w, "unexpected endpoint", http.StatusNotFound)
				}
			}))
			t.Cleanup(api.Close)
			target, _ := url.Parse(api.URL)
			previousClient := wechatutil.DefaultHTTPClient
			wechatutil.DefaultHTTPClient = &http.Client{Transport: imageUploadTestTransport{target: target, base: api.Client().Transport}}
			t.Cleanup(func() { wechatutil.DefaultHTTPClient = previousClient })

			logger := zerolog.Nop()
			imageConfig := &srvconfig.ImageAPIConfig{API: &appconfig.ImageAPI{MaxWidth: 1920}}
			if tc.noImageConfig {
				imageConfig = nil
			} else if tc.emptyImageConfig {
				imageConfig = &srvconfig.ImageAPIConfig{}
			}
			f.service.images = NewImageService(imageConfig, f.store, f.repo, &logger)
			result, err := f.service.Upload(ctx, UploadTaskImageRequest{
				UserID: f.userID, ExecutionID: f.executionID, ProjectID: project.ID, TaskID: f.task.ID, FilePath: "output/source-a.png",
			})
			if tc.missingCredentials {
				if err == nil || !strings.Contains(err.Error(), "credentials") {
					t.Fatalf("missing task channel credentials: result=%#v error=%v", result, err)
				}
				if tokenCalls != 0 || uploadCalls != 0 {
					t.Fatal("missing channel credentials reached WeChat")
				}
				return
			}
			if err != nil {
				t.Fatalf("upload with configured channel: %v", err)
			}
			if tc.channel == model.ChannelSeednote {
				if result.URL == "" || result.MediaID != "" || result.WechatURL != "" || tokenCalls != 0 || uploadCalls != 0 {
					t.Fatalf("storage upload result=%#v token_calls=%d upload_calls=%d", result, tokenCalls, uploadCalls)
				}
				return
			}
			if result.MediaID != "test-media" || result.WechatURL != "https://wechat.example/image.png" || tokenCalls != 1 || uploadCalls != 1 {
				t.Fatalf("WeChat upload result=%#v token_calls=%d upload_calls=%d", result, tokenCalls, uploadCalls)
			}
			file := findTaskImageOperationOutputFile(t, f.repo, f.executionID, "output/source-a.png")
			if file.MediaID != result.MediaID || file.WechatURL != result.WechatURL {
				t.Fatal("WeChat upload metadata was not persisted on the execution file")
			}
		})
	}
}
