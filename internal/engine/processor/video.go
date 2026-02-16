package processor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"my-go-server/internal/global"
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
