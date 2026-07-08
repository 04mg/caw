package terminal

import (
	"path/filepath"
	"strings"

	"github.com/04mg/caw/internal/state"
)

// resumeCmdForAgent returns the agent launch command augmented with a
// resume/continue flag so that, on reopen, the agent reconnects to its
// previous internal session instead of starting a fresh one.
//
// The decision to resume is driven by the persisted agent_sessions table:
// if a previous Caw process recorded that this leaf already hosted an agent
// PTY, we treat the current /api/terminal/create as a reopen and mutate the
// command accordingly. If the leaf has no prior record, the command is
// returned unchanged so the first launch starts a brand-new agent session.
//
// agentID is derived from cmd[0] basename (see agent.status.handleSessionStart)
// and is used to pick the right resume flag syntax per agent. Unknown agents
// and plain shells fall through and return the command untouched.
func resumeCmdForAgent(store *state.Store, leafID string, cmd []string) []string {
	if len(cmd) == 0 || store == nil {
		return cmd
	}
	if !store.WasAgentStarted(leafID) {
		return cmd
	}

	base := agentBaseName(cmd[0])
	switch base {
	case "opencode":
		// opencode -s <session> | --continue
		// Prefer --continue (reuses last session in this worktree) which is
		// the simplest and most reliable across versions; we don't have the
		// external session id persisted, so --continue matches the cwd.
		return appendNonFlag(cmd, "--continue")
	case "claude":
		// claude --dangerously-skip-permissions -> add --continue
		return appendNonFlag(cmd, "--continue")
	case "codex":
		// codex uses a subcommand: `codex resume --last`. When the original
		// command was `codex --sandbox workspace-write --ask-for-approval never`,
		// the resume form is `codex resume --last --sandbox workspace-write --ask-for-approval never`.
		// Insert "resume --last" right after the binary name and keep the rest.
		return injectCodexResume(cmd)
	case "copilot":
		// copilot --allow-all-tools --allow-all-paths -> add --continue
		return appendNonFlag(cmd, "--continue")
	case "agy":
		// Antigravity: --continue is the documented short alias for continuing
		// the most recent conversation.
		return appendNonFlag(cmd, "--continue")
	case "pi":
		// pi --continue / -c continues the previous session.
		return appendNonFlag(cmd, "--continue")
	default:
		return cmd
	}
}

// agentBaseName returns the lowercase basename of the agent binary, with
// the .exe suffix stripped on Windows. Mirrors the detection logic in
// agent.status.handleSessionStart so resumeCmdForAgent picks the same agentID
// the status tracker will assign.
func agentBaseName(name string) string {
	b := filepath.Base(name)
	b = strings.TrimSuffix(b, ".exe")
	return strings.ToLower(b)
}

// isKnownAgent reports whether a binary basename corresponds to one of the
// agents Caw knows how to launch. Used to gate persistence so plain shells
// (bash, cmd, zsh, custom shells) don't get marked as agent sessions and
// don't receive a spurious --continue flag on reopen.
func isKnownAgent(b string) bool {
	switch b {
	case "claude", "codex", "copilot", "agy", "opencode", "pi":
		return true
	}
	return false
}

// appendNonFlag appends a flag to cmd only if it is not already present, so
// repeated resumes don't stack duplicate flags.
func appendNonFlag(cmd []string, flag string) []string {
	for _, a := range cmd {
		if a == flag {
			return cmd
		}
	}
	return append(cmd, flag)
}

// injectCodexResume turns `codex <opts...>` into `codex resume --last <opts...>`.
// It preserves any existing flags (sandbox, approval policy, etc.) by
// inserting the subcommand and --last immediately after the binary name.
func injectCodexResume(cmd []string) []string {
	if len(cmd) == 0 {
		return cmd
	}
	for _, a := range cmd[1:] {
		if a == "resume" {
			// already in resume form
			return appendNonFlag(cmd, "--last")
		}
	}
	out := make([]string, 0, len(cmd)+2)
	out = append(out, cmd[0], "resume", "--last")
	out = append(out, cmd[1:]...)
	return out
}