package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupStaleTempMedia(t *testing.T) {
	root := t.TempDir()

	mustWrite := func(path string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	mustWrite(filepath.Join(root, "task_1", "video.mp4"))
	mustWrite(filepath.Join(root, "task_abc", "watermarked.mp4"))
	mustWrite(filepath.Join(root, "tgthumb_123.jpg"))
	mustWrite(filepath.Join(root, "keep", "file.txt"))
	mustWrite(filepath.Join(root, "other.mp4"))

	removed, err := cleanupStaleTempMedia(root)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if removed != 3 {
		t.Fatalf("removed mismatch: got %d want 3", removed)
	}

	for _, path := range []string{
		filepath.Join(root, "task_1"),
		filepath.Join(root, "task_abc"),
		filepath.Join(root, "tgthumb_123.jpg"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, stat err=%v", path, err)
		}
	}

	for _, path := range []string{
		filepath.Join(root, "keep", "file.txt"),
		filepath.Join(root, "other.mp4"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to remain: %v", path, err)
		}
	}
}

func TestCleanupStaleTempMediaRefusesUnsafeRoot(t *testing.T) {
	for _, root := range []string{"", ".", string(os.PathSeparator)} {
		if _, err := cleanupStaleTempMedia(root); err == nil {
			t.Fatalf("expected unsafe root %q to fail", root)
		}
	}
}
