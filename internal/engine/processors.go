package engine

import (
	"sync"

	"my-go-server/internal/engine/processor"
	"my-go-server/internal/global"

	"go.uber.org/zap"
)

type processorSet struct {
	Text       *processor.TextProcessor
	Image      *processor.ImageProcessor
	CoverImage *processor.ImageProcessor
	Video      *processor.VideoProcessor
}

var (
	procOnce sync.Once
	procs    processorSet
)

func (m *TaskManager) processors() processorSet {
	_ = m

	procOnce.Do(func() {
		cfg := global.Config.Processor

		procs.Text = processor.NewTextProcessor(cfg.Text)

		img, err := processor.NewImageProcessor(cfg.Image)
		if err != nil {
			if global.Logger != nil {
				global.Logger.Warn("init image processor failed, disabled", zap.Error(err))
			}
			disabled := cfg.Image
			disabled.Enabled = false
			img, _ = processor.NewImageProcessor(disabled)
		}
		procs.Image = img

		vid, err := processor.NewVideoProcessor(cfg.Video)
		if err != nil {
			if global.Logger != nil {
				global.Logger.Warn("init video processor failed, disabled", zap.Error(err))
			}
			vid = processor.NewDisabledVideoProcessor(cfg.Video)
		}
		procs.Video = vid

		// Cover image processing shares watermark settings but uses video-specific size/quality.
		if cfg.Video.ExtractCover {
			coverCfg := cfg.Image
			coverCfg.Enabled = true
			coverCfg.MaxWidth = cfg.Video.CoverMaxWidth
			coverCfg.MaxHeight = cfg.Video.CoverMaxHeight
			coverCfg.OutputQuality = cfg.Video.CoverOutputQuality

			cover, err := processor.NewImageProcessor(coverCfg)
			if err != nil {
				if global.Logger != nil {
					global.Logger.Warn("init cover image processor failed, disabled", zap.Error(err))
				}
				coverCfg.Enabled = false
				cover, _ = processor.NewImageProcessor(coverCfg)
			}
			procs.CoverImage = cover
		}

		if procs.Image == nil {
			disabled := cfg.Image
			disabled.Enabled = false
			procs.Image, _ = processor.NewImageProcessor(disabled)
		}
		if procs.Video == nil {
			procs.Video = processor.NewDisabledVideoProcessor(cfg.Video)
		}
		if procs.CoverImage == nil {
			disabled := cfg.Image
			disabled.Enabled = false
			procs.CoverImage, _ = processor.NewImageProcessor(disabled)
		}
	})

	return procs
}
