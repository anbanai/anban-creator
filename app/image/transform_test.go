package image

import (
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/disintegration/imaging"
)

// makeSourcePNG writes a solid-color PNG of the given size to a temp file and
// returns its path.
func makeSourcePNG(t *testing.T, w, h int) string {
	t.Helper()
	img := imaging.New(w, h, color.White)
	path := filepath.Join(t.TempDir(), "src.png")
	if err := imaging.Save(img, path); err != nil {
		t.Fatalf("save source: %v", err)
	}
	return path
}

func TestCropToSize_ExactDimensions(t *testing.T) {
	cases := []struct {
		name       string
		srcW, srcH int
	}{
		{"square", 2048, 2048},
		{"wide_21_9_hint", 3024, 1296}, // nearest supported ratio the skill requests
		{"tall", 1296, 3024},
		{"already_target", WeChatCoverWidth, WeChatCoverHeight},
		{"large_landscape", 4000, 2000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := makeSourcePNG(t, tc.srcW, tc.srcH)
			if err := CropToSize(path, WeChatCoverWidth, WeChatCoverHeight); err != nil {
				t.Fatalf("CropToSize: %v", err)
			}
			w, h, err := GetImageDimensions(path)
			if err != nil {
				t.Fatalf("GetImageDimensions: %v", err)
			}
			if w != WeChatCoverWidth || h != WeChatCoverHeight {
				t.Fatalf("got %dx%d, want %dx%d", w, h, WeChatCoverWidth, WeChatCoverHeight)
			}
		})
	}
}

func TestCropToSize_PreservesCenterSubject(t *testing.T) {
	// White image with a large red block in the dead center. After a center
	// crop to 2.35:1 + resize, the center of the output must still be red —
	// proving a centered subject survives the crop (the 1:1 forward-card crop
	// relies on this).
	srcW, srcH := 2048, 2048
	img := imaging.New(srcW, srcH, color.White)
	blockW, blockH := 400, 400
	x0 := (srcW - blockW) / 2
	y0 := (srcH - blockH) / 2
	for y := y0; y < y0+blockH; y++ {
		for x := x0; x < x0+blockW; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
		}
	}
	path := filepath.Join(t.TempDir(), "center.png")
	if err := imaging.Save(img, path); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := CropToSize(path, WeChatCoverWidth, WeChatCoverHeight); err != nil {
		t.Fatalf("CropToSize: %v", err)
	}
	out, err := imaging.Open(path)
	if err != nil {
		t.Fatalf("open cropped: %v", err)
	}
	b := out.Bounds()
	cx, cy := b.Dx()/2, b.Dy()/2
	r, g, _, _ := out.At(cx, cy).RGBA()
	// Center must stay red-dominant (Lanczos blends edges, but the block center
	// is far from any edge).
	if r>>8 < 200 || g>>8 > 80 {
		t.Fatalf("center subject not preserved: at(%d,%d) R=%d G=%d", cx, cy, r>>8, g>>8)
	}
}

func TestCropToSize_OverwritesOriginalAtomically(t *testing.T) {
	path := makeSourcePNG(t, 3024, 1296)
	if err := CropToSize(path, WeChatCoverWidth, WeChatCoverHeight); err != nil {
		t.Fatalf("CropToSize: %v", err)
	}
	// No leftover temp files in the directory.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(path) {
			t.Fatalf("unexpected leftover file: %s", e.Name())
		}
	}
}

func TestCropToSize_InvalidTarget(t *testing.T) {
	path := makeSourcePNG(t, 2048, 2048)
	if err := CropToSize(path, 0, 383); err == nil {
		t.Fatal("expected error for zero width")
	}
}

func TestCropToSize_MissingFile(t *testing.T) {
	if err := CropToSize(filepath.Join(t.TempDir(), "nope.png"), 900, 383); err == nil {
		t.Fatal("expected error for missing file")
	}
}
