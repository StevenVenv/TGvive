package model

// WatermarkRule controls per-strategy watermarking for images.
//
// It is stored as JSON in Strategy.WatermarkRule.
type WatermarkRule struct {
	Enable bool `json:"enable"`

	// Type: "text" or "image".
	Type string `json:"type"`

	// Text is used when Type == "text".
	Text string `json:"text"`

	// ImagePath is used when Type == "image". Must be an absolute local PNG path.
	ImagePath string `json:"image_path"`

	// Position: "bottom_right", "bottom_left", "top_right", "top_left", "center", "custom".
	Position string `json:"position"`

	// CustomX/CustomY are used when Position == "custom" (0..1).
	CustomX float64 `json:"custom_x"`
	CustomY float64 `json:"custom_y"`

	// Margin is a ratio of the base image width (0..1).
	Margin float64 `json:"margin"`

	// ScaleRatio is watermark width ratio relative to base image width (0..1).
	ScaleRatio float64 `json:"scale_ratio"`

	// Opacity is 0..1.
	Opacity float64 `json:"opacity"`
}
