package workspace

import (
	"os"
	"path/filepath"
	"strings"
)

type Service struct {
	hub     *FileEventHub
	history *HistoryManager
}

func NewService() *Service {
	return &Service{hub: getHub(), history: globalHistory}
}

type FileContentResponse struct {
	Content string `json:"content"`
}

func (s *Service) OpenDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", ErrNotDir
	}
	return abs, nil
}

func (s *Service) FileTree(path string) (FileNode, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return FileNode{}, err
	}
	return buildTree(abs, 2)
}

func (s *Service) ListDir(path string) ([]FileNode, error) {
	if path == "" {
		path = "/"
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, ErrNotDir
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return []FileNode{}, nil
	}
	children := []FileNode{}
	for _, e := range entries {
		name := e.Name()
		if name == "." || name == ".." {
			continue
		}
		if name == ".git" || name == ".caw_trash" {
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
	return children, nil
}

func (s *Service) SearchDirs(q, root string) ([]FileNode, error) {
	if root == "" {
		root = "/"
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return []FileNode{}, nil
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return []FileNode{}, nil
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
	return results, nil
}

func (s *Service) SearchAll(q, root string) ([]FileNode, error) {
	if root == "" {
		root = "."
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return []FileNode{}, nil
	}
	results := []FileNode{}
	searchAll(abs, q, &results, 0, 4, 30)
	return results, nil
}

func (s *Service) ListAll(path string) ([]FileNode, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, ErrNotDir
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return []FileNode{}, nil
	}
	children := []FileNode{}
	for _, e := range entries {
		name := e.Name()
		if name == "." || name == ".." {
			continue
		}
		if name == ".git" {
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
	return children, nil
}

func (s *Service) ReadFile(path string) (*FileContentResponse, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, ErrReadDir
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	return &FileContentResponse{Content: string(content)}, nil
}

func (s *Service) WriteFile(req WriteRequest) error {
	abs, err := filepath.Abs(req.Path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(abs, []byte(req.Content), 0644); err != nil {
		return err
	}
	s.hub.EmitEvent(abs, "file-modified", false)
	return nil
}

func (s *Service) Upload(targetDir, filename string, fileContent []byte) error {
	absDir, err := filepath.Abs(targetDir)
	if err != nil {
		return err
	}
	dest := filepath.Join(absDir, filename)
	if err := os.WriteFile(dest, fileContent, 0644); err != nil {
		return err
	}
	s.hub.EmitEvent(dest, "file-created", false)
	return nil
}

func (s *Service) Rename(req RenameRequest) error {
	absOld, err := filepath.Abs(req.OldPath)
	if err != nil {
		return err
	}
	absNew, err := filepath.Abs(req.NewPath)
	if err != nil {
		return err
	}
	wasDir := false
	if info, err := os.Stat(absOld); err == nil {
		wasDir = info.IsDir()
	}
	if err := os.Rename(absOld, absNew); err != nil {
		return err
	}
	s.history.PushUndo(HistoryEntry{
		Type:     "rename",
		Path:     absOld,
		DestPath: absNew,
		IsDir:    wasDir,
	})
	s.hub.EmitEvent(absOld, "file-deleted", wasDir)
	s.hub.EmitEvent(absNew, "file-created", wasDir)
	return nil
}

func (s *Service) Copy(req CopyRequest) error {
	absSrc, err := filepath.Abs(req.SourcePath)
	if err != nil {
		return err
	}
	absDst, err := filepath.Abs(req.DestPath)
	if err != nil {
		return err
	}
	info, err := os.Stat(absSrc)
	if err != nil {
		return err
	}
	isDir := info.IsDir()
	if isDir {
		if err := os.MkdirAll(absDst, 0755); err != nil {
			return err
		}
		if err := copyDir(absSrc, absDst); err != nil {
			return err
		}
	} else {
		if err := copyFile(absSrc, absDst); err != nil {
			return err
		}
	}
	s.hub.EmitEvent(absDst, "file-created", isDir)
	return nil
}

func (s *Service) Delete(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	wasDir := true
	if info, err := os.Stat(abs); err == nil {
		wasDir = info.IsDir()
	}
	trashPath, err := moveToTrash(abs)
	if err != nil {
		return err
	}
	s.history.PushUndo(HistoryEntry{
		Type:      "delete",
		Path:      abs,
		TrashPath: trashPath,
		IsDir:     wasDir,
	})
	s.hub.EmitEvent(abs, "file-deleted", wasDir)
	return nil
}

func (s *Service) Create(req CreateRequest) error {
	abs, err := filepath.Abs(req.Path)
	if err != nil {
		return err
	}
	isDir := req.Type == "dir"
	if isDir {
		if err := os.MkdirAll(abs, 0755); err != nil {
			return err
		}
	} else {
		if err := os.WriteFile(abs, []byte{}, 0644); err != nil {
			return err
		}
	}
	s.history.PushUndo(HistoryEntry{
		Type:  "create",
		Path:  abs,
		IsDir: isDir,
	})
	s.hub.EmitEvent(abs, "file-created", isDir)
	return nil
}

func (s *Service) Paste(req PasteRequest) error {
	absSrc, err := filepath.Abs(req.SourcePath)
	if err != nil {
		return err
	}
	absTarget, err := filepath.Abs(req.TargetDir)
	if err != nil {
		return err
	}
	srcName := filepath.Base(absSrc)
	destPath := uniquePath(filepath.Join(absTarget, srcName))

	info, err := os.Stat(absSrc)
	if err != nil {
		return err
	}
	isDir := info.IsDir()
	if isDir {
		if err := os.MkdirAll(destPath, 0755); err != nil {
			return err
		}
		if err := copyDir(absSrc, destPath); err != nil {
			return err
		}
	} else {
		if err := copyFile(absSrc, destPath); err != nil {
			return err
		}
	}
	s.history.PushUndo(HistoryEntry{
		Type:     "paste",
		Path:     absSrc,
		DestPath: destPath,
		IsDir:    isDir,
	})
	s.hub.EmitEvent(destPath, "file-created", isDir)
	return nil
}

func (s *Service) Undo() error  { return s.history.Undo() }
func (s *Service) Redo() error { return s.history.Redo() }

func containsFold(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}