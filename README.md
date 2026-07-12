<!-- Header -->
  <p align="center">
    <img src="assets/logo.png" alt="Caw" width="110" />
  </p>

  <h1 align="center">Caw</h1>

  <p align="center">
    <img src="https://img.shields.io/github/v/release/04mg/caw?style=flat&label=release" alt="Latest release" />
    <a href="https://github.com/04mg/caw/stargazers"><img src="https://img.shields.io/github/stars/04mg/caw?style=flat&logo=github" alt="GitHub stars" /></a>
    <img src="https://img.shields.io/github/downloads/04mg/caw/total?style=flat" alt="Total downloads" />
    <img src="https://img.shields.io/github/license/04mg/caw?style=flat" alt="License" />
  </p>

  <p align="center">
    A single self-hostable binary that runs AI coding agents inside browser-based terminal panes, with live status tracking, git isolation and push notifications.
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
  <!-- TODO: generate feature GIF → assets/feature-wall/multi-agent.gif (jpg is the fallback) -->
  <picture><source srcset="assets/feature-wall/multi-agent.gif" type="image/gif"><img src="assets/feature-wall/multi-agent.jpg" alt="Caw running multiple AI coding agents in browser terminal panes" width="100%" /></picture>
</td>
</tr>
<tr>
<td width="50%" valign="middle">

### Agent Control Center

Stop babysitting terminals. Caw watches each agent's transcript and shows its real-time state — Working, Needs Input, or Idle — on a live Kanban board.

</td>
<td width="50%">
  <!-- TODO: generate feature GIF → assets/feature-wall/status-board.gif (jpg is the fallback) -->
  <picture><source srcset="assets/feature-wall/status-board.gif" type="image/gif"><img src="assets/feature-wall/status-board.jpg" alt="Caw Agent Control Center showing agent statuses: Idle, Working, Needs Input" width="100%" /></picture>
</td>
</tr>
<tr>
<td width="50%" valign="middle">

### Parallel Git Worktrees

Each agent works in its own isolated git worktree and branch, created and cleaned up automatically — fan prompts across agents in parallel with zero file clashes.

</td>
<td width="50%">
  <!-- TODO: generate feature GIF → assets/feature-wall/parallel-worktrees.gif (jpg is the fallback) -->
  <picture><source srcset="assets/feature-wall/parallel-worktrees.gif" type="image/gif"><img src="assets/feature-wall/parallel-worktrees.jpg" alt="Caw running agents in isolated git worktrees" width="100%" /></picture>
</td>
</tr>
<tr>
<td width="50%" valign="middle">

### Push Notifications

Walk away and stay in the loop. Get a push notification when an agent needs your input or finishes its task — on your desktop browser or on your phone.

</td>
<td width="50%">
  <!-- TODO: generate feature GIF → assets/feature-wall/push-notifications.gif (jpg is the fallback) -->
  <picture><source srcset="assets/feature-wall/push-notifications.gif" type="image/gif"><img src="assets/feature-wall/push-notifications.jpg" alt="Caw push notification on a phone and browser when an agent needs input or finishes" width="100%" /></picture>
</td>
</tr>
<tr>
<td width="50%" valign="middle">

### Usage Quota Monitoring

Keep an eye on your spend. Caw shows live usage limits and reset times for Claude, Codex, Copilot, Antigravity, OpenCode, Ollama, and OpenRouter.

</td>
<td width="50%">
  <!-- TODO: generate feature GIF → assets/feature-wall/quota-monitoring.gif (jpg is the fallback) -->
  <picture><source srcset="assets/feature-wall/quota-monitoring.gif" type="image/gif"><img src="assets/feature-wall/quota-monitoring.jpg" alt="Caw usage quota monitoring for multiple providers" width="100%" /></picture>
</td>
</tr>
</table>

## Supported agents

Caw launches any CLI coding agent that runs in a terminal. These are detected on your machine and supported out of the box:

<p align="center">
  <img src="assets/claude.svg" width="18" alt="Claude Code" /> <b>Claude Code</b> &nbsp;&nbsp;
  <img src="assets/codex.svg" width="18" alt="Codex CLI" /> <b>Codex CLI</b> &nbsp;&nbsp;
  <img src="assets/copilot.svg" width="18" alt="GitHub Copilot" /> <b>GitHub Copilot</b> &nbsp;&nbsp;
  <img src="assets/antigravity.svg" width="18" alt="Antigravity" /> <b>Antigravity</b> &nbsp;&nbsp;
  <img src="assets/opencode.svg" width="18" alt="OpenCode" /> <b>OpenCode</b> &nbsp;&nbsp;
  <img src="assets/pi.svg" width="18" alt="Pi" /> <b>Pi</b>
</p>

### Usage quota providers

Caw can show live usage limits and reset times for the following providers:

<p align="center">
  <img src="assets/claude.svg" width="18" alt="Claude" /> <b>Claude</b> &nbsp;&nbsp;
  <img src="assets/codex.svg" width="18" alt="Codex" /> <b>Codex</b> &nbsp;&nbsp;
  <img src="assets/copilot.svg" width="18" alt="GitHub Copilot" /> <b>GitHub Copilot</b> &nbsp;&nbsp;
  <img src="assets/antigravity.svg" width="18" alt="Antigravity" /> <b>Antigravity</b> &nbsp;&nbsp;
  <img src="assets/opencode.svg" width="18" alt="OpenCode" /> <b>OpenCode</b> &nbsp;&nbsp;
  <img src="assets/ollama.svg" width="18" alt="Ollama" /> <b>Ollama</b> &nbsp;&nbsp;
  <img src="assets/openrouter.svg" width="18" alt="OpenRouter" /> <b>OpenRouter</b>
</p>

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for commit and branch naming conventions.
