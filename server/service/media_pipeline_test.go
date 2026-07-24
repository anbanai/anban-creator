package service

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestMediaPipelineStatusReportsAtomicReadiness(t *testing.T) {
	tests := []struct {
		name             string
		storeName        string
		tingwuConfigured bool
		want             MediaPipelineStatusResult
	}{
		{
			name:             "fully configured",
			storeName:        "oss",
			tingwuConfigured: true,
			want: MediaPipelineStatusResult{
				StorageConfigured: true, OSSDirectUpload: true, TingWuConfigured: true,
				Missing: []string{},
			},
		},
		{
			name:      "local storage and no tingwu",
			storeName: "local",
			want: MediaPipelineStatusResult{
				StorageConfigured: true,
				Missing: []string{
					"oss direct-upload storage",
					"tingwu endpoint/region/app_key/access_key/access_secret",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewMediaPipelineService(&fakeTaskStorage{name: tt.storeName}, tt.tingwuConfigured)
			got := svc.Status(context.Background(), MediaPipelineStatusRequest{})
			if got.StorageConfigured != tt.want.StorageConfigured ||
				got.OSSDirectUpload != tt.want.OSSDirectUpload ||
				got.TingWuConfigured != tt.want.TingWuConfigured ||
				!reflect.DeepEqual(got.Missing, tt.want.Missing) {
				t.Fatalf("Status = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestMediaPipelineStatusPublicResultContainsStateOnly(t *testing.T) {
	for _, result := range []*MediaPipelineStatusResult{
		NewMediaPipelineService(&fakeTaskStorage{name: "oss"}, true).Status(context.Background(), MediaPipelineStatusRequest{}),
		NewMediaPipelineService(&fakeTaskStorage{name: "local"}, false).Status(context.Background(), MediaPipelineStatusRequest{}),
	} {
		raw, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		payload := strings.ToLower(string(raw))
		for _, forbidden := range []string{"hints", "prepare_file_upload", "create_live_analysis_task", "then pass"} {
			if strings.Contains(payload, forbidden) {
				t.Fatalf("media pipeline public result contains workflow orchestration %q: %s", forbidden, raw)
			}
		}
	}
}
