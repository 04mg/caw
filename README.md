<!-- Header -->
  <p align="center">
    <img src="LOGO.svg" alt="Caw" width="200">
  </p>

  <h1 align="center">Caw</h1>

  <p align="center">
    <i>Cloud agentic workspace</i>
  </p>
<!-- /Header -->

## Features

<table>
<tr>
<td width="50%" valign="middle">

### Run Agents in the Browser

Launch Claude Code, Codex CLI, GitHub Copilot, OpenCode, Antigravity, or Pi side-by-side — Caw auto-detects which agents are installed and spins up a terminal pane for each.

</td>
<td width="50%">
  <!-- TODO: generate feature GIF → docs/assets/feature-wall/multi-agent.gif (jpg is the fallback) -->
  <picture><source srcset="docs/assets/feature-wall/multi-agent.gif" type="image/gif"><img src="docs/assets/feature-wall/multi-agent.jpg" alt="Caw running multiple AI coding agents in browser terminal panes" width="100%" /></picture>
</td>
</tr>
<tr>
<td width="50%" valign="middle">

### Live Agent Status Board

Stop babysitting terminals. Caw watches each agent's transcript and shows its real-time state — Working, Needs Input, or Idle — on a live Kanban board.

</td>
<td width="50%">
  <!-- TODO: generate feature GIF → docs/assets/feature-wall/status-board.gif (jpg is the fallback) -->
  <picture><source srcset="docs/assets/feature-wall/status-board.gif" type="image/gif"><img src="docs/assets/feature-wall/status-board.jpg" alt="Caw Kanban board showing agent statuses: Idle, Working, Needs Input" width="100%" /></picture>
</td>
</tr>
<tr>
<td width="50%" valign="middle">

### Parallel Git Worktrees

Each agent works in its own isolated git worktree and branch, created and cleaned up automatically — fan prompts across agents in parallel with zero file clashes.

</td>
<td width="50%">
  <!-- TODO: generate feature GIF → docs/assets/feature-wall/parallel-worktrees.gif (jpg is the fallback) -->
  <picture><source srcset="docs/assets/feature-wall/parallel-worktrees.gif" type="image/gif"><img src="docs/assets/feature-wall/parallel-worktrees.jpg" alt="Caw running agents in isolated git worktrees" width="100%" /></picture>
</td>
</tr>
<tr>
<td width="50%" valign="middle">

### Push Notifications

Walk away and stay in the loop. Get a push notification when an agent needs your input or finishes its task — on your desktop browser or on your phone.

</td>
<td width="50%">
  <!-- TODO: generate feature GIF → docs/assets/feature-wall/push-notifications.gif (jpg is the fallback) -->
  <picture><source srcset="docs/assets/feature-wall/push-notifications.gif" type="image/gif"><img src="docs/assets/feature-wall/push-notifications.jpg" alt="Caw push notification on a phone and browser when an agent needs input or finishes" width="100%" /></picture>
</td>
</tr>
</table>

**Also in the box:**

- **Cloud multi-client sync** — your workspace layout and live terminals stay in sync across every browser you open Caw in.
- **Usage quota monitoring** — live limits and reset times for Claude, Codex, Copilot, Antigravity, OpenCode, Ollama, and OpenRouter.
- **File explorer with undo/redo** — browse, edit, and manage files with a full trash-based history (`Ctrl+Z` / `Ctrl+Y`).
- **Terminal grid & command palette** — split terminal panes to any layout, a fuzzy command palette, and a mobile-friendly control center.

## Build

### Prerequisites
- [Go](https://go.dev/dl/) 1.21+
- [Node.js](https://nodejs.org/) 20+
- npm
- [Make](https://www.gnu.org/software/make/)

### Build
```sh
make build
```

### Lint
```sh
make lint
```

## Contributing

See [CONVENTIONS.md](CONVENTIONS.md) for commit and branch naming conventions.
