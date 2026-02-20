package watermark

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"my-go-server/internal/model"
)

func TestApplyWatermark_Text(t *testing.T) {
	base := image.NewNRGBA(image.Rect(0, 0, 200, 100))
	for i := 0; i < len(base.Pix); i += 4 {
		base.Pix[i] = 20
		base.Pix[i+1] = 40
		base.Pix[i+2] = 200
		base.Pix[i+3] = 255
	}

	var in bytes.Buffer
	if err := png.Encode(&in, base); err != nil {
		t.Fatalf("png encode: %v", err)
	}

	out, err := ApplyWatermark(in.Bytes(), model.WatermarkRule{
		Enable:     true,
		Type:       "text",
		Text:       "@MyChannel",
		Position:   "bottom_right",
		Margin:     0.02,
		ScaleRatio: 0.03,
		Opacity:    0.35,
	})
	if err != nil {
		t.Fatalf("ApplyWatermark: %v", err)
	}

	img, format, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if format != "jpeg" {
		t.Fatalf("expected jpeg, got %q", format)
	}
	if img.Bounds().Dx() != 200 || img.Bounds().Dy() != 100 {
		t.Fatalf("unexpected size: %v", img.Bounds())
	}
}

func TestApplyWatermark_Image_CustomClamp(t *testing.T) {
	dir := t.TempDir()
	wmPath := filepath.Join(dir, "wm.png")

	wm := image.NewNRGBA(image.Rect(0, 0, 40, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 40; x++ {
			wm.SetNRGBA(x, y, color.NRGBA{R: 255, G: 0, B: 0, A: 128})
		}
	}
	f, err := os.Create(wmPath)
	if err != nil {
		t.Fatalf("create wm: %v", err)
	}
	if err := png.Encode(f, wm); err != nil {
		_ = f.Close()
		t.Fatalf("encode wm: %v", err)
	}
	_ = f.Close()

	base := image.NewNRGBA(image.Rect(0, 0, 200, 100))
	for i := 0; i < len(base.Pix); i += 4 {
		base.Pix[i] = 10
		base.Pix[i+1] = 10
		base.Pix[i+2] = 10
		base.Pix[i+3] = 255
	}
	var in bytes.Buffer
	if err := jpeg.Encode(&in, base, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("jpeg encode: %v", err)
	}

	out, err := ApplyWatermark(in.Bytes(), model.WatermarkRule{
		Enable:     true,
		Type:       "image",
		ImagePath:  wmPath,
		Position:   "custom",
		CustomX:    0.99,
		CustomY:    0.99,
		Margin:     0.02,
		ScaleRatio: 0.9, // intentionally huge, should clamp without overflow
		Opacity:    0.5,
	})
	if err != nil {
		t.Fatalf("ApplyWatermark: %v", err)
	}
	img, format, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if format != "jpeg" {
		t.Fatalf("expected jpeg, got %q", format)
	}
	if img.Bounds().Dx() != 200 || img.Bounds().Dy() != 100 {
		t.Fatalf("unexpected size: %v", img.Bounds())
	}
}
