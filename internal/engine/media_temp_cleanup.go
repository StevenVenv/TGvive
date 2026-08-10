package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CleanupStaleTempMedia removes leftover media transfer files from previous runs.
func CleanupStaleTempMedia() (int, error) {
	return cleanupStaleTempMedia(tmpMediaRoot)
}

func cleanupStaleTempMedia(root string) (int, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || root == "." || root == string(os.PathSeparator) {
		return 0, fmt.Errorf("refuse to clean unsafe temp media root %q", root)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}

	removed := 0
	var errs []error
	for _, entry := range entries {
		name := entry.Name()
		remove := false
		switch {
		case entry.IsDir() && strings.HasPrefix(name, "task_"):
			remove = true
		case !entry.IsDir() && strings.HasPrefix(name, "tgthumb_"):
			remove = true
		}
		if !remove {
			continue
		}

		path := filepath.Join(root, name)
		if err := os.RemoveAll(path); err != nil {
			errs = append(errs, fmt.Errorf("remove %s: %w", path, err))
			continue
		}
		removed++
	}

	return removed, errors.Join(errs...)
}
