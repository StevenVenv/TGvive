package engine

import (
	"encoding/json"
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
	if strings.HasPrefix(s, "#") {
		s = strings.TrimPrefix(s, "#")
	}
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

func normalizeRuntimeWatermarkRule(in model.WatermarkRule) model.WatermarkRule {
	out := in
	out.Type = strings.ToLower(strings.TrimSpace(out.Type))
	out.Position = strings.ToLower(strings.TrimSpace(out.Position))
	out.Text = strings.TrimSpace(out.Text)
	out.TextStyle = strings.ToLower(strings.TrimSpace(out.TextStyle))
	out.TextColor = normalizeHexColor(out.TextColor, "#FFFFFF")
	out.StrokeColor = normalizeHexColor(out.StrokeColor, "#000000")
	out.ShadowColor = normalizeHexColor(out.ShadowColor, "#000000")
	out.FontPath = strings.TrimSpace(out.FontPath)
	out.ImagePath = strings.TrimSpace(out.ImagePath)

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

func resolveWatermarkRule(task model.Task, st *model.Strategy) (rule model.WatermarkRule, enabled bool, key string) {
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
