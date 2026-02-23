package engine

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"my-go-server/internal/model"
)

func clampFloat01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func normalizeHexColor(in string, def string) string {
	s := strings.TrimSpace(in)
	if s == "" {
		return def
	}
	s = strings.TrimPrefix(s, "#")
	s = strings.TrimSpace(s)
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		return def
	}
	for i := 0; i < 6; i++ {
		c := s[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			continue
		}
		return def
	}
	return "#" + strings.ToUpper(s)
}

func resolveUploadedWatermarkImagePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	norm := strings.ReplaceAll(p, "\\", "/")
	base := strings.TrimSpace(filepath.Base(norm))
	if base == "" || base == "." || base == ".." || base == "/" {
		return ""
	}

	// If user stores only filename (recommended) or an old absolute path that contains our watermark dir,
	// resolve it to project-relative data path to avoid leaking local machine absolute paths.
	if !strings.ContainsAny(p, `/\`) || strings.Contains(norm, "/data/watermarks/") || strings.HasPrefix(norm, "data/watermarks/") {
		if !strings.HasSuffix(strings.ToLower(base), ".png") {
			return ""
		}
		return filepath.Join("data", "watermarks", base)
	}
	return p
}

func resolveUploadedWatermarkFontPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	norm := strings.ReplaceAll(p, "\\", "/")
	base := strings.TrimSpace(filepath.Base(norm))
	if base == "" || base == "." || base == ".." || base == "/" {
		return ""
	}

	if !strings.ContainsAny(p, `/\`) || strings.Contains(norm, "/data/watermarks/fonts/") || strings.HasPrefix(norm, "data/watermarks/fonts/") {
		ext := strings.ToLower(filepath.Ext(base))
		if ext != ".ttf" && ext != ".otf" {
			return ""
		}
		return filepath.Join("data", "watermarks", "fonts", base)
	}
	return p
}

func normalizeRuntimeWatermarkRule(in model.WatermarkRule) model.WatermarkRule {
	out := in
	out.Type = strings.ToLower(strings.TrimSpace(out.Type))
	out.Position = strings.ToLower(strings.TrimSpace(out.Position))
	out.Text = strings.TrimSpace(out.Text)
	out.TextStyle = strings.ToLower(strings.TrimSpace(out.TextStyle))
	out.TextColor = normalizeHexColor(out.TextColor, "#FFFFFF")
	out.StrokeColor = normalizeHexColor(out.StrokeColor, "#000000")
	out.ShadowColor = normalizeHexColor(out.ShadowColor, "#000000")
	out.FontPath = resolveUploadedWatermarkFontPath(out.FontPath)
	out.ImagePath = resolveUploadedWatermarkImagePath(out.ImagePath)

	if out.Type == "" {
		if out.ImagePath != "" {
			out.Type = "image"
		} else {
			out.Type = "text"
		}
	}
	switch out.Type {
	case "text", "image":
	default:
		if out.ImagePath != "" {
			out.Type = "image"
		} else {
			out.Type = "text"
		}
	}

	switch out.TextStyle {
	case "plain", "stroke", "shadow", "stroke_shadow":
	default:
		out.TextStyle = "stroke"
	}

	switch out.Position {
	case "bottom_right", "bottom_left", "top_right", "top_left", "center", "custom":
	default:
		out.Position = "bottom_right"
	}

	out.CustomX = clampFloat01(out.CustomX)
	out.CustomY = clampFloat01(out.CustomY)

	out.Margin = clampFloat01(out.Margin)
	if out.Margin == 0 {
		out.Margin = 0.02
	}
	if out.Margin > 0.1 {
		out.Margin = 0.1
	}

	out.ScaleRatio = clampFloat01(out.ScaleRatio)
	if out.ScaleRatio == 0 {
		if out.Type == "image" {
			out.ScaleRatio = 0.15
		} else {
			out.ScaleRatio = 0.03
		}
	}
	if out.ScaleRatio > 0.5 {
		out.ScaleRatio = 0.5
	}

	out.Opacity = clampFloat01(out.Opacity)
	if out.Opacity == 0 {
		out.Opacity = 0.35
	}

	return out
}

func resolveWatermarkRule(st *model.Strategy) (rule model.WatermarkRule, enabled bool, key string) {
	if st != nil && len(st.WatermarkRule) > 0 && strings.TrimSpace(string(st.WatermarkRule)) != "null" {
		var r model.WatermarkRule
		if err := json.Unmarshal(st.WatermarkRule, &r); err == nil {
			r = normalizeRuntimeWatermarkRule(r)
			if !r.Enable {
				return model.WatermarkRule{}, false, "off"
			}
			b, _ := json.Marshal(r)
			return r, true, string(b)
		}
	}
	return model.WatermarkRule{}, false, "off"
}

func normalizeRuntimeVideoWatermarkRule(in model.VideoWatermarkRule) model.VideoWatermarkRule {
	out := in
	out.Type = strings.ToLower(strings.TrimSpace(out.Type))
	out.Position = strings.ToLower(strings.TrimSpace(out.Position))
	out.Motion = strings.ToLower(strings.TrimSpace(out.Motion))
	out.Text = strings.TrimSpace(out.Text)
	out.TextStyle = strings.ToLower(strings.TrimSpace(out.TextStyle))
	out.TextColor = normalizeHexColor(out.TextColor, "#FFFFFF")
	out.StrokeColor = normalizeHexColor(out.StrokeColor, "#000000")
	out.ShadowColor = normalizeHexColor(out.ShadowColor, "#000000")
	out.FontPath = resolveUploadedWatermarkFontPath(out.FontPath)
	out.ImagePath = resolveUploadedWatermarkImagePath(out.ImagePath)

	if out.Type == "" {
		if out.ImagePath != "" {
			out.Type = "image"
		} else {
			out.Type = "text"
		}
	}
	switch out.Type {
	case "text", "image":
	default:
		if out.ImagePath != "" {
			out.Type = "image"
		} else {
			out.Type = "text"
		}
	}

	switch out.TextStyle {
	case "plain", "stroke", "shadow", "stroke_shadow":
	default:
		out.TextStyle = "stroke"
	}

	switch out.Position {
	case "bottom_right", "bottom_left", "top_right", "top_left", "center", "custom":
	default:
		out.Position = "bottom_right"
	}

	out.CustomX = clampFloat01(out.CustomX)
	out.CustomY = clampFloat01(out.CustomY)

	out.Margin = clampFloat01(out.Margin)
	if out.Margin == 0 {
		out.Margin = 0.02
	}
	if out.Margin > 0.1 {
		out.Margin = 0.1
	}

	out.ScaleRatio = clampFloat01(out.ScaleRatio)
	if out.ScaleRatio == 0 {
		if out.Type == "image" {
			out.ScaleRatio = 0.15
		} else {
			out.ScaleRatio = 0.03
		}
	}
	if out.ScaleRatio < 0.01 {
		out.ScaleRatio = 0.01
	}
	if out.ScaleRatio > 0.5 {
		out.ScaleRatio = 0.5
	}

	out.Opacity = clampFloat01(out.Opacity)
	if out.Opacity == 0 {
		out.Opacity = 0.35
	}

	switch out.Motion {
	case "", "bounce":
		out.Motion = "bounce"
	case "static":
		// ok
	default:
		out.Motion = "bounce"
	}

	if out.MotionPeriodSec <= 0 {
		out.MotionPeriodSec = 12
	}
	if out.MotionPeriodSec < 2 {
		out.MotionPeriodSec = 2
	}
	if out.MotionPeriodSec > 120 {
		out.MotionPeriodSec = 120
	}

	return out
}

func resolveVideoWatermarkRule(st *model.Strategy) (rule model.VideoWatermarkRule, enabled bool, key string) {
	if st != nil && len(st.VideoWatermarkRule) > 0 && strings.TrimSpace(string(st.VideoWatermarkRule)) != "null" {
		var r model.VideoWatermarkRule
		if err := json.Unmarshal(st.VideoWatermarkRule, &r); err == nil {
			r = normalizeRuntimeVideoWatermarkRule(r)
			if !r.Enable {
				return model.VideoWatermarkRule{}, false, "off"
			}
			b, _ := json.Marshal(r)
			return r, true, string(b)
		}
	}
	return model.VideoWatermarkRule{}, false, "off"
}
