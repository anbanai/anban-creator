package image

import (
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/disintegration/imaging"
)

// WeChat cover (公众号封面) official dimensions. The large/primary cover shown
// in the subscription list and article detail is 2.35:1 = 900×383px (900/383 ≈
// 2.349). The 1:1 forward/share card is auto-cropped by WeChat from the cover's
// CENTER, so the design must keep the subject inside the central 1:1 safe zone
// (handled in the article-cover-design skill), but only one 2.35:1 file is
// produced here.
const (
	WeChatCoverWidth  = 900
	WeChatCoverHeight = 383
)

// CropToSize center-crops the image at filePath to exactly targetW×targetH and
// overwrites filePath atomically (temp file in the same directory → rename).
//
// It first crops the source to the TARGET ASPECT RATIO from the center (keeping
// the maximum possible area, so a centered subject is retained), then resizes
// to the exact pixel dimensions — no distortion, no letterboxing.
//
// Used to force WeChat article covers to the exact 900×383 (2.35:1) spec so
// WeChat never re-crops the uploaded thumbnail (providers cannot natively emit
// this ratio; the skill requests 21:9 as the nearest supported generation hint).
func CropToSize(filePath string, targetW, targetH int) error {
	if targetW <= 0 || targetH <= 0 {
		return fmt.Errorf("invalid target dimensions %dx%d", targetW, targetH)
	}

	img, err := imaging.Open(filePath)
	if err != nil {
		return fmt.Errorf("open image: %w", err)
	}

	srcBounds := img.Bounds()
	srcW, srcH := srcBounds.Dx(), srcBounds.Dy()
	if srcW <= 0 || srcH <= 0 {
		return fmt.Errorf("invalid source dimensions %dx%d", srcW, srcH)
	}

	// Crop to the target aspect ratio from the center, keeping the maximum area.
	// One dimension stays at the source size; the other is trimmed.
	targetRatio := float64(targetW) / float64(targetH)
	srcRatio := float64(srcW) / float64(srcH)
	cropW, cropH := srcW, srcH
	if srcRatio > targetRatio {
		// Source is wider than the target ratio → trim width, keep full height.
		cropW = min(int(math.Round(float64(srcH)*targetRatio)), srcW)
	} else {
		// Source is taller than the target ratio → trim height, keep full width.
		cropH = min(int(math.Round(float64(srcW)/targetRatio)), srcH)
	}

	cropped := imaging.CropCenter(img, cropW, cropH)
	resized := imaging.Resize(cropped, targetW, targetH, imaging.Lanczos)

	// Atomic save: write a temp file in the same directory, then rename over the
	// original so a crash never leaves a half-written cover.
	dir := filepath.Dir(filePath)
	ext := strings.ToLower(filepath.Ext(filePath))
	tmp, err := os.CreateTemp(dir, ".cover-crop-*"+ext)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		if _, statErr := os.Stat(tmpName); statErr == nil {
			os.Remove(tmpName)
		}
	}

	if err := encodeTransformedImage(tmp, resized, ext); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("encode image: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, filePath); err != nil {
		cleanup()
		return fmt.Errorf("rename cropped cover: %w", err)
	}
	return nil
}

// encodeTransformedImage writes img to f in the format implied by ext. PNG is
// preserved losslessly; everything else (jpg/jpeg/unknown) is JPEG at high
// quality. WeChat covers are PNG.
func encodeTransformedImage(f *os.File, img image.Image, ext string) error {
	switch ext {
	case ".png":
		enc := png.Encoder{CompressionLevel: png.DefaultCompression}
		return enc.Encode(f, img)
	default:
		return jpeg.Encode(f, img, &jpeg.Options{Quality: 90})
	}
}
