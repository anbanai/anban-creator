package image

import (
	"bytes"
	"errors"
	"fmt"
	stdimage "image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strings"
	"sync"

	"github.com/davidbyttow/govips/v2/vips"
	_ "golang.org/x/image/webp"
)

const (
	// visionPassthroughBytes keeps already-cheap payloads untouched so a
	// downscale never spends quality where it buys no transport win.
	visionPassthroughBytes   = 1 << 20
	defaultVisionJPEGQuality = 85
)

var visionLoggingOnce sync.Once

// VisionDownscaleOptions configures how a vision payload is re-encoded.
type VisionDownscaleOptions struct {
	// MaxEdge caps the long edge. A non-positive value disables downscaling.
	MaxEdge int
	// Quality is the JPEG quality (1-100). Zero selects the default.
	Quality int
}

// VisionImage is the payload a vision model receives, after any downscale.
type VisionImage struct {
	Data     []byte
	MIMEType string
	Width    int
	Height   int
	// Changed reports whether Data was re-encoded instead of passed through.
	Changed bool
}

// DownscaleForVision caps the long edge of an image before it is base64 encoded
// for a vision model, which is what dominates request size and image-token cost.
//
// The original bytes are always returned on any decode or encode failure and
// the caller is expected to log the error and keep going: a failed downscale
// must never fail the image analysis itself. GIF is passed through untouched
// because flattening an animated image to one frame silently changes meaning.
func DownscaleForVision(data []byte, mimeType string, opts VisionDownscaleOptions) (VisionImage, error) {
	original := VisionImage{Data: data, MIMEType: mimeType}
	if len(data) == 0 {
		return original, errors.New("vision image is empty")
	}
	if opts.MaxEdge <= 0 {
		return original, nil
	}
	ensureVisionLogging()
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/png", "image/jpeg", "image/jpg", "image/webp":
	case "image/gif":
		return original, nil
	default:
		return original, fmt.Errorf("vision image type %q is not downscaled", mimeType)
	}

	width, height, err := visionImageDimensions(data)
	if err != nil {
		return original, fmt.Errorf("decode vision image dimensions: %w", err)
	}
	original.Width, original.Height = width, height
	needsResize := max(width, height) > opts.MaxEdge
	if !needsResize && len(data) <= visionPassthroughBytes {
		return original, nil
	}

	encoded, newWidth, newHeight, err := encodeVisionJPEG(data, opts.MaxEdge, normalizeVisionQuality(opts.Quality), needsResize)
	if err != nil {
		return original, err
	}
	// A re-encode that did not need to shrink the image and grew the payload is
	// strictly worse: keep the caller's bytes.
	if len(encoded) >= len(data) && !needsResize {
		return original, nil
	}
	return VisionImage{Data: encoded, MIMEType: "image/jpeg", Width: newWidth, Height: newHeight, Changed: true}, nil
}

func visionImageDimensions(data []byte) (int, int, error) {
	config, _, err := stdimage.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, err
	}
	return config.Width, config.Height, nil
}

// normalizeVisionQuality falls back to the default for a zero or out-of-range
// quality, so a bad caller value can never produce an invalid encoder setting.
func normalizeVisionQuality(quality int) int {
	if quality < 1 || quality > 100 {
		return defaultVisionJPEGQuality
	}
	return quality
}

// encodeVisionJPEG converts the source to JPEG. libvips thumbnailing decodes
// only the pixels the target size needs, but it also upscales a source below the
// cap, so it is used only when the long edge actually has to shrink.
func encodeVisionJPEG(data []byte, maxEdge, quality int, needsResize bool) ([]byte, int, int, error) {
	var (
		ref *vips.ImageRef
		err error
	)
	if needsResize {
		ref, err = vips.NewThumbnailFromBuffer(data, maxEdge, maxEdge, vips.InterestingNone)
	} else {
		ref, err = vips.NewImageFromBuffer(data)
	}
	if err != nil {
		return nil, 0, 0, fmt.Errorf("vips load: %w", err)
	}
	defer ref.Close()

	if ref.HasAlpha() {
		if err := ref.Flatten(&vips.Color{R: 255, G: 255, B: 255}); err != nil {
			return nil, 0, 0, fmt.Errorf("vips flatten alpha: %w", err)
		}
	}
	encoded, _, err := ref.ExportJpeg(&vips.JpegExportParams{
		Quality:       quality,
		StripMetadata: true,
		Interlace:     false,
	})
	if err != nil {
		return nil, 0, 0, fmt.Errorf("vips export jpeg: %w", err)
	}
	return encoded, ref.Width(), ref.Height(), nil
}

// StartVisionRuntime initialises libvips for the process. It is safe to call
// more than once; the process cannot be restarted after StopVisionRuntime.
func StartVisionRuntime() error {
	ensureVisionLogging()
	if err := vips.Startup(nil); err != nil {
		return fmt.Errorf("start libvips: %w", err)
	}
	return nil
}

// ensureVisionLogging must run before the library starts: libvips otherwise
// defaults to info-level logging on stderr, which floods the Server log with one
// block per analysed image. Callers that reach the downscale without an explicit
// StartVisionRuntime still get quiet logging through govips' lazy startup.
func ensureVisionLogging() {
	visionLoggingOnce.Do(func() {
		vips.LoggingSettings(nil, vips.LogLevelError)
	})
}

// StopVisionRuntime releases libvips resources.
func StopVisionRuntime() {
	vips.Shutdown()
}
