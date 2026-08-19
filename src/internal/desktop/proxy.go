package desktop

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
)

// newProxy builds a reverse proxy that forwards to the xpra WS server for
// the given session. It handles both plain HTTP (the HTML5 client assets)
// and WebSocket upgrades (the xpra display stream) by delegating to
// httputil.ReverseProxy, which supports WS when the request carries the
// Upgrade header.
func newProxy(sess *Session) *httputil.ReverseProxy {
	target, err := url.Parse(sess.proxyTarget())
	if err != nil {
		return nil
	}
	p := httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			// Preserve the Host so xpra's virtual-host matching (if any)
			// doesn't get confused by the Caw upstream Host.
			r.Out.Host = r.In.Host
		},
		// The xpra WS server sometimes returns 400 for a probe request
		// during WS upgrade; surface the error so the iframe shows a
		// useful message instead of a blank page.
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, "xpra upstream unavailable: "+err.Error(), http.StatusBadGateway)
		},
	}
	return &p
}

// stripDesktopPrefix removes the /desktop/{id} prefix from the request path
// so the proxy forwards the bare path to xpra. e.g.
//   /desktop/abc/index.html -> /index.html
//   /desktop/abc/ws         -> /ws
// It returns the stripped path and the {id} segment.
func stripDesktopPrefix(path string) (id, rest string) {
	// path looks like /desktop/{id}/...
	trimmed := strings.TrimPrefix(path, "/desktop/")
	if trimmed == path {
		return "", ""
	}
	slash := strings.IndexByte(trimmed, '/')
	if slash < 0 {
		return trimmed, "/"
	}
	return trimmed[:slash], trimmed[slash:]
}

// portFromSession returns the session's TCP port as a string for query
// building in the frontend-facing handlers.
func portFromSession(sess *Session) string {
	return strconv.Itoa(sess.Port)
}