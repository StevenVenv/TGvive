package v1

import (
	"fmt"
	"os/exec"
	"strings"

	"my-go-server/internal/global"
	"my-go-server/pkg/app"

	"github.com/gin-gonic/gin"
)

type SystemApi struct{}

type systemCapabilities struct {
	FFmpeg ffmpegCapability `json:"ffmpeg"`
}

type ffmpegCapability struct {
	Enabled   bool   `json:"enabled"`
	Path      string `json:"path"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

func (a *SystemApi) Capabilities(c *gin.Context) {
	cfg := global.Config.Processor.Video

	ffmpegPath := strings.TrimSpace(cfg.FFmpegPath)
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}

	out := systemCapabilities{
		FFmpeg: ffmpegCapability{
			Enabled: cfg.Enabled,
			Path:    ffmpegPath,
		},
	}

	if !cfg.Enabled {
		out.FFmpeg.Available = false
		out.FFmpeg.Reason = "未开启视频处理器（processor.video.enabled=false）"
		app.OkWithData(out, c)
		return
	}

	if _, err := exec.LookPath(ffmpegPath); err != nil {
		out.FFmpeg.Available = false
		out.FFmpeg.Reason = fmt.Sprintf("未找到 FFmpeg：%s", ffmpegPath)
		app.OkWithData(out, c)
		return
	}

	out.FFmpeg.Available = true
	app.OkWithData(out, c)
}
