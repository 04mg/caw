package workspace

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

func buildTree(dir string, depth int) (FileNode, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return FileNode{}, err
	}
	node := FileNode{
		Name:  filepath.Base(dir),
		Path:  dir,
		IsDir: info.IsDir(),
	}
	if !info.IsDir() || depth <= 0 {
		return node, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return node, nil
	}
	for _, e := range entries {
		name := e.Name()
		if name == "." || name == ".." {
			continue
		}
		if name == ".git" {
			continue
		}
		child, err := buildTree(filepath.Join(dir, name), depth-1)
		if err != nil {
			continue
		}
		node.Children = append(node.Children, child)
	}
	return node, nil
}

func searchAll(dir, q string, results *[]FileNode, depth, maxDepth, maxResults int) {
	searchAllRec(dir, dir, q, results, depth, maxDepth, maxResults)
}

func searchAllRec(root, dir, q string, results *[]FileNode, depth, maxDepth, maxResults int) {
	if depth > maxDepth || len(*results) >= maxResults {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if len(*results) >= maxResults {
			return
		}
		name := e.Name()
		if name == "" || name == "." || name == ".." {
			continue
		}
		if name == ".git" {
			continue
		}
		path := filepath.Join(dir, name)
		isDir := e.IsDir()

		relPath, relErr := filepath.Rel(root, path)
		if relErr != nil {
			relPath = name
		}

		if q == "" || containsFold(name, q) || containsFold(relPath, q) {
			*results = append(*results, FileNode{Name: name, Path: path, IsDir: isDir})
		}
		if isDir {
			searchAllRec(root, path, q, results, depth+1, maxDepth, maxResults)
		}
	}
}

const (
	searchMaxResults  = 500
	searchMaxFileSize = 4 << 20 // 4 MB
	searchMaxLineLen  = 2000
)

// searchContentOpts carries matching options for content searches.
type searchContentOpts struct {
	regex         bool
	caseSensitive bool
}

// searchFileContent scans a single text file for the query and appends hits
// to results. It returns true when the result cap is reached.
func searchFileContent(absPath, root, q string, opts searchContentOpts, results *[]SearchHit) (bool, error) {
	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		return false, nil
	}
	if info.Size() > searchMaxFileSize {
		return false, nil
	}

	var re *regexp.Regexp
	if opts.regex {
		pattern := q
		if !opts.caseSensitive {
			pattern = "(?i)" + pattern
		}
		re, err = regexp.Compile(pattern)
		if err != nil {
			return false, err
		}
	}

	f, err := os.Open(absPath)
	if err != nil {
		return false, nil
	}
	defer f.Close()

	if isBinaryFile(f) {
		return false, nil
	}
	// isBinaryFile consumed the first 8 KB of the stream; rewind so the
	// scanner sees the full file instead of starting partway through.
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return false, nil
	}

	absPathRel := filepath.ToSlash(absPath)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), searchMaxLineLen)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if line == "" {
			continue
		}

		if opts.regex {
			loc := re.FindStringIndex(line)
			if loc == nil {
				continue
			}
			*results = append(*results, SearchHit{
				Path:    absPathRel,
				Line:    lineNo,
				Column:  loc[0],
				Preview: trimPreview(line),
			})
		} else {
			var idx int
			if opts.caseSensitive {
				idx = strings.Index(line, q)
			} else {
				idx = strings.Index(strings.ToLower(line), strings.ToLower(q))
			}
			if idx == -1 {
				continue
			}
			*results = append(*results, SearchHit{
				Path:    absPathRel,
				Line:    lineNo,
				Column:  idx,
				Preview: trimPreview(line),
			})
		}
		if len(*results) >= searchMaxResults {
			return true, nil
		}
	}
	return len(*results) >= searchMaxResults, nil
}

// isBinaryFile reports whether the stream looks like binary data by checking
// for NUL bytes and UTF-8 validity in the first 8 KB.
func isBinaryFile(r io.Reader) bool {
	buf := make([]byte, 8192)
	n, _ := io.ReadFull(r, buf)
	if n == 0 {
		return false
	}
	buf = buf[:n]
	if strings.IndexByte(string(buf), 0) != -1 {
		return true
	}
	return !utf8.Valid(buf)
}

func trimPreview(line string) string {
	line = strings.TrimRight(line, "\r")
	line = strings.TrimSpace(line)
	if len(line) > 200 {
		line = line[:200]
	}
	return line
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFile(path, target)
	})
}

func uniquePath(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	name := base[:len(base)-len(ext)]
	for i := 1; ; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s(%d)%s", name, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}