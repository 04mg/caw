package agents

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FindLatestFile finds the most recently modified file with the given extension
func FindLatestFile(baseDir string, ext string, after time.Time) (string, time.Time, error) {
	var latestPath string
	var latestMod time.Time

	err := filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		name := strings.ToLower(info.Name())
		if strings.HasSuffix(name, ext) {
			if info.ModTime().After(after) && info.ModTime().After(latestMod) {
				latestPath = path
				latestMod = info.ModTime()
			}
		}
		return nil
	})

	return latestPath, latestMod, err
}

// ReadNewLines reads newly appended bytes of a file and returns them split by newline
func ReadNewLines(filePath string, fromOffset int64) ([]string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	_, err = f.Seek(fromOffset, 0)
	if err != nil {
		return nil, err
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	var result []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result, nil
}
