package image

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestBackgroundOpacityPreservesSubjectAndExport(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	subject := color.NRGBA{R: 40, G: 100, B: 160, A: 255}
	source.SetNRGBA(4, 4, subject)
	source.SetNRGBA(3, 4, color.NRGBA{R: 40, G: 100, B: 160, A: 128})
	var input bytes.Buffer
	if err := png.Encode(&input, source); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"png", "webp", "jpeg"} {
		for _, opacity := range []int{0, 50, 100} {
			t.Run(fmt.Sprintf("%s/%d", format, opacity), func(t *testing.T) {
				output, err := ApplyBackgroundOpacity(input.Bytes(), opacity, format, 100)
				if err != nil {
					t.Fatal(err)
				}
				decoded, gotFormat, err := image.Decode(bytes.NewReader(output))
				if err != nil {
					t.Fatal(err)
				}
				if gotFormat != format {
					t.Fatalf("format = %s, want %s", gotFormat, format)
				}
				if decoded.Bounds() != source.Bounds() {
					t.Fatal("dimensions changed")
				}
				background := color.NRGBAModel.Convert(decoded.At(0, 0)).(color.NRGBA)
				wantAlpha := uint8((opacity*255 + 50) / 100)
				if format == "jpeg" {
					wantAlpha = 255
				}
				if background.A != wantAlpha {
					t.Fatalf("background alpha = %d, want %d", background.A, wantAlpha)
				}
				if format != "jpeg" {
					if got := color.NRGBAModel.Convert(decoded.At(4, 4)); got != subject {
						t.Fatalf("subject changed: %v", got)
					}
					_, _, _, edgeAlpha := decoded.At(3, 4).RGBA()
					if edgeAlpha < 128*257 {
						t.Fatal("subject edge became more transparent")
					}
					if wantAlpha > 0 && background.R != 255 {
						t.Fatalf("background is not white: %v", background)
					}
				}
			})
		}
	}
}

func TestBackgroundOpacityRejectsOpaqueProviderOutput(t *testing.T) {
	var input bytes.Buffer
	source := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			source.SetNRGBA(x, y, color.NRGBA{A: 255})
		}
	}
	if err := png.Encode(&input, source); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyBackgroundOpacity(input.Bytes(), 50, "png", 100); err == nil {
		t.Fatal("opaque image was accepted as a transparent cutout")
	}
}
