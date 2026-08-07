package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type HistoryEntry struct {
	Type      string `json:"type"`      // "create", "rename", "delete", "paste", "replace"
	Path      string `json:"path"`      // Original/Old path
	DestPath  string `json:"destPath"`  // New path (for rename/paste)
	TrashPath string `json:"trashPath"` // Location of deleted file/folder in trash
	IsDir     bool   `json:"isDir"`
	Files     []ReplaceBackup `json:"files,omitempty"` // Content backups for batch replace
}

type HistoryManager struct {
	mu        sync.Mutex
	undoStack []HistoryEntry
	redoStack []HistoryEntry
}

var globalHistory = &HistoryManager{}

func (h *HistoryManager) PushUndo(entry HistoryEntry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.undoStack = append(h.undoStack, entry)
	// Clear redo stack on new user action
	h.redoStack = nil
}

func (h *HistoryManager) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.undoStack = nil
	h.redoStack = nil
}

func findWorkspaceRoot(path string) string {
	dir, err := filepath.Abs(path)
	if err != nil {
		dir = filepath.Clean(path)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return filepath.Dir(path)
}

func getTrashDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	trashDir := filepath.Join(home, ".caw", "trash")
	if err := os.MkdirAll(trashDir, 0755); err != nil {
		return "", err
	}
	return trashDir, nil
}

func moveFileOrDir(src, dest string) error {
	if err := os.Rename(src, dest); err == nil {
		return nil
	}
	// Fallback to copy + remove
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.MkdirAll(dest, 0755); err != nil {
			return err
		}
		if err := copyDir(src, dest); err != nil {
			os.RemoveAll(dest)
			return err
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return err
		}
		if err := copyFile(src, dest); err != nil {
			os.Remove(dest)
			return err
		}
	}
	return os.RemoveAll(src)
}

func moveToTrash(src string) (string, error) {
	trashDir, err := getTrashDir()
	if err != nil {
		return "", err
	}
	base := filepath.Base(src)
	timestamp := time.Now().UnixNano()
	trashName := fmt.Sprintf("%d_%s", timestamp, base)
	dest := filepath.Join(trashDir, trashName)
	if err := moveFileOrDir(src, dest); err != nil {
		return "", err
	}
	return dest, nil
}

func restoreFromTrash(trashPath, dest string) error {
	// Make sure the parent directory of dest exists
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	return moveFileOrDir(trashPath, dest)
}

func init() {
	go func() {
		// Run initial cleanup
		cleanupTrash()
		// Periodic cleanup every 5 minutes
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			cleanupTrash()
		}
	}()
}

func cleanupTrash() {
	trashDir, err := getTrashDir()
	if err != nil {
		return
	}
	entries, err := os.ReadDir(trashDir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-5 * time.Minute)
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		name := entry.Name()
		idx := strings.Index(name, "_")
		if idx != -1 {
			var nano int64
			_, err := fmt.Sscanf(name[:idx], "%d", &nano)
			if err == nil {
				t := time.Unix(0, nano)
				if t.Before(cutoff) {
					path := filepath.Join(trashDir, name)
					_ = os.RemoveAll(path)
				}
				continue
			}
		}
		// Fallback to ModTime
		if info.ModTime().Before(cutoff) {
			path := filepath.Join(trashDir, name)
			_ = os.RemoveAll(path)
		}
	}
}


