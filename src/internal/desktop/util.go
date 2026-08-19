package desktop

import "os"

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}