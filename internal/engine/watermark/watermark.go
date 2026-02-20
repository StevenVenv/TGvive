package watermark

import (
	"bytes"
	"errors"
	"image"
	stdraw "image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"strings"
	"sync"

	"my-go-server/internal/model"

	"github.com/fogleman/gg"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
)

var (
	regularFontOnce sync.Once
	regularFont     *opentype.Font
	regularFontErr  error

	pngCache sync.Map // map[path]string -> image.Image
)

func loadRegularFont() (*opentype.Font, error) {
	regularFontOnce.Do(func() {
		regularFont, regularFontErr = opentype.Parse(goregular.TTF)
	})
	return regularFont, regularFontErr
}

func clampInt(min, v, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func calcXY(baseW, baseH, wmW, wmH int, rule model.WatermarkRule) (x, y int) {
	marginPx := int(math.Round(float64(baseW) * rule.Margin))

	pos := strings.ToLower(strings.TrimSpace(rule.Position))
	switch pos {
	case "center":
		x = (baseW - wmW) / 2
		y = (baseH - wmH) / 2
	case "top_right":
		x = baseW - wmW - marginPx
		y = marginPx
	case "top_left":
		x = marginPx
		y = marginPx
	case "bottom_left":
		x = marginPx
		y = baseH - wmH - marginPx
	case "custom":
		x = int(math.Round(float64(baseW) * rule.CustomX))
		y = int(math.Round(float64(baseH) * rule.CustomY))
	case "bottom_right", "":
		fallthrough
	default:
		x = baseW - wmW - marginPx
		y = baseH - wmH - marginPx
	}

	// Clamp to bounds (avoid overflow).
	if x+wmW > baseW {
		x = baseW - wmW
	}
	if y+wmH > baseH {
		y = baseH - wmH
	}
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return x, y
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

func loadPNG(path string) (image.Image, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("empty image_path")
	}
	if v, ok := pngCache.Load(path); ok {
		if img, ok2 := v.(image.Image); ok2 && img != nil {
			return img, nil
		}
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	pngCache.Store(path, img)
	return img, nil
}

func applyImageWatermark(base image.Image, rule model.WatermarkRule) (image.Image, error) {
	wmImg, err := loadPNG(rule.ImagePath)
	if err != nil {
		return nil, err
	}

	b := base.Bounds()
	baseW, baseH := b.Dx(), b.Dy()
	if baseW <= 0 || baseH <= 0 {
		return nil, errors.New("invalid base image size")
	}

	wmb := wmImg.Bounds()
	wmW, wmH := wmb.Dx(), wmb.Dy()
	if wmW <= 0 || wmH <= 0 {
		return nil, errors.New("invalid watermark image size")
	}

	targetW := int(math.Round(float64(baseW) * rule.ScaleRatio))
	targetW = clampInt(1, targetW, baseW)
	targetH := int(math.Round(float64(targetW) * float64(wmH) / float64(wmW)))
	targetH = clampInt(1, targetH, baseH)

	scaled := image.NewNRGBA(image.Rect(0, 0, targetW, targetH))
	xdraw.BiLinear.Scale(scaled, scaled.Bounds(), wmImg, wmb, xdraw.Over, nil)

	opacity := rule.Opacity
	if opacity <= 0 {
		return base, nil
	}
	if opacity > 1 {
		opacity = 1
	}

	mask := image.NewAlpha(scaled.Bounds())
	for i := 0; i < targetW*targetH; i++ {
		a := scaled.Pix[i*4+3]
		mask.Pix[i] = uint8(math.Round(float64(a) * opacity))
		scaled.Pix[i*4+3] = 0xFF
	}

	dst := toNRGBA(base)
	x, y := calcXY(baseW, baseH, targetW, targetH, rule)
	pos := image.Rect(x, y, x+targetW, y+targetH)
	stdraw.DrawMask(dst, pos, scaled, image.Point{}, mask, image.Point{}, stdraw.Over)
	return dst, nil
}

func applyTextWatermark(base image.Image, rule model.WatermarkRule) (image.Image, error) {
	text := strings.TrimSpace(rule.Text)
	if text == "" {
		return nil, errors.New("empty watermark text")
	}

	b := base.Bounds()
	baseW, baseH := b.Dx(), b.Dy()
	if baseW <= 0 || baseH <= 0 {
		return nil, errors.New("invalid base image size")
	}

	fontSize := float64(baseW) * rule.ScaleRatio
	if fontSize < 8 {
		fontSize = 8
	}

	f, err := loadRegularFont()
	if err != nil {
		return nil, err
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{
		Size:    fontSize,
		DPI:     72,
		Hinting: font.HintingNone,
	})
	if err != nil {
		return nil, err
	}
	defer func() {
		if c, ok := face.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	}()

	dc := gg.NewContextForImage(base)
	dc.SetFontFace(face)

	raw := strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(raw, "\n")
	if len(lines) == 0 {
		return base, nil
	}

	maxW := 0.0
	maxH := 0.0
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
		w, h := dc.MeasureString(lines[i])
		if w > maxW {
			maxW = w
		}
		if h > maxH {
			maxH = h
		}
	}
	if maxW <= 0 || maxH <= 0 {
		return base, nil
	}

	lineStep := fontSize * 1.2
	totalH := maxH
	if len(lines) > 1 {
		totalH = float64(len(lines)-1)*lineStep + maxH
	}

	wmW := int(math.Ceil(maxW))
	wmH := int(math.Ceil(totalH))

	x, y := calcXY(baseW, baseH, wmW, wmH, rule)

	shadowAlpha := rule.Opacity
	if shadowAlpha > 1 {
		shadowAlpha = 1
	}
	if shadowAlpha < 0 {
		shadowAlpha = 0
	}
	mainAlpha := shadowAlpha
	shadowAlpha *= 0.8

	// Draw per-line to keep ordering stable.
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		yy := float64(y) + float64(i)*lineStep

		dc.SetRGBA(0, 0, 0, shadowAlpha)
		dc.DrawStringAnchored(line, float64(x)+2, yy+2, 0, 0)
		dc.DrawStringAnchored(line, float64(x)-2, yy-2, 0, 0)

		dc.SetRGBA(1, 1, 1, mainAlpha)
		dc.DrawStringAnchored(line, float64(x), yy, 0, 0)
	}

	return dc.Image(), nil
}

// ApplyWatermark applies the watermark rule to baseBytes in memory and returns JPEG bytes (quality=85).
func ApplyWatermark(baseBytes []byte, rule model.WatermarkRule) ([]byte, error) {
	if !rule.Enable {
		return baseBytes, nil
	}
	if len(baseBytes) == 0 {
		return nil, errors.New("empty base image bytes")
	}

	base, _, err := image.Decode(bytes.NewReader(baseBytes))
	if err != nil {
		return nil, err
	}

	typ := strings.ToLower(strings.TrimSpace(rule.Type))
	if typ == "" {
		if strings.TrimSpace(rule.ImagePath) != "" {
			typ = "image"
		} else {
			typ = "text"
		}
	}

	var out image.Image
	switch typ {
	case "image":
		out, err = applyImageWatermark(base, rule)
	case "text":
		out, err = applyTextWatermark(base, rule)
	default:
		out, err = applyTextWatermark(base, rule)
	}
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, errors.New("watermark output image is nil")
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
