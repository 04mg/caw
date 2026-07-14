package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTrashAndCleanup(t *testing.T) {
	// Set HOME to a temporary directory so we don't mess up actual home directory
	tempHome, err := os.MkdirTemp("", "caw_home_test")
	if err != nil {
		t.Fatalf("failed to create temp home: %v", err)
	}
	defer os.RemoveAll(tempHome)

	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tempHome)
	defer os.Setenv("HOME", oldHome)

	// Create a dummy workspace file
	tempDir, err := os.MkdirTemp("", "caw_ws_test")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	defer os.RemoveAll(tempDir)

	srcFile := filepath.Join(tempDir, "test.txt")
	err = os.WriteFile(srcFile, []byte("hello world"), 0644)
	if err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// 1. Test moveToTrash
	trashPath, err := moveToTrash(srcFile)
	if err != nil {
		t.Fatalf("moveToTrash failed: %v", err)
	}

	// Verify file is gone from src
	if _, err := os.Stat(srcFile); !os.IsNotExist(err) {
		t.Fatalf("expected src file to be deleted, but it exists")
	}

	// Verify file is in trash
	if _, err := os.Stat(trashPath); err != nil {
		t.Fatalf("expected file to be in trash: %v", err)
	}

	// 2. Test restoreFromTrash
	err = restoreFromTrash(trashPath, srcFile)
	if err != nil {
		t.Fatalf("restoreFromTrash failed: %v", err)
	}

	// Verify file is back at src
	data, err := os.ReadFile(srcFile)
	if err != nil {
		t.Fatalf("failed to read restored file: %v", err)
	}
	if string(data) != "hello world" {
		t.Fatalf("expected content 'hello world', got '%s'", string(data))
	}

	// Verify file is gone from trash
	if _, err := os.Stat(trashPath); !os.IsNotExist(err) {
		t.Fatalf("expected trash path to be cleared, but it exists")
	}

	// 3. Test cleanupTrash
	// Move file to trash again
	trashPath, err = moveToTrash(srcFile)
	if err != nil {
		t.Fatalf("moveToTrash failed: %v", err)
	}

	// Make the file look older than 5 minutes by changing its timestamp prefix in name
	trashDir, err := getTrashDir()
	if err != nil {
		t.Fatalf("failed to get trash dir: %v", err)
	}
	oldTimestamp := time.Now().Add(-6 * time.Minute).UnixNano()
	oldTrashName := fmt.Sprintf("%d_%s", oldTimestamp, "test.txt")
	oldTrashPath := filepath.Join(trashDir, oldTrashName)

	err = os.Rename(trashPath, oldTrashPath)
	if err != nil {
		t.Fatalf("failed to rename trash item to old name: %v", err)
	}

	// Run cleanup
	cleanupTrash()

	// Verify old file is gone
	if _, err := os.Stat(oldTrashPath); !os.IsNotExist(err) {
		t.Fatalf("expected old trash file to be cleaned up, but it still exists")
	}
}
