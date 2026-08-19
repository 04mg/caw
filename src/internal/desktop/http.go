package desktop

import (
	"io"
	"net/http"
	"time"
)

// httpClient is a short-timeout client for provisioning downloads (the
// xpra GPG key is tiny).
var httpClient = &http.Client{Timeout: 30 * time.Second}

// copyFile is a small helper wrapping io.Copy so provision.go doesn't
// pull in io on its own import list for a single call.
func copyFile(dst io.Writer, src io.Reader) (int64, error) {
	return io.Copy(dst, src)
}