package processor

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	stdraw "image/draw"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"

	"my-go-server/internal/global"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

var ErrUnsupportedImage = errors.New("unsupported image format")

type ImageProcessor struct {
	enabled bool

	defaultMaxWidth  int
	defaultMaxHeight int
	defaultQuality   int

	wm global.WatermarkConfig

	font  *opentype.Font
	wmImg image.Image
}

func NewImageProcessor(cfg global.ImageProcessorConfig) (*ImageProcessor, error) {
	p := &ImageProcessor{
		enabled:          cfg.Enabled,
		defaultMaxWidth:  cfg.MaxWidth,
		defaultMaxHeight: cfg.MaxHeight,
		defaultQuality:   clampInt(cfg.OutputQuality, 1, 100, 85),
		wm:               cfg.Watermark,
	}
	p.wm.Type = strings.ToLower(strings.TrimSpace(p.wm.Type))
	p.wm.Position = strings.ToLower(strings.TrimSpace(p.wm.Position))

	if !p.enabled {
		return p, nil
	}

	if p.wm.Enabled {
		switch p.wm.Type {
		case "", "text":
			if strings.TrimSpace(p.wm.Text) != "" {
				f, err := loadFont(p.wm.FontPath)
				if err != nil {
					return nil, err
				}
				p.font = f
			}
		case "image":
			if path := strings.TrimSpace(p.wm.ImagePath); path != "" {
				img, err := loadImageFile(path)
				if err != nil {
					return nil, err
				}
				p.wmImg = img
			}
		default:
			return nil, fmt.Errorf("unknown watermark type %q", p.wm.Type)
		}
	}

	return p, nil
}

func (p *ImageProcessor) Enabled() bool {
	return p != nil && p.enabled
}

func (p *ImageProcessor) ProcessPath(ctx context.Context, inPath string) (outPath string, cleanup func() error, changed bool, err error) {
	if p == nil {
		return inPath, nil, false, nil
	}
	return p.processPath(ctx, inPath, p.defaultMaxWidth, p.defaultMaxHeight, p.defaultQuality, true)
}

func (p *ImageProcessor) ProcessPathWith(ctx context.Context, inPath string, maxW, maxH, quality int) (outPath string, cleanup func() error, changed bool, err error) {
	if p == nil {
		return inPath, nil, false, nil
	}
	quality = clampInt(quality, 1, 100, p.defaultQuality)
	return p.processPath(ctx, inPath, maxW, maxH, quality, true)
}

// ProcessPathNoWatermark runs image resize (and other non-watermark steps) but skips watermark overlay.
// It is used when Strategy-level watermarking is enabled to avoid double watermarking.
func (p *ImageProcessor) ProcessPathNoWatermark(ctx context.Context, inPath string) (outPath string, cleanup func() error, changed bool, err error) {
	if p == nil {
		return inPath, nil, false, nil
	}
	return p.processPath(ctx, inPath, p.defaultMaxWidth, p.defaultMaxHeight, p.defaultQuality, false)
}

func (p *ImageProcessor) processPath(ctx context.Context, inPath string, maxW, maxH, quality int, applyWatermark bool) (outPath string, cleanup func() error, changed bool, err error) {
	if err := ctx.Err(); err != nil {
		return inPath, nil, false, err
	}
	inPath = strings.TrimSpace(inPath)
	if p == nil || !p.enabled || inPath == "" {
		return inPath, nil, false, nil
	}

	wmEnabled := p.wm.Enabled && applyWatermark

	// Fast path: nothing to do (still leave decoding to the caller).
	if !wmEnabled && maxW <= 0 && maxH <= 0 {
		return inPath, nil, false, nil
	}

	ext := strings.ToLower(filepath.Ext(inPath))
	if ext == "" {
		ext = ".jpg"
	}
	dir := filepath.Dir(inPath)
	f, err := os.CreateTemp(dir, "imgproc_*"+ext)
	if err != nil {
		return inPath, nil, false, err
	}
	outPath = f.Name()
	_ = f.Close()

	changed, err = p.processToFile(ctx, inPath, outPath, maxW, maxH, quality, applyWatermark)
	if err != nil {
		_ = os.Remove(outPath)
		return inPath, nil, false, err
	}
	if !changed {
		_ = os.Remove(outPath)
		return inPath, nil, false, nil
	}

	return outPath, func() error { return os.Remove(outPath) }, true, nil
}

