package httpx

import (
	"io"
	"io/fs"
	"net/http"
	"path"
	"path/filepath"
	"strings"
)

func SPAHandler(fsys fs.FS) http.HandlerFunc {
	fileServer := http.FileServerFS(fsys)
	const indexHTML = "index.html"

	return func(w http.ResponseWriter, r *http.Request) {
		cleanPath := path.Clean("/" + r.URL.Path)
		if ext := filepath.Ext(cleanPath); ext != "" && !strings.HasSuffix(cleanPath, "/") {
			fileServer.ServeHTTP(w, r)
			return
		}
		f, err := fsys.Open(indexHTML)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		stat, err := f.Stat()
		if err != nil {
			http.NotFound(w, r)
			return
		}
		rs, ok := f.(io.ReadSeeker)
		if !ok {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, indexHTML, stat.ModTime(), rs)
	}
}