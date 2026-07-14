package workspace

import (
	"fmt"
	"os"
	"path/filepath"
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