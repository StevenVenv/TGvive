package processor

import (
	"strings"
	"testing"

	"my-go-server/internal/model"
)

func TestBuildImageOverlayFilter_Bounce(t *testing.T) {
	rule := model.VideoWatermarkRule{
		Type:            "image",
		ImagePath:       "data/watermarks/wm.png",
		Margin:          0.02,
		ScaleRatio:      0.15,
		Opacity:         0.35,
		Motion:          "bounce",
		MotionPeriodSec: 12,
	}

	filter, mapLabel, err := buildImageOverlayFilter(rule, rule.ImagePath)
	if err != nil {
		t.Fatalf("buildImageOverlayFilter: %v", err)
	}
	if mapLabel != "[outv]" {
		t.Fatalf("unexpected map label: %q", mapLabel)
	}

	needles := []string{
		"format=rgba",
		"colorchannelmixer=aa=0.35",
		"scale2ref=w=ref_w*0.15",
		"overlay=x='",
		"shortest=1",
		"sin(2*PI*t/12)",
		"PI/2",
		"[outv]",
	}
	for _, n := range needles {
		if !strings.Contains(filter, n) {
			t.Fatalf("expected filter to contain %q, got: %s", n, filter)
		}
	}
}

func TestFFmpegOverlayXYExpr_StaticPositions(t *testing.T) {
	base := model.VideoWatermarkRule{
		Margin:   0.02,
		Motion:   "static",
		Position: "bottom_right",
	}

	cases := []struct {
		name string
		rule model.VideoWatermarkRule
		x    string
		y    string
	}{
		{
			name: "bottom_right",
			rule: base,
			x:    "W-w-(W*0.02)",
			y:    "H-h-(W*0.02)",
		},
		{
			name: "top_left",
			rule: model.VideoWatermarkRule{Margin: 0.02, Motion: "static", Position: "top_left"},
			x:    "(W*0.02)",
			y:    "(W*0.02)",
		},
		{
			name: "center",
			rule: model.VideoWatermarkRule{Margin: 0.02, Motion: "static", Position: "center"},
			x:    "(W-w)/2",
			y:    "(H-h)/2",
		},
		{
			name: "custom",
			rule: model.VideoWatermarkRule{Margin: 0.02, Motion: "static", Position: "custom", CustomX: 0.3, CustomY: 0.4},
			x:    "min(max(W*0.3,0),W-w)",
			y:    "min(max(H*0.4,0),H-h)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			x, y := ffmpegOverlayXYExpr(tc.rule)
			if x != tc.x || y != tc.y {
				t.Fatalf("unexpected expr: x=%q y=%q (want x=%q y=%q)", x, y, tc.x, tc.y)
			}
		})
	}
}

func TestBuildDrawTextFilter_BounceAndStyleMapping(t *testing.T) {
	base := model.VideoWatermarkRule{
		Type:            "text",
		Text:            "hello\nworld",
		TextColor:       "#FFFFFF",
		StrokeColor:     "#000000",
		ShadowColor:     "#000000",
		Margin:          0.02,
		ScaleRatio:      0.03,
		Opacity:         0.35,
		Motion:          "bounce",
		MotionPeriodSec: 10,
	}

	rule := base
	rule.TextStyle = "stroke_shadow"
	filter, err := buildDrawTextFilter(rule, "/tmp/wm.txt", "/tmp/font.ttf")
	if err != nil {
		t.Fatalf("buildDrawTextFilter: %v", err)
	}

	needles := []string{
		"drawtext:",
		"textfile=",
		"fontfile=",
		"fontsize='max(8,w*0.03)'",
		"fontcolor=#FFFFFF@0.35",
		"sin(2*PI*t/10)",
		"PI/2",
		"borderw=2",
		"shadowx=2",
	}
	for _, n := range needles {
		if !strings.Contains(filter, n) {
			t.Fatalf("expected filter to contain %q, got: %s", n, filter)
		}
	}

	plain := base
	plain.TextStyle = "plain"
	plainFilter, err := buildDrawTextFilter(plain, "/tmp/wm.txt", "/tmp/font.ttf")
	if err != nil {
		t.Fatalf("buildDrawTextFilter plain: %v", err)
	}
	if strings.Contains(plainFilter, "borderw=") || strings.Contains(plainFilter, "shadowx=") {
		t.Fatalf("expected plain style to have no stroke/shadow, got: %s", plainFilter)
	}
}

func TestVideoWatermarkExprAndFilter_Clamp(t *testing.T) {
	rule := model.VideoWatermarkRule{
		Type:            "image",
		ImagePath:       "data/watermarks/wm.png",
		Margin:          0.9,
		ScaleRatio:      2,
		Opacity:         9,
		Motion:          "bounce",
		MotionPeriodSec: 999,
	}

	filter, _, err := buildImageOverlayFilter(rule, rule.ImagePath)
	if err != nil {
		t.Fatalf("buildImageOverlayFilter: %v", err)
	}

	needles := []string{
		"W*0.1",           // margin clamped to 0.1
		"ref_w*0.5",       // scale clamped to 0.5
		"aa=1",            // opacity clamped to 1
		"sin(2*PI*t/120)", // period clamped to 120
	}
	for _, n := range needles {
		if !strings.Contains(filter, n) {
			t.Fatalf("expected filter to contain %q, got: %s", n, filter)
		}
	}

	txt := model.VideoWatermarkRule{
		Type:            "text",
		Text:            "x",
		Margin:          0.9,
		ScaleRatio:      2,
		Opacity:         9,
		Motion:          "bounce",
		MotionPeriodSec: 999,
		TextStyle:       "stroke",
		TextColor:       "#FFFFFF",
		StrokeColor:     "#000000",
	}

	vf, err := buildDrawTextFilter(txt, "/tmp/wm.txt", "/tmp/font.ttf")
	if err != nil {
		t.Fatalf("buildDrawTextFilter: %v", err)
	}
	for _, n := range []string{"w*0.1", "w*0.5", "@1", "sin(2*PI*t/120)"} {
		if !strings.Contains(vf, n) {
			t.Fatalf("expected vf to contain %q, got: %s", n, vf)
		}
	}
}
