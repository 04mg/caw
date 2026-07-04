package main

import (
	"os"
	"os/exec"
	"runtime"

	"github.com/aymanbagabas/go-pty"
)

type ptySession struct {
	ptmx pty.Pty
	cmd  *pty.Cmd
}

func startPty(cwd string) (*ptySession, error) {
	ptmx, err := pty.New()
	if err != nil {
		return nil, err
	}

	shell := getShell()
	c := ptmx.Command(shell)
	c.Dir = cwd
	c.Env = append(os.Environ(), "TERM=xterm-256color")

	if err := c.Start(); err != nil {
		ptmx.Close()
		return nil, err
	}

	return &ptySession{ptmx: ptmx, cmd: c}, nil
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