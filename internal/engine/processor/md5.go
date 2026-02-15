package processor

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
)

// ModifyFileMD5 appends random hex bytes to the file tail to change its MD5 hash.
// It does not change existing bytes (lossless for most media formats that ignore trailing data).
func ModifyFileMD5(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("empty file path")
	}
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if st.IsDir() {
		return fmt.Errorf("path is a directory: %q", path)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	// Random hex length: [16..64] chars (8..32 bytes), to look more "real".
	nBytes, err := randIntRange(8, 32)
	if err != nil {
		return err
	}
	buf := make([]byte, nBytes)
	if _, err := rand.Read(buf); err != nil {
		return err
	}
	hexStr := hex.EncodeToString(buf)

	if _, err := f.WriteString(hexStr); err != nil {
		return err
	}
	return nil
}

func randIntRange(min, max int) (int, error) {
	if min <= 0 || max < min {
		return 0, errors.New("invalid random range")
	}
	n := int64(max - min + 1)
	v, err := rand.Int(rand.Reader, big.NewInt(n))
	if err != nil {
		return 0, err
	}
	return min + int(v.Int64()), nil
}
