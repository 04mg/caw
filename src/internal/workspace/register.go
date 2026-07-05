package workspace

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/04mg/caw/internal/httputil"
)

type FileNode struct {
	Name     string     `json:"name"`
	Path     string     `json:"path"`
	IsDir    bool       `json:"isDir"`
	Children []FileNode `json:"children,omitempty"`
}

type WriteRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type RenameRequest struct {
	OldPath string `json:"oldPath"`
	NewPath string `json:"newPath"`
}

type CopyRequest struct {
	SourcePath string `json:"sourcePath"`
	DestPath   string `json:"destPath"`
}

type DeleteRequest struct {
	Path string `json:"path"`
}

type CreateRequest struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

type PasteRequest struct {
	SourcePath string `json:"sourcePath"`
	TargetDir  string `json:"targetDir"`
}

func Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/workspace/open", handleOpenDir)
	mux.HandleFunc("/api/workspace/tree", handleFileTree)
	mux.HandleFunc("/api/workspace/list", handleListDir)
	mux.HandleFunc("/api/workspace/list-all", handleListAll)
	mux.HandleFunc("/api/workspace/search", handleSearchDirs)
	mux.HandleFunc("/api/workspace/search-all", handleSearchAll)
	mux.HandleFunc("/api/workspace/file/read", handleFileRead)
	mux.HandleFunc("/api/workspace/file/download", handleFileDownload)
	mux.HandleFunc("/api/workspace/file/write", handleFileWrite)
	mux.HandleFunc("/api/workspace/file/upload", handleFileUpload)
	mux.HandleFunc("/api/workspace/file/rename", handleFileRename)
	mux.HandleFunc("/api/workspace/file/copy", handleFileCopy)
	mux.HandleFunc("/api/workspace/file/delete", handleFileDelete)
	mux.HandleFunc("/api/workspace/file/create", handleFileCreate)
	mux.HandleFunc("/api/workspace/file/paste", handleFilePaste)
}

func handleOpenDir(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("path")
	if dir == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !info.IsDir() {
		http.Error(w, "not a directory", http.StatusBadRequest)
		return
	}
	httputil.WriteJSON(w, map[string]string{"path": abs})
}

