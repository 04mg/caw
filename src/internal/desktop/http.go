package desktop

import (
	"net/http"
	"time"
)

// httpClient is a short-timeout client for xpra health checks.
var httpClient = &http.Client{Timeout: 30 * time.Second}
