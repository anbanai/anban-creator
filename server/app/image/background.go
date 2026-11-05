package image

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"

	"github.com/davidbyttow/govips/v2/vips"
)

// ApplyBackgroundOpacity composites a transparent cutout over white without
// changing opaque subject pixels. JPEG necessarily flattens the background.
func ApplyBackgroundOpacity(data []byte, opacity int, format string, quality int) ([]byte, error) {
	if opacity < 0 || opacity > 100 {
		return nil, fmt.Errorf("background opacity must be between 0 and 100")
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	bounds := source.Bounds()
	hasTransparency := false
	for y := bounds.Min.Y; y < bounds.Max.Y && !hasTransparency; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, alpha := source.At(x, y).RGBA()
			if alpha < 65535 {
				hasTransparency = true
				break
			}
		}
	}
	if !hasTransparency {
		return nil, fmt.Errorf("provider did not return a transparent background")
	}
	if format == "jpeg" {
		opacity = 100
	}
	result := image.NewNRGBA(bounds)
	draw.Draw(result, bounds, image.NewUniform(color.NRGBA{R: 255, G: 255, B: 255, A: uint8((opacity*255 + 50) / 100)}), image.Point{}, draw.Src)
	draw.Draw(result, bounds, source, bounds.Min, draw.Over)
	if quality <= 0 || quality > 100 {
		quality = 100
	}
	var encoded bytes.Buffer
	if format == "jpeg" {
		err = jpeg.Encode(&encoded, result, &jpeg.Options{Quality: quality})
		return encoded.Bytes(), err
	}
	if format != "png" && format != "webp" {
		return nil, fmt.Errorf("unsupported background output format %q", format)
	}
	if err := png.Encode(&encoded, result); err != nil {
		return nil, err
	}
	if format == "png" {
		return encoded.Bytes(), nil
	}
	ensureVisionLogging()
	ref, err := vips.NewImageFromBuffer(encoded.Bytes())
	if err != nil {
		return nil, err
	}
	defer ref.Close()
	output, _, err := ref.ExportWebp(&vips.WebpExportParams{Quality: quality, Lossless: quality == 100, StripMetadata: true})
	return output, err
}
