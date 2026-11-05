package image

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math/rand"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if err := StartVisionRuntime(); err != nil {
		fmt.Fprintf(os.Stderr, "start libvips: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	StopVisionRuntime()
	os.Exit(code)
}

func TestDownscaleForVisionCapsLandscapeLongEdge(t *testing.T) {
	source := visionTestPNG(t, 3000, 2000)

	got, err := DownscaleForVision(source, "image/png", VisionDownscaleOptions{MaxEdge: 1600})
	if err != nil {
		t.Fatalf("DownscaleForVision: %v", err)
	}
	if !got.Changed || got.MIMEType != "image/jpeg" {
		t.Fatalf("changed=%v mime=%q, want jpeg re-encode", got.Changed, got.MIMEType)
	}
	if got.Width != 1600 || got.Height < 1066 || got.Height > 1067 {
		t.Fatalf("size = %dx%d, want 1600x1066 or 1600x1067", got.Width, got.Height)
	}
	// The byte win depends on the source codec: a synthetic gradient PNG packs
	// into ~128KB while its JPEG re-encode is larger, and the resize still pays
	// because the vision model bills tokens by pixel count. Assert the payload
	// stays bounded rather than smaller than this particular fixture.
	if len(got.Data) > 400<<10 {
		t.Fatalf("encoded %d bytes, want a bounded payload", len(got.Data))
	}
	width, height, err := visionImageDimensions(got.Data)
	if err != nil {
		t.Fatalf("decode encoded dimensions: %v", err)
	}
	if width != 1600 || height != got.Height {
		t.Fatalf("encoded image is %dx%d, want 1600x%d", width, height, got.Height)
	}
}

func TestDownscaleForVisionCapsPortraitLongEdge(t *testing.T) {
	source := visionTestPNG(t, 1200, 2400)

	got, err := DownscaleForVision(source, "image/png", VisionDownscaleOptions{MaxEdge: 1600})
	if err != nil {
		t.Fatalf("DownscaleForVision: %v", err)
	}
	if !got.Changed || got.Height != 1600 || got.Width != 800 {
		t.Fatalf("changed=%v size=%dx%d, want 800x1600", got.Changed, got.Width, got.Height)
	}
}

func TestDownscaleForVisionPassesSmallImagesThrough(t *testing.T) {
	source := visionTestPNG(t, 800, 600)

	got, err := DownscaleForVision(source, "image/png", VisionDownscaleOptions{MaxEdge: 1600})
	if err != nil {
		t.Fatalf("DownscaleForVision: %v", err)
	}
	if got.Changed || got.MIMEType != "image/png" {
		t.Fatalf("changed=%v mime=%q, want an untouched PNG", got.Changed, got.MIMEType)
	}
	if !bytes.Equal(got.Data, source) {
		t.Fatalf("payload mutated for an image that already fits the budget")
	}
	if got.Width != 800 || got.Height != 600 {
		t.Fatalf("size = %dx%d, want 800x600", got.Width, got.Height)
	}
}

func TestDownscaleForVisionReencodesOversizedBytesWithinEdge(t *testing.T) {
	source := visionTestNoisePNG(t, 700, 700)
	if len(source) <= visionPassthroughBytes {
		t.Fatalf("fixture is %d bytes, want more than %d", len(source), visionPassthroughBytes)
	}

	got, err := DownscaleForVision(source, "image/png", VisionDownscaleOptions{MaxEdge: 1600})
	if err != nil {
		t.Fatalf("DownscaleForVision: %v", err)
	}
	if !got.Changed || got.MIMEType != "image/jpeg" {
		t.Fatalf("changed=%v mime=%q, want jpeg re-encode", got.Changed, got.MIMEType)
	}
	if len(got.Data) >= len(source) {
		t.Fatalf("encoded %d bytes, source %d bytes; want a smaller payload", len(got.Data), len(source))
	}
	if got.Width != 700 || got.Height != 700 {
		t.Fatalf("size = %dx%d, want 700x700 (no resize needed)", got.Width, got.Height)
	}
}

func TestDownscaleForVisionDisabledWithoutMaxEdge(t *testing.T) {
	source := visionTestPNG(t, 3000, 2000)

	got, err := DownscaleForVision(source, "image/png", VisionDownscaleOptions{MaxEdge: 0})
	if err != nil {
		t.Fatalf("DownscaleForVision: %v", err)
	}
	if got.Changed || !bytes.Equal(got.Data, source) || got.MIMEType != "image/png" {
		t.Fatalf("changed=%v mime=%q, want the original payload", got.Changed, got.MIMEType)
	}
}

