package image

import (
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/disintegration/imaging"
	"go.uber.org/zap"
)

func TestCropMargin(t *testing.T) {
	log := zap.NewNop()

	tests := []struct {
		name        string
		imgW        int
		imgH        int
		margin      int
		format      string // "png" or "jpeg"
		wantW       int
		wantH       int
		wantErr     bool
		errContains string
	}{
		{
			// hMargin=10, vMargin=round(10*80/100)=8 → 80x64
			name:   "normal crop 100x80 margin=10",
			imgW:   100, imgH: 80, margin: 10,
			format: "png",
			wantW:  80, wantH: 64,
		},
		{
			// hMargin=1, vMargin=round(1*40/50)=round(0.8)=1 → 48x38
			name:   "1px margin",
			imgW:   50, imgH: 40, margin: 1,
			format: "png",
			wantW:  48, wantH: 38,
		},
		{
			// hMargin=20, vMargin=round(20*150/200)=round(15)=15 → 160x120
			name:   "jpeg format preserved",
			imgW:   200, imgH: 150, margin: 20,
			format: "jpeg",
			wantW:  160, wantH: 120,
		},
		{
			// square: hMargin=10, vMargin=round(10*100/100)=10 → 80x80
			name:   "square 100x100 margin=10",
			imgW:   100, imgH: 100, margin: 10,
			format: "png",
			wantW:  80, wantH: 80,
		},
		{
			// landscape 1792x1024 margin=20: vMargin=round(20*1024/1792)=round(11.43)=11 → 1752x1002
			name:   "landscape 1792x1024 margin=20",
			imgW:   1792, imgH: 1024, margin: 20,
			format: "png",
			wantW:  1752, wantH: 1002,
		},
		{
			// portrait 1728x2304 margin=20: vMargin=round(20*2304/1728)=round(26.67)=27 → 1688x2250
			name:   "portrait 1728x2304 margin=20",
			imgW:   1728, imgH: 2304, margin: 20,
			format: "png",
			wantW:  1688, wantH: 2250,
		},
		{
			name:        "margin too large both dims",
			imgW:        20, imgH: 20, margin: 10,
			format:      "png",
			wantErr:     true,
			errContains: "cropped size would be",
		},
		{
			name:        "margin too large width only",
			imgW:        15, imgH: 100, margin: 8,
			format:      "png",
			wantErr:     true,
			errContains: "cropped size would be",
		},
		{
			name:        "zero margin",
			imgW:        100, imgH: 100, margin: 0,
			format:      "png",
			wantErr:     true,
			errContains: "margin must be positive",
		},
		{
			name:        "negative margin",
			imgW:        100, imgH: 100, margin: -5,
			format:      "png",
			wantErr:     true,
			errContains: "margin must be positive",
		},
		{
			name:        "file not found",
			imgW:        0, imgH: 0, margin: 10,
			format:      "nonexistent",
			wantErr:     true,
			errContains: "open image",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var inputPath string

			if tt.format == "nonexistent" {
				inputPath = filepath.Join(t.TempDir(), "no_such_file.png")
			} else {
				// Create a test image
				img := imaging.New(tt.imgW, tt.imgH, color.NRGBA{R: 200, G: 100, B: 50, A: 255})

				tmpDir := t.TempDir()
				var ext string
				switch tt.format {
				case "png":
					ext = ".png"
				default:
					ext = ".jpg"
				}
				inputPath = filepath.Join(tmpDir, "test_input"+ext)
				if err := imaging.Save(img, inputPath); err != nil {
					t.Fatalf("failed to create test image: %v", err)
				}
			}

			outPath, err := CropMargin(log, inputPath, tt.margin)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error but got nil")
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			defer os.Remove(outPath)

			// Verify output dimensions
			w, h, err := GetImageDimensions(outPath)
			if err != nil {
				t.Fatalf("failed to get output dimensions: %v", err)
			}
			if w != tt.wantW || h != tt.wantH {
				t.Errorf("got dimensions %dx%d, want %dx%d", w, h, tt.wantW, tt.wantH)
			}

			// Verify output format matches input (png→png, jpeg→jpeg)
			if tt.format == "png" && !strings.HasSuffix(outPath, ".png") {
				t.Errorf("expected PNG output, got %s", outPath)
			}
			if tt.format == "jpeg" && !strings.HasSuffix(outPath, ".jpg") {
				t.Errorf("expected JPEG output, got %s", outPath)
			}
		})
	}
}

// TestCropMargin_AspectRatioDrift verifies that the aspect ratio drift after
// cropping is less than 0.5% for a typical portrait image.
func TestCropMargin_AspectRatioDrift(t *testing.T) {
	log := zap.NewNop()

	imgW, imgH := 1728, 2304
	margin := 20

	img := imaging.New(imgW, imgH, color.NRGBA{R: 100, G: 150, B: 200, A: 255})
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "portrait.png")
	if err := imaging.Save(img, inputPath); err != nil {
		t.Fatalf("failed to create test image: %v", err)
	}

	outPath, err := CropMargin(log, inputPath, margin)
	if err != nil {
		t.Fatalf("CropMargin() error = %v", err)
	}
	defer os.Remove(outPath)

	w, h, err := GetImageDimensions(outPath)
	if err != nil {
		t.Fatalf("failed to get output dimensions: %v", err)
	}

	origRatio := float64(imgW) / float64(imgH)
	croppedRatio := float64(w) / float64(h)
	drift := math.Abs(croppedRatio-origRatio) / origRatio

	if drift >= 0.005 {
		t.Errorf("aspect ratio drift %.4f%% >= 0.5%% (orig %dx%d → cropped %dx%d)",
			drift*100, imgW, imgH, w, h)
	}
}
