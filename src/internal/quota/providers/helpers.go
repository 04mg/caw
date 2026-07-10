package providers

import "os/exec"

// execLookPath is a small wrapper to centralize PATH probing for provider
// installation checks. It returns the path of the binary if found.
func execLookPath(file string) (string, error) {
	return exec.LookPath(file)
}