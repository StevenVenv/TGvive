package processor

import (
	"crypto/md5"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestModifyFileMD5_ChangesHashAndAppendsHex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.bin")
	if err := os.WriteFile(path, []byte("hello world"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	beforeSum, beforeSize := fileMD5AndSize(t, path)

	if err := ModifyFileMD5(path); err != nil {
		t.Fatalf("ModifyFileMD5: %v", err)
	}

	afterSum, afterSize := fileMD5AndSize(t, path)

	if beforeSum == afterSum {
		t.Fatalf("expected md5 to change, got same: %s", beforeSum)
	}

	delta := afterSize - beforeSize
	if delta < 16 || delta > 64 || delta%2 != 0 {
		t.Fatalf("unexpected appended length: %d", delta)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	tail := b[len(b)-delta:]
	if _, err := hex.DecodeString(string(tail)); err != nil {
		t.Fatalf("appended tail is not hex: %v", err)
	}
}

func fileMD5AndSize(t *testing.T, path string) (sum string, size int) {
	t.Helper()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	h := md5.Sum(b)
	return hex.EncodeToString(h[:]), len(b)
}
