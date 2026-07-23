package service

import (
	"context"
	"reflect"
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
					"oss storage for prepare_file_upload direct uploads",
					"tingwu endpoint/region/app_key/access_key/access_secret for live-slicer",
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
