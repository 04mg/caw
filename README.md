<!-- Header -->
  <p align="center">
    <img src="assets/caw/logo.svg" alt="Caw" width="100" />
  </p>

  <h1 align="center">Caw</h1>

  <p align="center">
    <img src="https://img.shields.io/github/v/release/04mg/caw?style=flat&label=release" alt="Latest release" />
    <a href="https://github.com/04mg/caw/stargazers"><img src="https://img.shields.io/github/stars/04mg/caw?style=flat&logo=github" alt="GitHub stars" /></a>
    <img src="https://img.shields.io/github/license/04mg/caw?style=flat" alt="License" />
  </p>

  <p align="center">
    Web terminal multiplexer for AI agents.
  </p>

  <p align="center">
    <img src="assets/caw/banner.png" alt="Caw banner" width="100%" />
  </p>

## Features

<table>
<tr>
<td width="50%" valign="middle">

### Run Agents in the Browser

Launch Claude Code, Codex CLI, GitHub Copilot, OpenCode, Antigravity, Pi, Oh My Pi, or Hermes side-by-side — Caw auto-detects which agents are installed and spins up a terminal pane for each.

</td>
<td width="50%">
  <picture><source srcset="assets/feature-wall/multi-agent.gif" type="image/gif"><img src="assets/feature-wall/multi-agent.jpg" alt="Caw running multiple AI coding agents in browser terminal panes" width="100%" /></picture>
</td>
</tr>
<tr>
<td width="50%" valign="middle">

### Agent Control Center

Stop babysitting terminals. Caw watches each agent's transcript and shows its real-time state — Working, Needs Input, or Idle — on a live Kanban board.

</td>
<td width="50%">
  <picture><source srcset="assets/feature-wall/status-board.gif" type="image/gif"><img src="assets/feature-wall/status-board.jpg" alt="Caw Agent Control Center showing agent statuses: Idle, Working, Needs Input" width="100%" /></picture>
</td>
</tr>
<tr>
<td width="50%" valign="middle">

### Parallel Git Worktrees

Each agent works in its own isolated git worktree and branch, created and cleaned up automatically — fan prompts across agents in parallel with zero file clashes.

</td>
<td width="50%">
  <picture><source srcset="assets/feature-wall/parallel-worktrees.gif" type="image/gif"><img src="assets/feature-wall/parallel-worktrees.jpg" alt="Caw running agents in isolated git worktrees" width="100%" /></picture>
</td>
</tr>
<tr>
<td width="50%" valign="middle">

### Push Notifications

Walk away and stay in the loop. Get a push notification when an agent needs your input or finishes its task — on your desktop browser or on your phone.

</td>
<td width="50%">
  <picture><source srcset="assets/feature-wall/push-notifications.gif" type="image/gif"><img src="assets/feature-wall/push-notifications.jpg" alt="Caw push notification on a phone and browser when an agent needs input or finishes" width="100%" /></picture>
</td>
</tr>
<tr>
<td width="50%" valign="middle">

### Usage Quota Monitoring

Keep an eye on your spend. Caw shows live usage limits and reset times for Claude, Codex, Copilot, Antigravity, OpenCode, Ollama, and OpenRouter.

</td>
<td width="50%">
  <picture><source srcset="assets/feature-wall/quota-monitoring.gif" type="image/gif"><img src="assets/feature-wall/quota-monitoring.jpg" alt="Caw usage quota monitoring for multiple providers" width="100%" /></picture>
</td>
</tr>
</table>

## Supported agents

Caw launches any CLI coding agent that runs in a terminal. These are detected on your machine and supported out of the box:

<p align="center">
  <img src="assets/icons/claude.svg" width="18" alt="Claude Code" /> <b>Claude Code</b> &nbsp;&nbsp;
  <img src="assets/icons/codex.svg" width="18" alt="Codex CLI" /> <b>Codex CLI</b> &nbsp;&nbsp;
  <img src="assets/icons/copilot.svg" width="18" alt="GitHub Copilot" /> <b>GitHub Copilot</b> &nbsp;&nbsp;
  <img src="assets/icons/antigravity.svg" width="18" alt="Antigravity" /> <b>Antigravity</b> &nbsp;&nbsp;
  <img src="assets/icons/opencode.svg" width="18" alt="OpenCode" /> <b>OpenCode</b> &nbsp;&nbsp;
  <img src="assets/icons/pi.svg" width="18" alt="Pi" /> <b>Pi</b> &nbsp;&nbsp;
  <img src="assets/icons/omp.svg" width="18" alt="Oh My Pi" /> <b>Oh My Pi</b> &nbsp;&nbsp;
  <img src="assets/icons/hermes.svg" width="18" alt="Hermes" /> <b>Hermes</b>
</p>

### Usage quota providers

Caw can show live usage limits and reset times for the following providers:

<p align="center">
  <img src="assets/icons/claude.svg" width="18" alt="Claude" /> <b>Claude</b> &nbsp;&nbsp;
  <img src="assets/icons/codex.svg" width="18" alt="Codex" /> <b>Codex</b> &nbsp;&nbsp;
  <img src="assets/icons/copilot.svg" width="18" alt="GitHub Copilot" /> <b>GitHub Copilot</b> &nbsp;&nbsp;
  <img src="assets/icons/antigravity.svg" width="18" alt="Antigravity" /> <b>Antigravity</b> &nbsp;&nbsp;
  <img src="assets/icons/opencode.svg" width="18" alt="OpenCode" /> <b>OpenCode</b> &nbsp;&nbsp;
  <img src="assets/icons/ollama.svg" width="18" alt="Ollama" /> <b>Ollama</b> &nbsp;&nbsp;
  <img src="assets/icons/openrouter.svg" width="18" alt="OpenRouter" /> <b>OpenRouter</b>
</p>

## Supported commands

```
caw              Start the server (default)
caw update       Update caw to the latest release
caw version      Print the current version
caw help         Show available commands
```

Run `caw help` for the full list of flags, commands, and environment variables.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for commit and branch naming conventions.
