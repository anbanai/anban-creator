package image

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
)

// realPNGBytes builds a tiny but valid PNG so tests don't need fixture files.
func realPNGBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func writeFile(t *testing.T, pattern string, data []byte) string {
	t.Helper()
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		t.Fatalf("createtemp: %v", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		t.Fatalf("write: %v", err)
	}
	f.Close()
	return f.Name()
}

func TestIsValidImageFile_RealPNG(t *testing.T) {
	path := writeFile(t, "real*.png", realPNGBytes(t))
	defer os.Remove(path)

	if !IsValidImageFormat(path) {
		t.Fatal("IsValidImageFormat should accept .png extension")
	}
	if !IsValidImageFile(path) {
		t.Fatal("IsValidImageFile should accept a real PNG")
	}
}

// TestIsValidImageFile_RenamedText is the core regression: a non-image file
// renamed with a .png extension must be rejected by IsValidImageFile even though
// the extension-only IsValidImageFormat accepts it.
func TestIsValidImageFile_RenamedText(t *testing.T) {
	path := writeFile(t, "fake*.png", []byte("<html><body>not an image</body></html>"))
	defer os.Remove(path)

	if !IsValidImageFormat(path) {
		t.Fatal("IsValidImageFormat should accept the .png extension alone")
	}
	if IsValidImageFile(path) {
		t.Fatal("IsValidImageFile must reject non-image content masquerading as .png")
	}
}

func TestIsValidImageFile_UnsupportedExt(t *testing.T) {
	path := writeFile(t, "real*.txt", realPNGBytes(t))
	defer os.Remove(path)

	if IsValidImageFile(path) {
		t.Fatal("IsValidImageFile must reject unsupported extensions even with image content")
	}
}

func TestIsValidImageFile_Missing(t *testing.T) {
	if IsValidImageFile("/no/such/anban-creator-file.png") {
		t.Fatal("IsValidImageFile must reject a missing file")
	}
}

// TestIsValidImageFile_SmallerThanSniff ensures files shorter than the 512-byte
// sniff window are still classified correctly (ReadFull returns the bytes read).
func TestIsValidImageFile_SmallerThanSniff(t *testing.T) {
	path := writeFile(t, "tiny*.gif", []byte("GIF89a")) // valid GIF header, 6 bytes
	defer os.Remove(path)

	if !IsValidImageFile(path) {
		t.Fatal("IsValidImageFile should accept a small but real GIF header")
	}
}