func (h *HistoryManager) Undo() error {
	h.mu.Lock()
	if len(h.undoStack) == 0 {
		h.mu.Unlock()
		return fmt.Errorf("nothing to undo")
	}
	// Pop from undo
	entry := h.undoStack[len(h.undoStack)-1]
	h.undoStack = h.undoStack[:len(h.undoStack)-1]
	h.mu.Unlock()

	var err error
	var redoEntry HistoryEntry
	redoEntry.Type = entry.Type
	redoEntry.IsDir = entry.IsDir

	switch entry.Type {
	case "create":
		// Created at Path. Undo is to move it to trash.
		trashPath, renameErr := moveToTrash(entry.Path)
		if renameErr != nil {
			err = renameErr
		} else {
			redoEntry.Path = entry.Path
			redoEntry.TrashPath = trashPath
			getHub().EmitEvent(entry.Path, "file-deleted", entry.IsDir)
		}

	case "delete":
		// Deleted from Path, trash is at TrashPath. Undo is to restore it.
		if renameErr := restoreFromTrash(entry.TrashPath, entry.Path); renameErr != nil {
			err = renameErr
		} else {
			redoEntry.Path = entry.Path
			redoEntry.TrashPath = entry.TrashPath
			getHub().EmitEvent(entry.Path, "file-created", entry.IsDir)
		}

	case "rename":
		// Renamed from Path (old) to DestPath (new). Undo is to rename back.
		if renameErr := os.Rename(entry.DestPath, entry.Path); renameErr != nil {
			err = renameErr
		} else {
			redoEntry.Path = entry.Path
			redoEntry.DestPath = entry.DestPath
			getHub().EmitEvent(entry.DestPath, "file-deleted", entry.IsDir)
			getHub().EmitEvent(entry.Path, "file-created", entry.IsDir)
		}

	case "paste":
		// Pasted (copied) to DestPath. Undo is to move it to trash.
		trashPath, renameErr := moveToTrash(entry.DestPath)
		if renameErr != nil {
			err = renameErr
		} else {
			redoEntry.Path = entry.Path
			redoEntry.DestPath = entry.DestPath
			redoEntry.TrashPath = trashPath
			getHub().EmitEvent(entry.DestPath, "file-deleted", entry.IsDir)
		}

	case "replace":
		// Batch replace. Undo restores every file's original content.
		redoEntry.Files = make([]ReplaceBackup, 0, len(entry.Files))
		for _, b := range entry.Files {
			cur, readErr := os.ReadFile(b.Path)
			if readErr != nil {
				continue
			}
			if writeErr := os.WriteFile(b.Path, []byte(b.Old), 0644); writeErr != nil {
				continue
			}
			redoEntry.Files = append(redoEntry.Files, ReplaceBackup{Path: b.Path, Old: b.New, New: string(cur)})
			getHub().EmitEvent(b.Path, "file-modified", false)
		}
		if len(redoEntry.Files) == 0 {
			err = fmt.Errorf("nothing to undo")
		}
	}

	if err != nil {
		// Put it back on undo stack on failure so we don't lose it
		h.mu.Lock()
		h.undoStack = append(h.undoStack, entry)
		h.mu.Unlock()
		return err
	}

	h.mu.Lock()
	h.redoStack = append(h.redoStack, redoEntry)
	h.mu.Unlock()
	return nil
}

func (h *HistoryManager) Redo() error {
	h.mu.Lock()
	if len(h.redoStack) == 0 {
		h.mu.Unlock()
		return fmt.Errorf("nothing to redo")
	}
	// Pop from redo
	entry := h.redoStack[len(h.redoStack)-1]
	h.redoStack = h.redoStack[:len(h.redoStack)-1]
	h.mu.Unlock()

	var err error
	var undoEntry HistoryEntry
	undoEntry.Type = entry.Type
	undoEntry.IsDir = entry.IsDir

	switch entry.Type {
	case "create":
		// Created. Redo is to restore from trash to Path.
		if renameErr := restoreFromTrash(entry.TrashPath, entry.Path); renameErr != nil {
			err = renameErr
		} else {
			undoEntry.Path = entry.Path
			getHub().EmitEvent(entry.Path, "file-created", entry.IsDir)
		}

	case "delete":
		// Deleted. Redo is to move from Path back to trash.
		trashPath, renameErr := moveToTrash(entry.Path)
		if renameErr != nil {
			err = renameErr
		} else {
			undoEntry.Path = entry.Path
			undoEntry.TrashPath = trashPath
			getHub().EmitEvent(entry.Path, "file-deleted", entry.IsDir)
		}

	case "rename":
		// Renamed. Redo is to rename old (Path) to new (DestPath).
		if renameErr := os.Rename(entry.Path, entry.DestPath); renameErr != nil {
			err = renameErr
		} else {
			undoEntry.Path = entry.Path
			undoEntry.DestPath = entry.DestPath
			getHub().EmitEvent(entry.Path, "file-deleted", entry.IsDir)
			getHub().EmitEvent(entry.DestPath, "file-created", entry.IsDir)
		}

	case "paste":
		// Pasted. Redo is to restore from trash to DestPath.
		if renameErr := restoreFromTrash(entry.TrashPath, entry.DestPath); renameErr != nil {
			err = renameErr
		} else {
			undoEntry.Path = entry.Path
			undoEntry.DestPath = entry.DestPath
			getHub().EmitEvent(entry.DestPath, "file-created", entry.IsDir)
		}

	case "replace":
		// Batch replace. Redo re-applies the new content.
		undoEntry.Files = make([]ReplaceBackup, 0, len(entry.Files))
		for _, b := range entry.Files {
			if writeErr := os.WriteFile(b.Path, []byte(b.New), 0644); writeErr != nil {
				continue
			}
			undoEntry.Files = append(undoEntry.Files, b)
			getHub().EmitEvent(b.Path, "file-modified", false)
		}
		if len(undoEntry.Files) == 0 {
			err = fmt.Errorf("nothing to redo")
		}
	}

	if err != nil {
		// Put back on redo stack on failure
		h.mu.Lock()
		h.redoStack = append(h.redoStack, entry)
		h.mu.Unlock()
		return err
	}

	h.mu.Lock()
	h.undoStack = append(h.undoStack, undoEntry)
	h.mu.Unlock()
	return nil
}