func (p *ImageProcessor) processToFile(ctx context.Context, inPath, outPath string, maxW, maxH, quality int, applyWatermark bool) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	f, err := os.Open(inPath)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()

	src, format, err := image.Decode(f)
	if err != nil {
		return false, ErrUnsupportedImage
	}

	format = strings.ToLower(strings.TrimSpace(format))
	switch format {
	case "jpeg", "jpg", "png":
		// ok
	default:
		// Avoid breaking animated formats by accident.
		return false, ErrUnsupportedImage
	}

	changed := false

	img := src
	if maxW > 0 || maxH > 0 {
		resized, ok := resizeToMax(img, maxW, maxH)
		if ok {
			img = resized
			changed = true
		}
	}

	nrgba := toNRGBA(img)
	if p.wm.Enabled && applyWatermark {
		ok, err := p.applyWatermark(nrgba)
		if err != nil {
			return false, err
		}
		if ok {
			changed = true
		}
	}

	if !changed {
		return false, nil
	}

	if err := saveImageFile(outPath, nrgba, format, quality); err != nil {
		return false, err
	}

	return true, nil
}

func (p *ImageProcessor) applyWatermark(dst *image.NRGBA) (bool, error) {
	if p == nil || dst == nil {
		return false, nil
	}
	if !p.wm.Enabled {
		return false, nil
	}
	opacity := clampFloat64(p.wm.Opacity, 0, 1)
	if opacity <= 0 {
		return false, nil
	}

	switch p.wm.Type {
	case "", "text":
		text := strings.TrimSpace(p.wm.Text)
		if text == "" {
			return false, nil
		}
		if p.font == nil {
			return false, errors.New("watermark font is not loaded")
		}
		return drawWatermarkText(dst, p.font, text, p.wm, opacity)
	case "image":
		if p.wmImg == nil {
			return false, errors.New("watermark image is not loaded")
		}
		return drawWatermarkImage(dst, p.wmImg, p.wm, opacity)
	default:
		return false, fmt.Errorf("unknown watermark type %q", p.wm.Type)
	}
}

func drawWatermarkText(dst *image.NRGBA, f *opentype.Font, text string, cfg global.WatermarkConfig, opacity float64) (bool, error) {
	size := cfg.FontSize
	if size <= 0 {
		size = 24
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingNone,
	})
	if err != nil {
		return false, err
	}
	defer func() {
		if c, ok := face.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	}()

	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	lines = dropEmpty(lines)
	if len(lines) == 0 {
		return false, nil
	}

	margin := cfg.Margin
	if margin < 0 {
		margin = 0
	}

	metrics := face.Metrics()
	ascent := metrics.Ascent.Ceil()
	descent := metrics.Descent.Ceil()
	lineHeight := ascent + descent
	if lineHeight <= 0 {
		lineHeight = int(size)
	}
	lineGap := int(math.Max(2, float64(lineHeight)*0.15))

	maxW := 0
	for _, line := range lines {
		w := font.MeasureString(face, line).Ceil()
		if w > maxW {
			maxW = w
		}
	}
	blockH := len(lines)*lineHeight + (len(lines)-1)*lineGap

	imgW := dst.Bounds().Dx()
	imgH := dst.Bounds().Dy()

	left, top := anchorRect(imgW, imgH, maxW, blockH, cfg.Position, margin)
	baselineY := top + ascent

	a := uint8(math.Round(opacity * 255))
	main := color.NRGBA{R: 255, G: 255, B: 255, A: a}
	shadow := color.NRGBA{R: 0, G: 0, B: 0, A: uint8(math.Round(opacity * 140))}

	for i, line := range lines {
		y := baselineY + i*(lineHeight+lineGap)
		drawString(dst, face, left, y, line, shadow, 1, 1)
		drawString(dst, face, left, y, line, main, 0, 0)
	}

	return true, nil
}

func drawString(dst stdraw.Image, face font.Face, x, y int, s string, col color.NRGBA, dx, dy int) {
	d := font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(col),
		Face: face,
		Dot:  fixed.P(x+dx, y+dy),
	}
	d.DrawString(s)
}

