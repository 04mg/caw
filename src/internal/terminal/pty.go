package terminal

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/aymanbagabas/go-pty"
)

type Pty struct {
	ptmx pty.Pty
	cmd  *pty.Cmd
}

func (p *Pty) Close() error {
	return p.ptmx.Close()
}

// TtyName returns the path of the PTY's slave device (e.g. "/dev/pts/3") as
// seen by the child process, or "" when unavailable (e.g. Windows ConPTY).
func (p *Pty) TtyName() string {
	if p == nil || p.ptmx == nil {
		return ""
	}
	return p.ptmx.Name()
}

func (p *Pty) Kill() error {
	if p.cmd != nil && p.cmd.Process != nil {
		return p.cmd.Process.Kill()
	}
	return nil
}

func startPty(cwd string, cmdArgs []string, extraEnv [][]string) (*Pty, error) {
	ptmx, err := pty.New()
	if err != nil {
		return nil, err
	}
	// Ensure the PTY has a valid size before the child starts. Caw's first
	// viewer resize (resizePTY) arrives via WebSocket only after the process
	// is already running. Starting at 0x0 causes some TUIs (notably fx) to
	// fail with "UnableToReadTerminalSize" and exit immediately as
	// interrupted. Seed with a sensible default; the real viewer size takes
	// over moments later via SIGWINCH.
	_ = ptmx.Resize(80, 24)

	var c *pty.Cmd
	if len(cmdArgs) > 0 {
		name := resolveAgentBinary(cmdArgs[0])
		c = ptmx.Command(name, cmdArgs[1:]...)
	} else {
		shell := getShell()
		c = ptmx.Command(shell)
	}
	c.Dir = cwd
	env := sanitizeChildEnv(os.Environ())
	env = append(env, "TERM=xterm-256color")
	for _, kv := range extraEnv {
		if len(kv) != 2 || kv[0] == "" {
			continue
		}
		env = append(env, kv[0]+"="+kv[1])
	}
	c.Env = env

	if err := c.Start(); err != nil {
		ptmx.Close()
		return nil, err
	}

	return &Pty{ptmx: ptmx, cmd: c}, nil
}

func getShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		if path, err := exec.LookPath(s); err == nil {
			return path
		}
		return s
	}
	if runtime.GOOS == "windows" {
		if p := os.Getenv("ComSpec"); p != "" {
			if path, err := exec.LookPath(p); err == nil {
				return path
			}
			return p
		}
		if path, err := exec.LookPath("cmd.exe"); err == nil {
			return path
		}
		return "cmd.exe"
	}
	if path, err := exec.LookPath("/bin/bash"); err == nil {
		return path
	}
	return "/bin/bash"
}

func resolveAgentBinary(name string) string {
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	// Fallback for user-local installs (e.g. fx → ~/.local/bin/fx) when
	// the caw service PATH doesn't include ~/.local/bin.
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if p := filepath.Join(home, ".local", "bin", name); isExecutable(p) {
			return p
		}
	}
	if p := filepath.Join("/root/.local/bin", name); isExecutable(p) {
		return p
	}
	return name
}

func isExecutable(p string) bool {
	fi, err := os.Stat(p)
	if err != nil {
		return false
	}
	return !fi.IsDir() && fi.Mode().Perm()&0111 != 0
}

// inheritedAgentSessionVars are environment variables that coding-agent CLIs
// set for their *own* child processes to mark them as nested sessions. When
// Caw itself was started from inside such an agent (e.g. launched from a
// Claude Code terminal), these leak into every PTY Caw spawns. Claude Code
// then believes it is a nested child session and turns transcript saving off
// ("Transcript saving is off — inherited CLAUDE_CODE_CHILD_SESSION marker"),
// which silently breaks Caw's transcript-based status tracking and resume.
// They describe Caw's parent process, never the agent Caw is launching, so
// they are dropped. Auth/config variables (CLAUDE_CODE_OAUTH_TOKEN,
// CLAUDE_CODE_USE_BEDROCK, ...) are left untouched.
var inheritedAgentSessionVars = map[string]bool{
	"CLAUDECODE":                   true,
	"CLAUDE_CODE_ENTRYPOINT":       true,
	"CLAUDE_CODE_CHILD_SESSION":    true,
	"CLAUDE_CODE_SESSION_ID":       true,
	"CLAUDE_CODE_SESSION_ATTENDED": true,
	"CLAUDE_CODE_MESSAGING_SOCKET": true,
	"CLAUDE_CODE_MESSAGING_TOKEN":  true,
	"CLAUDE_CODE_EXECPATH":         true,
	"CLAUDE_PID":                   true,
	"CLAUDE_EFFORT":                true,
	"CLAUDE_CODE_SSE_PORT":         true,
}

// sanitizeChildEnv returns env without inheritedAgentSessionVars.
func sanitizeChildEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if inheritedAgentSessionVars[k] {
			continue
		}
		out = append(out, kv)
	}
	return out
}