func handleFileTree(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("path")
	if root == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	node, err := buildTree(abs, 2)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httputil.WriteJSON(w, node)
}

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
		if name[0] == '.' {
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

func handleListDir(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("path")
	if dir == "" {
		dir = "/"
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !info.IsDir() {
		http.Error(w, "not a directory", http.StatusBadRequest)
		return
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		httputil.WriteJSON(w, []FileNode{})
		return
	}
	var children []FileNode
	for _, e := range entries {
		name := e.Name()
		if name[0] == '.' && name != "." && name != ".." {
			continue
		}
		if !e.IsDir() {
			continue
		}
		children = append(children, FileNode{
			Name:  name,
			Path:  filepath.Join(abs, name),
			IsDir: true,
		})
	}
	httputil.WriteJSON(w, children)
}

func handleSearchDirs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	root := r.URL.Query().Get("root")
	if root == "" {
		root = "/"
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		httputil.WriteJSON(w, []FileNode{})
		return
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		httputil.WriteJSON(w, []FileNode{})
		return
	}

	results := []FileNode{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name[0] == '.' {
			continue
		}
		if q == "" || containsFold(name, q) {
			results = append(results, FileNode{
				Name:  name,
				Path:  filepath.Join(abs, name),
				IsDir: true,
			})
		}
	}

	httputil.WriteJSON(w, results)
}

func containsFold(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

func handleSearchAll(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	root := r.URL.Query().Get("root")
	if root == "" {
		root = "."
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		httputil.WriteJSON(w, []FileNode{})
		return
	}
	results := []FileNode{}
	searchAll(abs, q, &results, 0, 4, 30)
	httputil.WriteJSON(w, results)
}

func searchAll(dir, q string, results *[]FileNode, depth, maxDepth, maxResults int) {
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
		if name == "" || name[0] == '.' {
			continue
		}
		path := filepath.Join(dir, name)
		isDir := e.IsDir()

		if q == "" || containsFold(name, q) {
			*results = append(*results, FileNode{Name: name, Path: path, IsDir: isDir})
		}
		if isDir {
			searchAll(path, q, results, depth+1, maxDepth, maxResults)
		}
	}
}

func handleListAll(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("path")
	if dir == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !info.IsDir() {
		http.Error(w, "not a directory", http.StatusBadRequest)
		return
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		httputil.WriteJSON(w, []FileNode{})
		return
	}
	var children []FileNode
	for _, e := range entries {
		name := e.Name()
		if name[0] == '.' && name != "." && name != ".." {
			continue
		}
		isDir := e.IsDir()
		info, err := e.Info()
		if err == nil {
			isDir = info.IsDir()
		}
		children = append(children, FileNode{
			Name:  name,
			Path:  filepath.Join(abs, name),
			IsDir: isDir,
		})
	}
	httputil.WriteJSON(w, children)
}

func handleFileRead(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if info.IsDir() {
		http.Error(w, "cannot read directory", http.StatusBadRequest)
		return
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(content)
}

func handleFileDownload(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if info.IsDir() {
		http.Error(w, "cannot download directory", http.StatusBadRequest)
		return
	}
	f, err := os.Open(abs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()

	name := filepath.Base(abs)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	io.Copy(w, f)
}

func handleFileWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req WriteRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Path == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	abs, err := filepath.Abs(req.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	err = os.WriteFile(abs, []byte(req.Content), 0644)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httputil.WriteJSON(w, map[string]string{"status": "ok"})
}

func handleFileUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	err := r.ParseMultipartForm(32 << 20)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	targetDir := r.FormValue("targetDir")
	if targetDir == "" {
		http.Error(w, "targetDir required", http.StatusBadRequest)
		return
	}
	absDir, err := filepath.Abs(targetDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	dest := filepath.Join(absDir, header.Filename)
	dst, err := os.Create(dest)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	_, err = io.Copy(dst, file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	httputil.WriteJSON(w, map[string]string{"status": "ok"})
}

func handleFileRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req RenameRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.OldPath == "" || req.NewPath == "" {
		http.Error(w, "oldPath and newPath required", http.StatusBadRequest)
		return
	}
	absOld, err := filepath.Abs(req.OldPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	absNew, err := filepath.Abs(req.NewPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := os.Rename(absOld, absNew); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httputil.WriteJSON(w, map[string]string{"status": "ok"})
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

func handleFileCopy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req CopyRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.SourcePath == "" || req.DestPath == "" {
		http.Error(w, "sourcePath and destPath required", http.StatusBadRequest)
		return
	}
	absSrc, err := filepath.Abs(req.SourcePath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	absDst, err := filepath.Abs(req.DestPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	info, err := os.Stat(absSrc)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if info.IsDir() {
		if err := os.MkdirAll(absDst, 0755); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := copyDir(absSrc, absDst); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		if err := copyFile(absSrc, absDst); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	httputil.WriteJSON(w, map[string]string{"status": "ok"})
}

func handleFileDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req DeleteRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Path == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	abs, err := filepath.Abs(req.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := os.RemoveAll(abs); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httputil.WriteJSON(w, map[string]string{"status": "ok"})
}

func handleFileCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req CreateRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Path == "" || req.Type == "" {
		http.Error(w, "path and type required", http.StatusBadRequest)
		return
	}
	abs, err := filepath.Abs(req.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Type == "dir" {
		if err := os.MkdirAll(abs, 0755); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		if err := os.WriteFile(abs, []byte{}, 0644); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	httputil.WriteJSON(w, map[string]string{"status": "ok"})
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

func handleFilePaste(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req PasteRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.SourcePath == "" || req.TargetDir == "" {
		http.Error(w, "sourcePath and targetDir required", http.StatusBadRequest)
		return
	}
	absSrc, err := filepath.Abs(req.SourcePath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	absTarget, err := filepath.Abs(req.TargetDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	srcName := filepath.Base(absSrc)
	destPath := uniquePath(filepath.Join(absTarget, srcName))

	info, err := os.Stat(absSrc)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if info.IsDir() {
		if err := os.MkdirAll(destPath, 0755); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := copyDir(absSrc, destPath); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		if err := copyFile(absSrc, destPath); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	httputil.WriteJSON(w, map[string]string{"status": "ok"})
}