func TestDownscaleForVisionFlattensAlphaOnWhite(t *testing.T) {
	source := visionTestTransparentPNG(t, 900, 900)

	got, err := DownscaleForVision(source, "image/png", VisionDownscaleOptions{MaxEdge: 400})
	if err != nil {
		t.Fatalf("DownscaleForVision: %v", err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(got.Data))
	if err != nil {
		t.Fatalf("decode jpeg: %v", err)
	}
	r, g, b, _ := decoded.At(0, 0).RGBA()
	if r>>8 < 235 || g>>8 < 235 || b>>8 < 235 {
		t.Fatalf("corner = (%d,%d,%d), want white after alpha flattening", r>>8, g>>8, b>>8)
	}
}

func TestDownscaleForVisionSkipsAnimatedGIF(t *testing.T) {
	source := visionTestGIF(t, 40, 40)

	got, err := DownscaleForVision(source, "image/gif", VisionDownscaleOptions{MaxEdge: 8})
	if err != nil {
		t.Fatalf("DownscaleForVision: %v", err)
	}
	if got.Changed || got.MIMEType != "image/gif" || !bytes.Equal(got.Data, source) {
		t.Fatalf("changed=%v mime=%q, want the original GIF", got.Changed, got.MIMEType)
	}
}

func TestDownscaleForVisionReturnsErrorForUndecodableBytes(t *testing.T) {
	source := []byte("not an image at all")

	got, err := DownscaleForVision(source, "image/png", VisionDownscaleOptions{MaxEdge: 100})
	if err == nil {
		t.Fatal("expected a decode error")
	}
	if got.Changed || !bytes.Equal(got.Data, source) || got.MIMEType != "image/png" {
		t.Fatalf("changed=%v mime=%q, want the original payload on failure", got.Changed, got.MIMEType)
	}
}

func TestDownscaleForVisionNeverUpscales(t *testing.T) {
	source := visionTestPNG(t, 100, 80)

	got, err := DownscaleForVision(source, "image/png", VisionDownscaleOptions{MaxEdge: 1600})
	if err != nil {
		t.Fatalf("DownscaleForVision: %v", err)
	}
	if got.Changed {
		t.Fatalf("changed=true, want no upscale for a 100x80 source")
	}
	if got.Width != 100 || got.Height != 80 {
		t.Fatalf("size = %dx%d, want 100x80", got.Width, got.Height)
	}
}

func visionTestPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return encoded.Bytes()
}

// visionTestNoisePNG produces a payload that PNG cannot compress, so the
// byte-budget branch is exercised without allocating an enormous image.
func visionTestNoisePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	rng := rand.New(rand.NewSource(1))
	for i := range img.Pix {
		img.Pix[i] = uint8(rng.Intn(256))
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatalf("encode noise png: %v", err)
	}
	return encoded.Bytes()
}

func visionTestTransparentPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatalf("encode transparent png: %v", err)
	}
	return encoded.Bytes()
}

func visionTestGIF(t *testing.T, width, height int) []byte {
	t.Helper()
	palette := color.Palette{color.Black, color.White}
	img := image.NewPaletted(image.Rect(0, 0, width, height), palette)
	var encoded bytes.Buffer
	if err := gif.Encode(&encoded, img, nil); err != nil {
		t.Fatalf("encode gif: %v", err)
	}
	return encoded.Bytes()
}

func TestDownscaleForVisionHonorsQuality(t *testing.T) {
	source := visionTestNoisePNG(t, 900, 900)

	low, err := DownscaleForVision(source, "image/png", VisionDownscaleOptions{MaxEdge: 500, Quality: 30})
	if err != nil {
		t.Fatalf("DownscaleForVision low quality: %v", err)
	}
	high, err := DownscaleForVision(source, "image/png", VisionDownscaleOptions{MaxEdge: 500, Quality: 95})
	if err != nil {
		t.Fatalf("DownscaleForVision high quality: %v", err)
	}
	if !low.Changed || !high.Changed {
		t.Fatalf("changed low=%v high=%v, want both re-encoded", low.Changed, high.Changed)
	}
	if low.Width != high.Width || low.Height != high.Height {
		t.Fatalf("quality changed dimensions: %dx%d vs %dx%d", low.Width, low.Height, high.Width, high.Height)
	}
	if len(low.Data) >= len(high.Data) {
		t.Fatalf("quality 30 produced %d bytes, quality 95 produced %d; want the lower quality smaller", len(low.Data), len(high.Data))
	}
}

func TestDownscaleForVisionDefaultsQuality(t *testing.T) {
	source := visionTestNoisePNG(t, 600, 600)

	implicit, err := DownscaleForVision(source, "image/png", VisionDownscaleOptions{MaxEdge: 400})
	if err != nil {
		t.Fatalf("DownscaleForVision default quality: %v", err)
	}
	explicit, err := DownscaleForVision(source, "image/png", VisionDownscaleOptions{MaxEdge: 400, Quality: defaultVisionJPEGQuality})
	if err != nil {
		t.Fatalf("DownscaleForVision explicit quality: %v", err)
	}
	if !bytes.Equal(implicit.Data, explicit.Data) {
		t.Fatalf("zero quality (%d bytes) differs from the default (%d bytes)", len(implicit.Data), len(explicit.Data))
	}
}
