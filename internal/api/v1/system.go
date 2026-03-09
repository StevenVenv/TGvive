package v1

import (
	"fmt"
	"os/exec"

	"my-go-server/internal/engine/processor"
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
	_ = global.Config.Processor.Video
	ffmpegPath := processor.DefaultFFmpegPath

	out := systemCapabilities{
		FFmpeg: ffmpegCapability{
			Enabled: true,
			Path:    ffmpegPath,
		},
	}

	if _, err := exec.LookPath(ffmpegPath); err != nil {
		out.FFmpeg.Available = false
		out.FFmpeg.Reason = fmt.Sprintf("未找到系统 FFmpeg：%s", ffmpegPath)
		app.OkWithData(out, c)
		return
	}

	out.FFmpeg.Available = true
	app.OkWithData(out, c)
}
