package desktop

import (
	"fmt"
	"net"
)

// freeTCPPort asks the kernel for an available TCP port on 127.0.0.1. The
// listener is closed immediately so xpra can rebind it; there is an
// inherent TOCTOU race but xpra will fail loudly if it loses the race.
func freeTCPPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("allocate tcp port: %w", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port, nil
}
