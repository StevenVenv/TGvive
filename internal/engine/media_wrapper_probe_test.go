package engine

import "testing"

func TestParseFFprobeRatio(t *testing.T) {
	tcs := []struct {
		in     string
		num    int64
		den    int64
		ok     bool
	}{
		{"16:15", 16, 15, true},
		{"16/15", 16, 15, true},
		{" 1:1 ", 1, 1, true},
		{"0:1", 0, 0, false},
		{"", 0, 0, false},
		{"bad", 0, 0, false},
	}
	for _, tc := range tcs {
		num, den, ok := parseFFprobeRatio(tc.in)
		if ok != tc.ok || num != tc.num || den != tc.den {
			t.Fatalf("parseFFprobeRatio(%q) = (%d,%d,%v), want (%d,%d,%v)", tc.in, num, den, ok, tc.num, tc.den, tc.ok)
		}
	}
}

func TestComputeVideoDisplaySizeFromProbe_SAR(t *testing.T) {
	p := ffprobeOut{
		Streams: []ffprobeStream{{
			Width:             720,
			Height:            576,
			SampleAspectRatio: "16:15",
		}},
	}
	w, h, err := computeVideoDisplaySizeFromProbe(p)
	if err != nil {
		t.Fatalf("computeVideoDisplaySizeFromProbe: %v", err)
	}
	if w != 768 || h != 576 {
		t.Fatalf("got %dx%d, want %dx%d", w, h, 768, 576)
	}
}

func TestComputeVideoDisplaySizeFromProbe_RotationSideData(t *testing.T) {
	rot := 90.0
	p := ffprobeOut{
		Streams: []ffprobeStream{{
			Width:       1920,
			Height:      1080,
			SideDataList: []ffprobeSideData{{Rotation: &rot}},
		}},
	}
	w, h, err := computeVideoDisplaySizeFromProbe(p)
	if err != nil {
		t.Fatalf("computeVideoDisplaySizeFromProbe: %v", err)
	}
	if w != 1080 || h != 1920 {
		t.Fatalf("got %dx%d, want %dx%d", w, h, 1080, 1920)
	}
}

func TestComputeVideoDisplaySizeFromProbe_RotationTagFallback(t *testing.T) {
	p := ffprobeOut{
		Streams: []ffprobeStream{{
			Width:  1280,
			Height: 720,
			Tags:   ffprobeStreamTags{Rotate: "270"},
		}},
	}
	w, h, err := computeVideoDisplaySizeFromProbe(p)
	if err != nil {
		t.Fatalf("computeVideoDisplaySizeFromProbe: %v", err)
	}
	if w != 720 || h != 1280 {
		t.Fatalf("got %dx%d, want %dx%d", w, h, 720, 1280)
	}
}

func TestComputeVideoDisplaySizeFromProbe_SARAndRotation(t *testing.T) {
	rot := 90.0
	p := ffprobeOut{
		Streams: []ffprobeStream{{
			Width:             720,
			Height:            576,
			SampleAspectRatio: "16:15",
			SideDataList:      []ffprobeSideData{{Rotation: &rot}},
		}},
	}
	w, h, err := computeVideoDisplaySizeFromProbe(p)
	if err != nil {
		t.Fatalf("computeVideoDisplaySizeFromProbe: %v", err)
	}
	if w != 576 || h != 768 {
		t.Fatalf("got %dx%d, want %dx%d", w, h, 576, 768)
	}
}

