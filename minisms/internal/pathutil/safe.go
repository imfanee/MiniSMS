// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package pathutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveUnder joins rel under baseDir and rejects paths that escape baseDir.
func ResolveUnder(baseDir, rel string) (string, error) {
	base, err := filepath.Abs(filepath.Clean(baseDir))
	if err != nil {
		return "", err
	}
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", fmt.Errorf("empty path")
	}
	rel = filepath.Clean(rel)
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute path not allowed")
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path traversal not allowed")
	}
	full := filepath.Clean(filepath.Join(base, rel))
	relToBase, err := filepath.Rel(base, full)
	if err != nil || relToBase == ".." || strings.HasPrefix(relToBase, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path outside base directory")
	}
	return full, nil
}

// ValidateRelativeDataPath ensures rel is a safe relative path under requiredPrefix (e.g. "assets").
func ValidateRelativeDataPath(rel, requiredPrefix string) error {
	rel = strings.TrimSpace(rel)
	if rel == "" || len(rel) > 500 {
		return fmt.Errorf("invalid path length")
	}
	clean := filepath.Clean(rel)
	if filepath.IsAbs(clean) {
		return fmt.Errorf("absolute path not allowed")
	}
	if clean == ".." || strings.Contains(clean, "..") {
		return fmt.Errorf("path traversal not allowed")
	}
	prefix := filepath.Clean(requiredPrefix)
	if clean != prefix && !strings.HasPrefix(clean, prefix+string(os.PathSeparator)) {
		return fmt.Errorf("path must be under %s/", requiredPrefix)
	}
	return nil
}
