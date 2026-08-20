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
// When the agent's own internal session id was persisted (via
// RecordExternalSession, set by the status watcher once it binds to the
// agent's session row/transcript), we resume that EXACT session. This matters
// when multiple agent panes share the same cwd: a bare --continue / --last
// would make every pane reopen the most recent session, colliding with each
// other. Agents whose CLIs don't accept an explicit session id (claude,
// copilot, antigravity, pi) fall back to --continue.
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
	// Prefer the agent's own session id when available — it pinpoints the
	// exact conversation to resume instead of "the most recent one in this
	// cwd", which is wrong when several panes run the same agent.
	externalID := store.GetExternalSessionID(leafID)
	switch base {
	case "opencode":
		// opencode -s <session> | --continue
		if externalID != "" {
			// Append "-s <id>" (guarding against a pre-existing -s) so the
			// pane resumes its exact prior session instead of the latest one.
			cmd = appendNonFlag(cmd, "-s")
			return append(cmd, externalID)
		}
		return appendNonFlag(cmd, "--continue")
	case "claude":
		// claude --dangerously-skip-permissions -> add --continue
		return appendNonFlag(cmd, "--continue")
	case "codex":
		// codex resume <session-id> | codex resume --last
		if externalID != "" {
			return injectCodexResume(cmd, externalID)
		}
		return injectCodexResume(cmd, "")
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
	case "omp":
		// Oh My Pi (omp) --continue / -c continues the previous session.
		return appendNonFlag(cmd, "--continue")
	case "hermes":
		// Hermes: --resume <session-id> reattaches to an exact session;
		// --continue / -c resumes the most recent one.
		if externalID != "" {
			cmd = appendNonFlag(cmd, "--resume")
			return append(cmd, externalID)
		}
		return appendNonFlag(cmd, "--continue")
	case "command-code", "commandcode":
		// Command Code: --continue / -c resumes the most recent session in
		// the current directory. The CLI does not expose an exact-session-id
		// resume flag for arbitrary external ids, so fall back to it.
		return appendNonFlag(cmd, "--continue")
	case "fx":
		// Fx: --resume <session-id> reattaches to an exact session;
		// --continue / -c resumes the most recent one in the current workspace.
		if externalID != "" {
			cmd = appendNonFlag(cmd, "--resume")
			return append(cmd, externalID)
		}
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
	case "claude", "codex", "copilot", "agy", "opencode", "pi", "omp", "hermes", "command-code", "commandcode", "fx":
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

// injectCodexResume turns `codex <opts...>` into `codex resume <session-id>
// <opts...>` when sessionID is non-empty, or `codex resume --last <opts...>`
// when it is empty. It preserves any existing flags (sandbox, approval
// policy, etc.) by inserting the subcommand and the id/--last immediately
// after the binary name. If the command is already in resume form, only the
// id/--last argument is added (id is never injected twice).
func injectCodexResume(cmd []string, sessionID string) []string {
	if len(cmd) == 0 {
		return cmd
	}
	wantLast := sessionID == ""
	for _, a := range cmd[1:] {
		if a == "resume" {
			// already in resume form
			if wantLast {
				return appendNonFlag(cmd, "--last")
			}
			return appendNonFlag(cmd, sessionID)
		}
	}
	out := make([]string, 0, len(cmd)+3)
	if wantLast {
		out = append(out, cmd[0], "resume", "--last")
	} else {
		out = append(out, cmd[0], "resume", sessionID)
	}
	out = append(out, cmd[1:]...)
	return out
}