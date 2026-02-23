package processor

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"golang.org/x/image/font/gofont/goregular"
)

var ErrFFmpegNotFound = errors.New("ffmpeg not found")

type VideoProcessor struct {
	enabled           bool
	ffmpegPath        string
	extractCover      bool
	coverTimestampSec float64
}

func NewVideoProcessor(cfg global.VideoProcessorConfig) (*VideoProcessor, error) {
	p := &VideoProcessor{
		enabled:           cfg.Enabled,
		ffmpegPath:        strings.TrimSpace(cfg.FFmpegPath),
		extractCover:      cfg.ExtractCover,
		coverTimestampSec: cfg.CoverTimestampSec,
	}

	if p.ffmpegPath == "" {
		p.ffmpegPath = "ffmpeg"
	}

	if !p.enabled {
		return p, nil
	}

	if _, err := exec.LookPath(p.ffmpegPath); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrFFmpegNotFound, p.ffmpegPath)
	}

	return p, nil
}

func (p *VideoProcessor) Enabled() bool {
	return p != nil && p.enabled
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func formatFloat(v float64) string {
	// Keep it stable for FFmpeg expressions.
	s := strconv.FormatFloat(v, 'f', 6, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" {
		return "0"
	}
	return s
}

func escapeFFmpegFilterValue(s string) string {
	// Escape characters for FFmpeg filtergraph option value.
	// See: https://ffmpeg.org/ffmpeg-filters.html#Notes-on-filtergraph-escaping
	s = strings.ReplaceAll(s, "\\", "/")
	repl := []struct{ from, to string }{
		{":", `\:`},
		{"'", `\'`},
		{",", `\,`},
		{"[", `\[`},
		{"]", `\]`},
	}
	for _, r := range repl {
		s = strings.ReplaceAll(s, r.from, r.to)
	}
	return s
}

func ffmpegOverlayXYExpr(rule model.VideoWatermarkRule) (xExpr, yExpr string) {
	margin := clampFloat64(rule.Margin, 0, 0.1)
	mExpr := fmt.Sprintf("(W*%s)", formatFloat(margin))

	motion := strings.ToLower(strings.TrimSpace(rule.Motion))
	if motion == "" {
		motion = "bounce"
	}

	if motion == "bounce" {
		period := rule.MotionPeriodSec
		if period <= 0 {
			period = 12
		}
		period = clampFloat64(period, 2, 120)
		p := formatFloat(period)

		ax := fmt.Sprintf("max(0, W-w-2*%s)", mExpr)
		ay := fmt.Sprintf("max(0, H-h-2*%s)", mExpr)
		xExpr = fmt.Sprintf("%s + %s*(0.5+0.5*sin(2*PI*t/%s))", mExpr, ax, p)
		yExpr = fmt.Sprintf("%s + %s*(0.5+0.5*sin(2*PI*t/%s + PI/2))", mExpr, ay, p)
		return xExpr, yExpr
	}

	pos := strings.ToLower(strings.TrimSpace(rule.Position))
	switch pos {
	case "center":
		return "(W-w)/2", "(H-h)/2"
	case "top_right":
		return fmt.Sprintf("W-w-%s", mExpr), mExpr
	case "top_left":
		return mExpr, mExpr
	case "bottom_left":
		return mExpr, fmt.Sprintf("H-h-%s", mExpr)
	case "custom":
		x := clamp01(rule.CustomX)
		y := clamp01(rule.CustomY)
		return fmt.Sprintf("min(max(W*%s,0),W-w)", formatFloat(x)), fmt.Sprintf("min(max(H*%s,0),H-h)", formatFloat(y))
	case "bottom_right", "":
		fallthrough
	default:
		return fmt.Sprintf("W-w-%s", mExpr), fmt.Sprintf("H-h-%s", mExpr)
	}
}

func ffmpegDrawTextXYExpr(rule model.VideoWatermarkRule) (xExpr, yExpr string) {
	margin := clampFloat64(rule.Margin, 0, 0.1)
	mExpr := fmt.Sprintf("(w*%s)", formatFloat(margin))

	motion := strings.ToLower(strings.TrimSpace(rule.Motion))
	if motion == "" {
		motion = "bounce"
	}

	if motion == "bounce" {
		period := rule.MotionPeriodSec
		if period <= 0 {
			period = 12
		}
		period = clampFloat64(period, 2, 120)
		p := formatFloat(period)

		ax := fmt.Sprintf("max(0, w-text_w-2*%s)", mExpr)
		ay := fmt.Sprintf("max(0, h-text_h-2*%s)", mExpr)
		xExpr = fmt.Sprintf("%s + %s*(0.5+0.5*sin(2*PI*t/%s))", mExpr, ax, p)
		yExpr = fmt.Sprintf("%s + %s*(0.5+0.5*sin(2*PI*t/%s + PI/2))", mExpr, ay, p)
		return xExpr, yExpr
	}

	pos := strings.ToLower(strings.TrimSpace(rule.Position))
	switch pos {
	case "center":
		return "(w-text_w)/2", "(h-text_h)/2"
	case "top_right":
		return fmt.Sprintf("w-text_w-%s", mExpr), mExpr
	case "top_left":
		return mExpr, mExpr
	case "bottom_left":
		return mExpr, fmt.Sprintf("h-text_h-%s", mExpr)
	case "custom":
		x := clamp01(rule.CustomX)
		y := clamp01(rule.CustomY)
		return fmt.Sprintf("min(max(w*%s,0),w-text_w)", formatFloat(x)), fmt.Sprintf("min(max(h*%s,0),h-text_h)", formatFloat(y))
	case "bottom_right", "":
		fallthrough
	default:
		return fmt.Sprintf("w-text_w-%s", mExpr), fmt.Sprintf("h-text_h-%s", mExpr)
	}
}

func buildDrawTextFilter(rule model.VideoWatermarkRule, textFilePath, fontFilePath string) (string, error) {
	text := strings.TrimSpace(rule.Text)
	if text == "" {
		return "", errors.New("empty watermark text")
	}

	scale := clampFloat64(rule.ScaleRatio, 0.01, 0.5)
	opacity := clamp01(rule.Opacity)
	if opacity <= 0 {
		return "", nil
	}

	mainColor := strings.TrimSpace(rule.TextColor)
	if mainColor == "" {
		mainColor = "#FFFFFF"
	}
	strokeColor := strings.TrimSpace(rule.StrokeColor)
	if strokeColor == "" {
		strokeColor = "#000000"
	}
	shadowColor := strings.TrimSpace(rule.ShadowColor)
	if shadowColor == "" {
		shadowColor = "#000000"
	}

	xExpr, yExpr := ffmpegDrawTextXYExpr(rule)

	args := []string{
		"drawtext",
		"textfile=" + "'" + escapeFFmpegFilterValue(textFilePath) + "'",
		"fontfile=" + "'" + escapeFFmpegFilterValue(fontFilePath) + "'",
		"fontsize=" + "'" + fmt.Sprintf("max(8,w*%s)", formatFloat(scale)) + "'",
		"fontcolor=" + fmt.Sprintf("%s@%s", mainColor, formatFloat(opacity)),
		"x=" + "'" + xExpr + "'",
		"y=" + "'" + yExpr + "'",
	}

	style := strings.ToLower(strings.TrimSpace(rule.TextStyle))
	switch style {
	case "", "stroke":
		strokeAlpha := math.Min(1, opacity*0.9)
		args = append(args, "borderw=2", "bordercolor="+fmt.Sprintf("%s@%s", strokeColor, formatFloat(strokeAlpha)))
	case "plain":
		// no-op
	case "shadow":
		shadowAlpha := math.Min(1, opacity*0.8)
		args = append(args, "shadowx=2", "shadowy=2", "shadowcolor="+fmt.Sprintf("%s@%s", shadowColor, formatFloat(shadowAlpha)))
	case "stroke_shadow":
		strokeAlpha := math.Min(1, opacity*0.9)
		shadowAlpha := math.Min(1, opacity*0.8)
		args = append(args,
			"borderw=2",
			"bordercolor="+fmt.Sprintf("%s@%s", strokeColor, formatFloat(strokeAlpha)),
			"shadowx=2",
			"shadowy=2",
			"shadowcolor="+fmt.Sprintf("%s@%s", shadowColor, formatFloat(shadowAlpha)),
		)
	default:
		strokeAlpha := math.Min(1, opacity*0.9)
		args = append(args, "borderw=2", "bordercolor="+fmt.Sprintf("%s@%s", strokeColor, formatFloat(strokeAlpha)))
	}

	return strings.Join(args, ":"), nil
}

func buildImageOverlayFilter(rule model.VideoWatermarkRule, imagePath string) (filter string, mapVideoLabel string, err error) {
	imagePath = strings.TrimSpace(imagePath)
	if imagePath == "" {
		return "", "", errors.New("empty watermark image path")
	}

	scale := clampFloat64(rule.ScaleRatio, 0.01, 0.5)
	opacity := clamp01(rule.Opacity)
	if opacity <= 0 {
		return "", "", nil
	}

	xExpr, yExpr := ffmpegOverlayXYExpr(rule)
	filter = strings.Join([]string{
		"[1:v]format=rgba,colorchannelmixer=aa=" + formatFloat(opacity) + "[wm0]",
		"[wm0][0:v]scale2ref=w=ref_w*" + formatFloat(scale) + ":h=-1[wm][base]",
		"[base][wm]overlay=x='" + xExpr + "':y='" + yExpr + "':format=auto:shortest=1[outv]",
	}, ";")
	return filter, "[outv]", nil
}

// WatermarkPath applies a dynamic/static watermark to a video file using FFmpeg.
// It returns (outPath, cleanup, changed, error). When disabled or rule.Enable=false, it returns (inPath, nil, false, nil).
func (p *VideoProcessor) WatermarkPath(ctx context.Context, inPath string, rule model.VideoWatermarkRule) (outPath string, cleanup func() error, changed bool, err error) {
	if err := ctx.Err(); err != nil {
		return inPath, nil, false, err
	}
	inPath = strings.TrimSpace(inPath)
	if p == nil || !p.enabled || !rule.Enable || inPath == "" {
		return inPath, nil, false, nil
	}

	typ := strings.ToLower(strings.TrimSpace(rule.Type))
	if typ == "" {
		if strings.TrimSpace(rule.ImagePath) != "" {
			typ = "image"
		} else {
			typ = "text"
		}
	}

	dir := filepath.Dir(inPath)
	f, err := os.CreateTemp(dir, "vidwm_*.mp4")
	if err != nil {
		return inPath, nil, false, err
	}
	outPath = f.Name()
	_ = f.Close()

	cleanups := make([]func() error, 0, 4)
	addCleanup := func(fn func() error) {
		if fn != nil {
			cleanups = append(cleanups, fn)
		}
	}
	addCleanup(func() error { return os.Remove(outPath) })

	cleanupAll := func() error {
		var last error
		for i := len(cleanups) - 1; i >= 0; i-- {
			if cleanups[i] == nil {
				continue
			}
			if err := cleanups[i](); err != nil && last == nil {
				last = err
			}
		}
		return last
	}

	// Prepare filter (may create temp files for text/font).
	vf := ""
	filterComplex := ""
	videoMap := "0:v:0"
	extraInputs := []string(nil)

	switch typ {
	case "text":
		textPath := ""
		fontPath := strings.TrimSpace(rule.FontPath)

		// textfile
		{
			tf, err := os.CreateTemp(dir, "wmtext_*.txt")
			if err != nil {
				_ = cleanupAll()
				return inPath, nil, false, err
			}
			textPath = tf.Name()
			_ = tf.Close()
			addCleanup(func() error { return os.Remove(textPath) })

			content := strings.ReplaceAll(rule.Text, "\r\n", "\n")
			if err := os.WriteFile(textPath, []byte(content), 0o600); err != nil {
				_ = cleanupAll()
				return inPath, nil, false, err
			}
		}

		// fontfile (fallback to embedded Go font)
		if fontPath == "" {
			ff, err := os.CreateTemp(dir, "wmfont_*.ttf")
			if err != nil {
				_ = cleanupAll()
				return inPath, nil, false, err
			}
			fontPath = ff.Name()
			_ = ff.Close()
			addCleanup(func() error { return os.Remove(fontPath) })
			if err := os.WriteFile(fontPath, goregular.TTF, 0o600); err != nil {
				_ = cleanupAll()
				return inPath, nil, false, err
			}
		}

		vf, err = buildDrawTextFilter(rule, textPath, fontPath)
		if err != nil {
			_ = cleanupAll()
			return inPath, nil, false, err
		}
		if strings.TrimSpace(vf) == "" {
			_ = cleanupAll()
			return inPath, nil, false, nil
		}

	case "image":
		wmPath := strings.TrimSpace(rule.ImagePath)
		if wmPath == "" {
			_ = cleanupAll()
			return inPath, nil, false, errors.New("empty watermark image path")
		}
		filterComplex, videoMap, err = buildImageOverlayFilter(rule, wmPath)
		if err != nil {
			_ = cleanupAll()
			return inPath, nil, false, err
		}
		if strings.TrimSpace(filterComplex) == "" {
			_ = cleanupAll()
			return inPath, nil, false, nil
		}
		extraInputs = []string{"-loop", "1", "-i", wmPath}
	default:
		_ = cleanupAll()
		return inPath, nil, false, fmt.Errorf("unknown watermark type %q", typ)
	}

	runOnce := func(audioCopy bool) error {
		if err := ctx.Err(); err != nil {
			return err
		}

		args := []string{"-y", "-hide_banner", "-loglevel", "error"}
		args = append(args, "-i", inPath)
		args = append(args, extraInputs...)

		if filterComplex != "" {
			args = append(args,
				"-filter_complex", filterComplex,
				"-map", videoMap,
			)
		} else if vf != "" {
			args = append(args,
				"-vf", vf,
				"-map", "0:v:0",
			)
		} else {
			args = append(args, "-map", "0:v:0")
		}
		args = append(args, "-map", "0:a?")

		args = append(args,
			"-c:v", "libx264",
			"-preset", "veryfast",
			"-crf", "23",
			"-pix_fmt", "yuv420p",
		)

		if audioCopy {
			args = append(args, "-c:a", "copy")
		} else {
			args = append(args, "-c:a", "aac", "-b:a", "128k")
		}

		args = append(args, "-movflags", "+faststart", "-shortest", outPath)

		if global.Stats != nil {
			global.Stats.IncFFmpegActive()
			defer global.Stats.DecFFmpegActive()
		}

		cmd := exec.CommandContext(ctx, p.ffmpegPath, args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			msg := strings.TrimSpace(string(out))
			if msg != "" {
				return fmt.Errorf("ffmpeg watermark failed: %w: %s", err, msg)
			}
			return fmt.Errorf("ffmpeg watermark failed: %w", err)
		}
		return nil
	}

	// Try copy audio first, then fallback to AAC if needed.
	if err := runOnce(true); err != nil {
		if ctx.Err() != nil {
			_ = cleanupAll()
			return inPath, nil, false, ctx.Err()
		}
		if err2 := runOnce(false); err2 != nil {
			_ = cleanupAll()
			return inPath, nil, false, err2
		}
	}

	return outPath, cleanupAll, true, nil
}

// ExtractCover extracts a single frame as JPEG/PNG using FFmpeg.
// Returns (generated, error). When processor is disabled, it returns (false, nil).
func (p *VideoProcessor) ExtractCover(ctx context.Context, videoPath, outImagePath string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if p == nil || !p.enabled || !p.extractCover {
		return false, nil
	}
	videoPath = strings.TrimSpace(videoPath)
	outImagePath = strings.TrimSpace(outImagePath)
	if videoPath == "" || outImagePath == "" {
		return false, errors.New("empty video/cover path")
	}

	if err := os.MkdirAll(filepath.Dir(outImagePath), 0o700); err != nil {
		return false, err
	}

	args := []string{"-y", "-hide_banner", "-loglevel", "error"}
	if p.coverTimestampSec > 0 {
		args = append(args, "-ss", formatSeconds(p.coverTimestampSec))
	}
	args = append(args,
		"-i", videoPath,
		"-map", "0:v:0",
		"-an",
		"-frames:v", "1",
		outImagePath,
	)

	if global.Stats != nil {
		global.Stats.IncFFmpegActive()
		defer global.Stats.DecFFmpegActive()
	}

	cmd := exec.CommandContext(ctx, p.ffmpegPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return false, fmt.Errorf("ffmpeg extract cover failed: %w: %s", err, msg)
		}
		return false, fmt.Errorf("ffmpeg extract cover failed: %w", err)
	}
	return true, nil
}

func formatSeconds(sec float64) string {
	// FFmpeg accepts seconds with decimals. Keep it stable.
	s := strconv.FormatFloat(sec, 'f', 3, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" {
		return "0"
	}
	return s
}
