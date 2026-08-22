package desktop

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

// newProxy builds a reverse proxy that forwards to the xpra WS server for
// the given session. It handles WebSocket upgrades (the xpra display
// stream) by delegating to httputil.ReverseProxy, which supports WS when
// the request carries the Upgrade header.
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
		// during WS upgrade; surface the error so the client sees a useful
		// message instead of a blank page.
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, "xpra upstream unavailable: "+err.Error(), http.StatusBadGateway)
		},
	}
	return &p
}