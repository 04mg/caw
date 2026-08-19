package desktop

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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

// provisionMu guards the one-shot xpra provisioning so concurrent desktop
// session creations don't race the apt-get install.
var provisionMu sync.Mutex

// EnsureInstalled makes xpra available on the host. It is a no-op if xpra
// is already on PATH. When xpra is missing it auto-provisions from the
// xpra.org apt repository (the only source that ships current xpra builds
// for Debian/Ubuntu); on other distros the user must install xpra manually
// and this returns an error describing the requirement.
//
// Provisioning is intentionally minimal: add the xpra.org signed-by key,
// add a distro-matching sources entry, apt-get update, and install the
// xpra + xpra-x11 packages with --no-install-recommends to keep the
// footprint small (no GTK client, no audio, no printer). Idempotent.
func EnsureInstalled() error {
	if xpraAvailable() {
		return nil
	}
	provisionMu.Lock()
	defer provisionMu.Unlock()
	if xpraAvailable() {
		return nil
	}
	if err := provisionApt(); err != nil {
		return fmt.Errorf("xpra auto-install failed: %w\n"+
			"install xpra manually: see https://github.com/Xpra-org/xpra", err)
	}
	if !xpraAvailable() {
		return fmt.Errorf("xpra installed but not found on PATH")
	}
	return nil
}

// provisionApt installs xpra from the xpra.org apt repository on a
// Debian/Ubuntu host. Returns an error on any non-debian-like distro or on
// apt failure.
func provisionApt() error {
	if _, err := exec.LookPath("apt-get"); err != nil {
		return fmt.Errorf("apt-get not found; only Debian/Ubuntu auto-install is supported")
	}
	codename, err := osReleaseCodename()
	if err != nil {
		return err
	}
	repoEntry, ok := xpraRepoForCodename(codename)
	if !ok {
		return fmt.Errorf("no xpra.org repo for distro codename %q", codename)
	}

	keyringPath := "/etc/apt/keyrings/xpra.asc"
	if err := os.MkdirAll(filepath.Dir(keyringPath), 0o755); err != nil {
		return err
	}
	if err := downloadFile("https://xpra.org/xpra.asc", keyringPath); err != nil {
		return fmt.Errorf("download xpra GPG key: %w", err)
	}

	sourcesPath := "/etc/apt/sources.list.d/xpra.list"
	if err := os.WriteFile(sourcesPath, []byte(repoEntry+"\n"), 0o644); err != nil {
		return err
	}

	if out, err := exec.Command("apt-get", "update", "-o", "Dir::Etc::sourceparts=-", "-o", "APT::Get::List-Cleanup=0").CombinedOutput(); err != nil {
		return fmt.Errorf("apt-get update: %w: %s", err, out)
	}

	// Install the meta-package xpra plus xpra-x11 (seamless X11 backend)
	// with --no-install-recommends to skip the GTK client, audio, printers
	// and other recommends we don't need for headless app forwarding.
	args := []string{"install", "-y", "--no-install-recommends", "xpra", "xpra-x11"}
	if out, err := exec.Command("apt-get", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("apt-get install xpra: %w: %s", err, out)
	}
	return nil
}

// xpraRepoForCodename maps a Debian/Ubuntu codename to the xpra.org apt
// sources.list entry. xpra.org publishes per-codename trees; this keeps
// the auto-install pinning to the host's actual distro.
func xpraRepoForCodename(codename string) (string, bool) {
	// xpra.org supported codenames (see https://xpra.org/dists/).
	known := map[string]bool{
		"bookworm": true, "bullseye": true, "buster": true,
		"trixie": true, "forky": true,
		"focal": true, "jammy": true, "noble": true,
		"kinetic": true, "lunar": true, "bionic": true,
	}
	if !known[codename] {
		// Fall back to a best-effort generic entry; the codename is
		// passed through so a newly added distro still works if xpra.org
		// publishes a tree for it.
		return fmt.Sprintf("deb [signed-by=/etc/apt/keyrings/xpra.asc] https://xpra.org/ %s main", codename), true
	}
	return fmt.Sprintf("deb [signed-by=/etc/apt/keyrings/xpra.asc] https://xpra.org/ %s main", codename), true
}

// osReleaseCodename reads /etc/os-release and returns the VERSION_CODENAME
// (e.g. "trixie", "bookworm", "jammy"). Used to pick the matching xpra.org
// apt repo branch.
func osReleaseCodename() (string, error) {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "", fmt.Errorf("read /etc/os-release: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VERSION_CODENAME=") {
			val := strings.TrimPrefix(line, "VERSION_CODENAME=")
			val = strings.Trim(val, "\"'")
			if val != "" {
				return val, nil
			}
		}
	}
	return "", fmt.Errorf("VERSION_CODENAME not found in /etc/os-release")
}

// downloadFile fetches a URL and writes it to dst. Used for the xpra GPG
// keyring. Uses the standard library net/http client with a short timeout.
func downloadFile(url, dst string) error {
	resp, err := httpClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := copyFile(f, resp.Body); err != nil {
		return err
	}
	return nil
}