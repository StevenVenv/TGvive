package v1

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"

	"my-go-server/pkg/app"

	"github.com/gin-gonic/gin"
	"golang.org/x/image/font/opentype"
)

type WatermarkApi struct{}

var pngMagic = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}

const maxWatermarkPNGSize = 5 * 1024 * 1024   // 5MB
const maxWatermarkFontSize = 10 * 1024 * 1024 // 10MB

func watermarkDir() string {
	return filepath.Join("data", "watermarks")
}

func watermarkFontDir() string {
	return filepath.Join(watermarkDir(), "fonts")
}

func safeBasename(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = filepath.Base(s)
	if strings.ContainsAny(s, `/\`) {
		return ""
	}
	return s
}

// UploadWatermarkPNG accepts a PNG file and stores it as a temp file on the server.
// It returns an absolute file path which can be used as Strategy.watermark_rule.image_path.
func (a *WatermarkApi) UploadWatermarkPNG(c *gin.Context) {
	fh, err := c.FormFile("file")
	if err != nil || fh == nil {
		app.FailWithMsg("请上传 PNG 文件（字段名：file）", c)
		return
	}
	if fh.Size <= 0 {
		app.FailWithMsg("上传文件为空", c)
		return
	}
	if fh.Size > maxWatermarkPNGSize {
		app.FailWithMsg("PNG 文件过大，最大 5MB", c)
		return
	}

	src, err := fh.Open()
	if err != nil {
		app.FailWithMsg("打开上传文件失败: "+err.Error(), c)
		return
	}
	defer func() { _ = src.Close() }()

	head := make([]byte, len(pngMagic))
	if _, err := io.ReadFull(src, head); err != nil {
		app.FailWithMsg("读取文件头失败: "+err.Error(), c)
		return
	}
	if !bytes.Equal(head, pngMagic) {
		app.FailWithMsg("仅支持 PNG 文件", c)
		return
	}

	dir := watermarkDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		app.FailWithMsg("创建目录失败: "+err.Error(), c)
		return
	}

	dst, err := os.CreateTemp(dir, "wm_*.png")
	if err != nil {
		app.FailWithMsg("创建临时文件失败: "+err.Error(), c)
		return
	}
	dstName := dst.Name()
	defer func() {
		_ = dst.Close()
	}()

	writeOK := false
	defer func() {
		if writeOK {
			return
		}
		_ = os.Remove(dstName)
	}()

	if _, err := dst.Write(head); err != nil {
		app.FailWithMsg("写入临时文件失败: "+err.Error(), c)
		return
	}
	if _, err := io.Copy(dst, src); err != nil {
		app.FailWithMsg("保存文件失败: "+err.Error(), c)
		return
	}
	if err := dst.Close(); err != nil {
		app.FailWithMsg("关闭文件失败: "+err.Error(), c)
		return
	}
	_ = os.Chmod(dstName, 0o644)

	abs, err := filepath.Abs(dstName)
	if err != nil {
		app.FailWithMsg("获取绝对路径失败: "+err.Error(), c)
		return
	}

	writeOK = true
	name := safeBasename(dstName)
	url := ""
	if name != "" {
		url = "/api/v1/watermarks/files/" + name
	}
	app.OkWithData(gin.H{"path": abs, "name": name, "url": url}, c)
}

// UploadWatermarkFont accepts a font file (.ttf/.otf) and stores it as a temp file on the server.
// It validates the font by parsing it via opentype.Parse.
func (a *WatermarkApi) UploadWatermarkFont(c *gin.Context) {
	fh, err := c.FormFile("file")
	if err != nil || fh == nil {
		app.FailWithMsg("请上传字体文件（字段名：file）", c)
		return
	}
	if fh.Size <= 0 {
		app.FailWithMsg("上传文件为空", c)
		return
	}
	if fh.Size > maxWatermarkFontSize {
		app.FailWithMsg("字体文件过大，最大 10MB", c)
		return
	}

	ext := strings.ToLower(filepath.Ext(fh.Filename))
	if ext != ".ttf" && ext != ".otf" {
		app.FailWithMsg("仅支持 .ttf/.otf 字体文件", c)
		return
	}

	src, err := fh.Open()
	if err != nil {
		app.FailWithMsg("打开上传文件失败: "+err.Error(), c)
		return
	}
	defer func() { _ = src.Close() }()

	b, err := io.ReadAll(io.LimitReader(src, maxWatermarkFontSize+1))
	if err != nil {
		app.FailWithMsg("读取字体文件失败: "+err.Error(), c)
		return
	}
	if len(b) == 0 {
		app.FailWithMsg("上传文件为空", c)
		return
	}
	if int64(len(b)) > maxWatermarkFontSize {
		app.FailWithMsg("字体文件过大，最大 10MB", c)
		return
	}
	if _, err := opentype.Parse(b); err != nil {
		app.FailWithMsg("字体解析失败: "+err.Error(), c)
		return
	}

	dir := watermarkFontDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		app.FailWithMsg("创建目录失败: "+err.Error(), c)
		return
	}

	dst, err := os.CreateTemp(dir, "font_*"+ext)
	if err != nil {
		app.FailWithMsg("创建临时文件失败: "+err.Error(), c)
		return
	}
	dstName := dst.Name()
	writeOK := false
	defer func() {
		_ = dst.Close()
		if writeOK {
			return
		}
		_ = os.Remove(dstName)
	}()

	if _, err := dst.Write(b); err != nil {
		app.FailWithMsg("写入临时文件失败: "+err.Error(), c)
		return
	}
	if err := dst.Close(); err != nil {
		app.FailWithMsg("关闭文件失败: "+err.Error(), c)
		return
	}
	_ = os.Chmod(dstName, 0o644)

	abs, err := filepath.Abs(dstName)
	if err != nil {
		app.FailWithMsg("获取绝对路径失败: "+err.Error(), c)
		return
	}

	writeOK = true
	name := safeBasename(dstName)
	url := ""
	if name != "" {
		url = "/api/v1/watermarks/fonts/" + name
	}
	app.OkWithData(gin.H{"path": abs, "name": name, "url": url}, c)
}

// GetWatermarkFile serves uploaded PNG files by filename.
func (a *WatermarkApi) GetWatermarkFile(c *gin.Context) {
	name := safeBasename(c.Param("name"))
	if name == "" || !strings.HasSuffix(strings.ToLower(name), ".png") {
		app.FailWithMsg("文件名不合法", c)
		return
	}
	p := filepath.Join(watermarkDir(), name)
	if _, err := os.Stat(p); err != nil {
		app.FailWithMsg("文件不存在", c)
		return
	}
	c.Header("Content-Type", "image/png")
	c.File(p)
}

// GetWatermarkFont serves uploaded font files by filename.
func (a *WatermarkApi) GetWatermarkFont(c *gin.Context) {
	name := safeBasename(c.Param("name"))
	if name == "" {
		app.FailWithMsg("文件名不合法", c)
		return
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".ttf" && ext != ".otf" {
		app.FailWithMsg("文件名不合法", c)
		return
	}
	p := filepath.Join(watermarkFontDir(), name)
	if _, err := os.Stat(p); err != nil {
		app.FailWithMsg("文件不存在", c)
		return
	}
	switch ext {
	case ".ttf":
		c.Header("Content-Type", "font/ttf")
	case ".otf":
		c.Header("Content-Type", "font/otf")
	default:
		c.Header("Content-Type", "application/octet-stream")
	}
	c.File(p)
}