func drawWatermarkImage(dst *image.NRGBA, wm image.Image, cfg global.WatermarkConfig, opacity float64) (bool, error) {
	if dst == nil || wm == nil {
		return false, nil
	}
	margin := cfg.Margin
	if margin < 0 {
		margin = 0
	}

	scale := cfg.Scale
	if scale <= 0 {
		scale = 0.2
	}

	imgW := dst.Bounds().Dx()
	imgH := dst.Bounds().Dy()
	if imgW <= 0 || imgH <= 0 {
		return false, nil
	}

	wmB := wm.Bounds()
	wmW := wmB.Dx()
	wmH := wmB.Dy()
	if wmW <= 0 || wmH <= 0 {
		return false, nil
	}

	targetW := wmW
	if scale > 0 && scale <= 1 {
		targetW = int(math.Round(float64(imgW) * scale))
	} else if scale > 1 {
		targetW = int(math.Round(scale))
	}
	if targetW < 1 {
		targetW = 1
	}
	if targetW > imgW-margin*2 {
		targetW = imgW - margin*2
	}
	if targetW < 1 {
		return false, nil
	}

	targetH := int(math.Round(float64(wmH) * float64(targetW) / float64(wmW)))
	if targetH < 1 {
		targetH = 1
	}

	scaled := image.NewNRGBA(image.Rect(0, 0, targetW, targetH))
	xdraw.ApproxBiLinear.Scale(scaled, scaled.Bounds(), wm, wmB, xdraw.Over, nil)

	applyAlpha(scaled, opacity)

	left, top := anchorRect(imgW, imgH, targetW, targetH, cfg.Position, margin)
	pos := image.Rect(left, top, left+targetW, top+targetH)
	stdraw.Draw(dst, pos, scaled, image.Point{}, stdraw.Over)
	return true, nil
}

func anchorRect(imgW, imgH, w, h int, pos string, margin int) (left, top int) {
	switch pos {
	case "top_left":
		left, top = margin, margin
	case "top_right":
		left, top = imgW-margin-w, margin
	case "bottom_left":
		left, top = margin, imgH-margin-h
	case "center":
		left, top = (imgW-w)/2, (imgH-h)/2
	case "bottom_right", "":
		fallthrough
	default:
		left, top = imgW-margin-w, imgH-margin-h
	}
	if left < 0 {
		left = 0
	}
	if top < 0 {
		top = 0
	}
	return left, top
}

func resizeToMax(src image.Image, maxW, maxH int) (image.Image, bool) {
	if src == nil {
		return src, false
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return src, false
	}

	scale := 1.0
	if maxW > 0 && w > maxW {
		scale = math.Min(scale, float64(maxW)/float64(w))
	}
	if maxH > 0 && h > maxH {
		scale = math.Min(scale, float64(maxH)/float64(h))
	}
	if scale >= 1.0 {
		return src, false
	}

	nw := int(math.Round(float64(w) * scale))
	nh := int(math.Round(float64(h) * scale))
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}

	dst := image.NewNRGBA(image.Rect(0, 0, nw, nh))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)
	return dst, true
}

func toNRGBA(src image.Image) *image.NRGBA {
	if src == nil {
		return nil
	}
	if v, ok := src.(*image.NRGBA); ok {
		return v
	}
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	stdraw.Draw(dst, dst.Bounds(), src, b.Min, stdraw.Src)
	return dst
}

func applyAlpha(img *image.NRGBA, opacity float64) {
	if img == nil {
		return
	}
	if opacity >= 1 {
		return
	}
	if opacity <= 0 {
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 0
		}
		return
	}
	for i := 3; i < len(img.Pix); i += 4 {
		a := float64(img.Pix[i])
		img.Pix[i] = uint8(math.Round(a * opacity))
	}
}

func saveImageFile(path string, img image.Image, format string, quality int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	switch format {
	case "jpeg", "jpg":
		return jpeg.Encode(f, img, &jpeg.Options{Quality: clampInt(quality, 1, 100, 85)})
	case "png":
		enc := png.Encoder{CompressionLevel: png.DefaultCompression}
		return enc.Encode(f, img)
	default:
		return ErrUnsupportedImage
	}
}

func loadFont(path string) (*opentype.Font, error) {
	path = strings.TrimSpace(path)
	var data []byte
	if path == "" {
		data = goregular.TTF
	} else {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		data = b
	}
	f, err := opentype.Parse(data)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func loadImageFile(path string) (image.Image, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("empty watermark image path")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	img, _, err := image.Decode(f)
	return img, err
}

func dropEmpty(lines []string) []string {
	out := lines[:0]
	for _, s := range lines {
		if strings.TrimSpace(s) == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

func clampInt(v, min, max, def int) int {
	if v == 0 {
		return def
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func clampFloat64(v, min, max float64) float64 {
	if math.IsNaN(v) {
		return min
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
