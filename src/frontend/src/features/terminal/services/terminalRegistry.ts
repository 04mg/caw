import { useEffect, useState } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { RingBuffer } from './ringBuffer'

export interface TerminalInstance {
  leafId: string
  term: Terminal
  fit: FitAddon
  ws: WebSocket | null
  /** Backend session ID used to re-connect the WebSocket after a drop. */
  backendId: string
  /** Buffer of output received before the term was ready, replayed on open. */
  buffer: RingBuffer<string>
  exited: boolean
  /** @internal set during buffer replay to queue incoming ws messages */
  _replaying: boolean
  /** @internal messages queued during replay */
  _pendingQueue: string[]
  /** @internal output chunks accumulated for batched rendering per frame */
  _pendingOutput: string[]
  /** @internal rAF id for the scheduled output flush, 0 when idle */
  _rafId: number
  /**
   * @internal True while the xterm.js instance is detached from the DOM
   * (e.g. the user switched to another tab). The WebSocket is kept alive so
   * that re-attaching replays the local ring buffer instead of reconnecting
   * and reloading scrollback from the backend. While detached, incoming WS
   * output is accumulated into the buffer only — never written to the
   * disposed terminal.
   */
  _detached: boolean
  /**
   * @internal Last-set state of the DEC private modes that affect input
   * routing (mouse tracking, SGR mouse, bracketed paste, focus reporting).
   * Tracked from the live WS stream so a re-attached xterm.js instance can
   * re-enter the same mode the running TUI expects, even after the 10k
   * buffer cap evicted the original mode-set sequence.
   */
  _modes: Map<number, boolean>
  /**
   * @internal Cell-based horizontal padding (cols) the server asked this
   * viewer to apply so the terminal grid is centered within a panel that
   * is wider than the PTY. The FitAddon still reports the full panel
   * dimensions; the padding is applied as CSS margins on the xterm.js
   * element after fit runs, so only the inner minCols×minRows grid is
   * rendered.
   */
  _padCols: number
  /**
   * @internal Cell-based vertical padding (rows) for centering. See _padCols.
   */
  _padRows: number
  /**
   * @internal Set to true while the user is actively scrolling (touch drag
   * or momentum). When true, safeScrollToBottom is suppressed so incoming
   * output does not yank the view back to the bottom.
   */
  userScrolling: boolean
  /**
   * @internal True when this pane is the workspace's active pane (the user
   * is focused on it). Maintained by TerminalPanel via the `focus`/`blur`
   * WS message so the backend's agent status heuristics know which terminal
   * the user is driving. On WS reconnect we re-send this flag so the
   * backend doesn't lose focus state across socket drops.
   */
  _focused: boolean
  /**
   * @internal True after releaseTerminal intentionally drops this client's
   * hold on a terminal (mobile switched to another terminal). Prevents the
   * ws.onclose auto-reconnect from silently re-attaching and re-pinning
   * the backend PTY to mobile dimensions. A later attachTerminal call
   * re-opens the terminal and reconnects.
   */
  _released: boolean
}

const registry = new Map<string, TerminalInstance>()
const subscribers = new Set<() => void>()
let onTerminalExit: ((leafId: string) => void) | null = null

export function setOnTerminalExit(cb: ((leafId: string) => void) | null) {
  onTerminalExit = cb
}

function notify() {
  for (const s of subscribers) s()
}

async function ensureBackend(leafId: string, cwd: string, cmd?: string[], env?: [string, string][]): Promise<string> {
  if (!cmd || cmd.length === 0) {
    const customShell = localStorage.getItem('caw:defaultShell')
    if (customShell) cmd = [customShell]
  }
  const body: Record<string, unknown> = { id: leafId, cwd: cwd || '', cmd }
  if (env && env.length > 0) body.env = env
  const res = await fetch('/api/terminals', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  const { id } = (await res.json())?.data ?? {}
  return id
}

const darkTerminalTheme = {
  background: '#0a0a0a',
  foreground: '#f0f0f0',
  cursor: '#f0f0f0',
  selectionBackground: '#264f78',
  black: '#2e2e2e',
  red: '#eb4129',
  green: '#abe047',
  yellow: '#f6c744',
  blue: '#47a0f0',
  magenta: '#7b5cb0',
  cyan: '#64dbed',
  white: '#e5e9f0',
  brightBlack: '#565656',
  brightRed: '#ec5357',
  brightGreen: '#c0e17d',
  brightYellow: '#f9da6a',
  brightBlue: '#6284cf',
  brightMagenta: '#a37bb7',
  brightCyan: '#76d7e8',
  brightWhite: '#f6f9fa',
}

const lightTerminalTheme = {
  background: '#ffffff',
  foreground: '#1a1a1a',
  cursor: '#1a1a1a',
  selectionBackground: '#b0d4f1',
  black: '#2e2e2e',
  red: '#c41a16',
  green: '#1c7c1c',
  yellow: '#8b6f00',
  blue: '#0050a4',
  magenta: '#6c2d82',
  cyan: '#007080',
  white: '#e0e0e0',
  brightBlack: '#565656',
  brightRed: '#d73a33',
  brightGreen: '#2da12d',
  brightYellow: '#b89500',
  brightBlue: '#2464b8',
  brightMagenta: '#8a44a0',
  brightCyan: '#0098a8',
  brightWhite: '#f5f5f5',
}

function getTerminalTheme(): 'dark' | 'light' {
  return (localStorage.getItem('caw:terminalTheme') as 'dark' | 'light') || 'dark'
}

function makeTerminal(): { term: Terminal; fit: FitAddon } {
  const savedSize = parseInt(localStorage.getItem('caw:terminalFontSize') || '13', 10)
  const fontSize = isNaN(savedSize) ? 13 : Math.max(8, Math.min(32, savedSize))
  const theme = getTerminalTheme() === 'light' ? lightTerminalTheme : darkTerminalTheme
  const term = new Terminal({
    cursorBlink: true,
    fontSize,
    fontFamily: "'JetBrainsMono Nerd Font', ui-monospace, SFMono-Regular, 'Cascadia Code', 'Fira Code', monospace",
    scrollback: 10000,
    theme,
  })
  const fit = new FitAddon()
  term.loadAddon(fit)
  // The FitAddon subtracts a default 14px scrollbar width from the available
  // terminal width (overviewRuler?.width || 14). Since we hide the VSCode-style
  // scrollbar via CSS (display: none), that reserved space shows as an
  // unrendered black strip on the right side of the terminal. Patch
  // proposeDimensions to use 0 for the scrollbar width so the terminal fills
  // its container completely.
  const originalProposeDimensions = fit.proposeDimensions.bind(fit)
  fit.proposeDimensions = () => {
    const dims = originalProposeDimensions()
    if (dims) {
      const cellWidth = (term as any)._core?._renderService?.dimensions?.css?.cell?.width
      if (cellWidth) {
        // Recalculate cols without the scrollbar subtraction
        const el = term.element
        if (el && el.parentElement) {
          const parentStyle = window.getComputedStyle(el.parentElement)
          const availableWidth = Math.max(0, parseInt(parentStyle.getPropertyValue('width')))
            - (parseInt(parentStyle.getPropertyValue('padding-right')) + parseInt(parentStyle.getPropertyValue('padding-left')))
          const myStyle = window.getComputedStyle(el)
          const innerWidth = availableWidth
            - (parseInt(myStyle.getPropertyValue('padding-right')) + parseInt(myStyle.getPropertyValue('padding-left')))
          const cols = Math.max(2, Math.floor(innerWidth / cellWidth))
          if (cols > dims.cols) {
            return { ...dims, cols }
          }
        }
      }
    }
  }
  
  // Register OSC 52 handler to capture TUI clipboard writes
  term.parser.registerOscHandler(52, (data) => {
    try {
      const parts = data.split(';')
      let base64Data = ''
      if (parts.length > 1) {
        base64Data = parts[1]
      } else {
        base64Data = parts[0]
      }
      if (base64Data === '?') {
        // Query - ignore
        return true
      }
      if (!base64Data) {
        ;(term as any)._tuiClipboard = ''
        return true
      }
      const binaryString = atob(base64Data.trim())
      const bytes = new Uint8Array(binaryString.length)
      for (let i = 0; i < binaryString.length; i++) {
        bytes[i] = binaryString.charCodeAt(i)
      }
      const decoded = new TextDecoder().decode(bytes)
      ;(term as any)._tuiClipboard = decoded
    } catch (e) {
      console.error('Failed to parse OSC 52 data:', e)
    }
    return true
  })

  return { term, fit }
}

// applyPadding centers the xterm.js grid within its container by setting
// CSS margins sized in pixels. The server sends cell-based padding
// (padCols/padRows) computed from (viewer dimensions − PTY dimensions);
// we multiply by the current cell pixel dimensions to get exact pixel
// margins. The odd cell goes to the right/bottom so the content sits
// slightly left/up, matching typical reading layout.
//
// Margins are applied to the .xterm element itself, not its parent, so
// the FitAddon's proposeDimensions (which reads the parent's CSS width)
// is unaffected — no feedback loop between padding and fitting.
function applyPadding(inst: TerminalInstance) {
  const el = inst.term.element
  if (!el) return
  if (inst._padCols <= 0 && inst._padRows <= 0) {
    el.style.marginLeft = ''
    el.style.marginRight = ''
    el.style.marginTop = ''
    el.style.marginBottom = ''
    return
  }
  const cell = (inst.term as any)._core?._renderService?.dimensions?.css?.cell
  const cellW = cell?.width ?? 0
  const cellH = cell?.height ?? 0
  if (cellW <= 0 || cellH <= 0) return
  const left = Math.floor(inst._padCols / 2) * cellW
  const right = Math.ceil(inst._padCols / 2) * cellW
  const top = Math.floor(inst._padRows / 2) * cellH
  const bottom = Math.ceil(inst._padRows / 2) * cellH
  el.style.marginLeft = `${left}px`
  el.style.marginRight = `${right}px`
  el.style.marginTop = `${top}px`
  el.style.marginBottom = `${bottom}px`
}

export const stickyModifiers = {
  ctrl: false,
  alt: false,
  shift: false,
}

const stickySubscribers = new Set<() => void>()
export function subscribeStickyModifiers(cb: () => void) {
  stickySubscribers.add(cb)
  return () => { stickySubscribers.delete(cb) }
}

function notifySticky() {
  for (const cb of stickySubscribers) cb()
}

export function toggleStickyCtrl() {
  stickyModifiers.ctrl = !stickyModifiers.ctrl
  if (stickyModifiers.ctrl) { stickyModifiers.alt = false; stickyModifiers.shift = false }
  notifySticky()
}

export function toggleStickyAlt() {
  stickyModifiers.alt = !stickyModifiers.alt
  if (stickyModifiers.alt) { stickyModifiers.ctrl = false; stickyModifiers.shift = false }
  notifySticky()
}

export function toggleStickyShift() {
  stickyModifiers.shift = !stickyModifiers.shift
  if (stickyModifiers.shift) { stickyModifiers.ctrl = false; stickyModifiers.alt = false }
  notifySticky()
}

export function resetStickyModifiers() {
  stickyModifiers.ctrl = false
  stickyModifiers.alt = false
  stickyModifiers.shift = false
  notifySticky()
}

export function sendTerminalInput(leafId: string, data: string) {
  const inst = registry.get(leafId)
  if (inst && inst.ws?.readyState === WebSocket.OPEN) {
    inst.ws.send(JSON.stringify({ type: 'input', data }))
  }
}

// reconnectTerminalWs forces an immediate WebSocket reconnection for the
// given terminal. Used by the visibility/focus handler in TerminalPanel so
// that returning from a PWA minimize / tab switch restores input without
// waiting for the 1s auto-reconnect timer.
export function reconnectTerminalWs(leafId: string) {
  const inst = registry.get(leafId)
  if (!inst || inst.exited || !inst.backendId) return
  if (inst.ws?.readyState === WebSocket.OPEN) return
  cancelFlush(inst)
  try { inst.ws?.close() } catch { /* ignore */ }
  inst.ws = null
  connectWs(inst, inst.backendId)
}

export function setTerminalUserScrolling(leafId: string, scrolling: boolean) {
  const inst = registry.get(leafId)
  if (inst) {
    inst.userScrolling = scrolling
  }
}

function wireInput(inst: TerminalInstance) {
  inst.term.attachCustomKeyEventHandler((e) => {
    if (e.type === 'keydown' && e.ctrlKey && !e.altKey && !e.metaKey && (e.key === 'Enter' || e.key === 'Return')) {
      if (inst.ws?.readyState === WebSocket.OPEN) {
        inst.ws.send(JSON.stringify({ type: 'input', data: '\n' }))
      }
      return false
    }
    return true
  })

  inst.term.onData((data) => {
    if (inst.ws?.readyState === WebSocket.OPEN) {
      let finalData = data
      if (stickyModifiers.ctrl && data.length === 1) {
        const code = data.charCodeAt(0)
        if (code >= 97 && code <= 122) { // a-z
          finalData = String.fromCharCode(code - 96)
        } else if (code >= 65 && code <= 90) { // A-Z
          finalData = String.fromCharCode(code - 64)
        }
        resetStickyModifiers()
      } else if (stickyModifiers.alt && data.length === 1) {
        finalData = '\x1b' + data
        resetStickyModifiers()
      } else if (stickyModifiers.shift && data.length === 1) {
        // Uppercase letters; for other characters, xterm.js already applied
        // the shift modifier internally before emitting onData, so we only
        // need to handle the case where the user pressed a key without a
        // physical shift (sticky mode sends the raw key). Uppercasing letters
        // gives the expected shifted result.
        const code = data.charCodeAt(0)
        if (code >= 97 && code <= 122) {
          finalData = String.fromCharCode(code - 32)
        }
        resetStickyModifiers()
      }
      inst.ws.send(JSON.stringify({ type: 'input', data: finalData }))
    }
  })
}

// DEC private modes whose state must be re-applied to a re-attached
// xterm.js instance so mouse clicks / wheel scroll / bracketed paste keep
// working. Mirrors the server-side syncModes set in session.go.
const SYNC_MODES = new Set([1000, 1002, 1003, 1004, 1005, 1006, 1015, 1016, 2004])
// Matches a single DEC private mode set/reset, e.g. "\x1b[?1003h" or
// "\x1b[?2004l". TUI apps emit one mode per sequence.
const MODE_RE = new RegExp(String.fromCharCode(27) + '\\[\\?(\\d+)([hl])', 'g')

function trackModes(inst: TerminalInstance, data: string) {
  let m: RegExpExecArray | null
  MODE_RE.lastIndex = 0
  while ((m = MODE_RE.exec(data)) !== null) {
    const n = parseInt(m[1], 10)
    if (SYNC_MODES.has(n)) {
      inst._modes.set(n, m[2] === 'h')
    }
  }
}

function syncModes(inst: TerminalInstance): string {
  const set: number[] = []
  for (const [n, on] of inst._modes) {
    if (on) set.push(n)
  }
  set.sort((a, b) => a - b)
  if (set.length === 0) return ''
  return '\x1b[?' + set.join(';') + 'h'
}

function safeScrollToBottom(inst: TerminalInstance) {
  if (inst.userScrolling) return
  try {
    inst.term.scrollToBottom()
  } catch { /* ignore if not attached to DOM */ }
}

// scheduleFlush accumulates output chunks and flushes them to xterm.js in
// a single term.write per animation frame. This replaces the previous
// pattern of calling term.write(data, cb) + scrollToBottom for every WS
// message, which caused N independent renders per burst. Batching into
// one write per frame (~16ms) keeps the terminal responsive under high
// output (compiles, log tailing, find /) and eliminates visible jank.
function scheduleFlush(inst: TerminalInstance) {
  if (inst._detached) {
    // Terminal is detached (another tab is visible). Keep buffering WS
    // output into the ring buffer but do NOT schedule a render flush —
    // there is no live xterm.js instance to write to. On re-attach the
    // buffer is replayed in one shot.
    for (const ch of inst._pendingOutput) inst.buffer.push(ch)
    inst._pendingOutput = []
    return
  }
  if (inst._rafId !== 0) return
  inst._rafId = requestAnimationFrame(() => {
    inst._rafId = 0
    const chunks = inst._pendingOutput
    inst._pendingOutput = []
    if (chunks.length === 0) return
    const combined = chunks.length === 1 ? chunks[0] : chunks.join('')
    inst.term.write(combined, () => {
      safeScrollToBottom(inst)
    })
    for (const ch of chunks) {
      inst.buffer.push(ch)
    }
  })
}

function cancelFlush(inst: TerminalInstance) {
  if (inst._rafId !== 0) {
    cancelAnimationFrame(inst._rafId)
    inst._rafId = 0
  }
  if (inst._pendingOutput.length > 0) {
    const chunks = inst._pendingOutput
    inst._pendingOutput = []
    if (chunks.length > 0) {
      if (!inst._detached) {
        const combined = chunks.length === 1 ? chunks[0] : chunks.join('')
        try { inst.term.write(combined) } catch { /* ignore */ }
      }
      for (const ch of chunks) inst.buffer.push(ch)
    }
  }
}

function flushPending(inst: TerminalInstance) {
  const queue = inst._pendingQueue
  inst._pendingQueue = []
  for (const data of queue) {
    inst.term.write(data, () => {
      safeScrollToBottom(inst)
    })
    inst.buffer.push(data)
  }
  // After replay, any subsequently batched output should resume normal
  // rAF scheduling.
}

function connectWs(inst: TerminalInstance, backendId: string) {
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
  const ws = new WebSocket(`${protocol}//${location.host}/ws/terminals/${backendId}`)
  inst.ws = ws

  ws.onopen = () => {
    const dims = inst.fit.proposeDimensions()
    if (dims) ws.send(JSON.stringify({ type: 'resize', cols: dims.cols, rows: dims.rows }))
    // Re-send the current focus flag so a dropped socket that the backend
    // cleared on disconnect is restored. Without this, the agent status
    // idle-timeout and re-bind heuristics would treat the pane as unfocused
    // until the user clicked another pane and back, leaving a window where
    // a focused-but-recently-reconnected watcher could be falsely reverted
    // to idle.
    ws.send(JSON.stringify({ type: 'focus', focused: inst._focused }))
  }
  ws.onmessage = (event) => {
    try {
      const msg = JSON.parse(event.data)
      if (msg.type === 'output') {
        // Track mode state from the live stream so a later re-attach can
        // re-apply it even if the original sequence was evicted from the
        // 10k-entry buffer.
        trackModes(inst, msg.data)
        if (inst._replaying) {
          inst._pendingQueue.push(msg.data)
          return
        }
        // Batch: accumulate the chunk and flush once per animation frame
        // instead of writing + scrollToBottom per WS message.
        inst._pendingOutput.push(msg.data)
        scheduleFlush(inst)
      } else if (msg.type === 'resize') {
        const cols = Number(msg.cols)
        const rows = Number(msg.rows)
        if (cols > 0 && rows > 0 && !inst._detached) {
          inst.term.resize(cols, rows)
        }
        inst._padCols = Number(msg.padCols) || 0
        inst._padRows = Number(msg.padRows) || 0
        applyPadding(inst)
      } else if (msg.type === 'exit') {
        inst.exited = true
        onTerminalExit?.(inst.leafId)
      }
    } catch { /* skip */ }
  }
  ws.onclose = () => {
    if (inst.ws === ws) {
      // The WebSocket closed (page reload, navigation, network drop,
      // or the mobile OS suspended the PWA while backgrounded).
      // Do NOT kill the backend PTY — it must keep running with no
      // clients, so reconnecting later resumes the same session.
      // Actual process exit is reported separately via the "exit" message.
      inst.ws = null
      // If this terminal was intentionally released (mobile switched to
      // another terminal), do NOT auto-reconnect — that would re-pin the
      // backend PTY to mobile dimensions. A later attachTerminal call
      // (switching back to this terminal) re-opens it explicitly.
      if (inst._released) {
        return
      }
      // Auto-reconnect after a short delay, mirroring the pattern used
      // by the other WS clients in the app (workspaceStore, agentStatusStore,
      // fileTreeWs). Without this, every keystroke is silently dropped
      // once the OS kills the background socket, and the user is forced
      // to reload the page to regain input.
      if (!inst.exited && inst.backendId) {
        fetch(`/api/terminals/${encodeURIComponent(inst.backendId)}`)
          .then((res) => {
            if (res.status === 404) {
              inst.exited = true
              onTerminalExit?.(inst.leafId)
            } else {
              setTimeout(() => {
                if (!inst.exited && inst.ws === null) {
                  connectWs(inst, inst.backendId)
                }
              }, 1000)
            }
          })
          .catch(() => {
            // Network/offline error, continue retrying
            setTimeout(() => {
              if (!inst.exited && inst.ws === null) {
                connectWs(inst, inst.backendId)
              }
            }, 1000)
          })
      }
    }
  }
}

// waitForLayout resolves when the container element has non-zero width
// and height (i.e. the layout engine has applied final dimensions).
// react-resizable-panels applies sizes asynchronously, so calling term.open
// + fit.fit immediately after mount produces wrong cols/rows and garbled
// rendering. We wait for the layout to settle, with a 500ms fallback so
// we never block forever on a hidden/offscreen panel.
function waitForLayout(el: HTMLElement): Promise<void> {
  return new Promise((resolve) => {
    const rect = el.getBoundingClientRect()
    if (rect.width > 0 && rect.height > 0) {
      resolve()
      return
    }
    let resolved = false
    const finish = () => {
      if (resolved) return
      resolved = true
      ro.disconnect()
      clearTimeout(timer)
      resolve()
    }
    const ro = new ResizeObserver(() => {
      const r = el.getBoundingClientRect()
      if (r.width > 0 && r.height > 0) finish()
    })
    ro.observe(el)
    const timer = setTimeout(finish, 500)
  })
}

export async function attachTerminal(
  leafId: string,
  el: HTMLElement,
  cwd: string,
  cmd?: string[],
  env?: [string, string][],
): Promise<TerminalInstance> {
  const existing = registry.get(leafId)
  if (existing) {
    // Recreate the xterm instance bound to the new DOM element, while
    // preserving the existing WebSocket/backend connection.
    try { existing.term.dispose() } catch { /* ignore */ }
    const { term, fit } = makeTerminal()
    existing.term = term
    existing.fit = fit
    // The terminal is being re-attached to the DOM, so live output can
    // once again be rendered directly to xterm.js.
    existing._detached = false
    // A previous releaseTerminal call (mobile tab switch) set this flag to
    // suppress auto-reconnect. We're now explicitly re-attaching, so clear
    // it so a future socket drop reconnects normally.
    existing._released = false
    // Wait for the container to have real dimensions before opening
    // xterm.js, so fit.fit() computes correct cols/rows and the terminal
    // doesn't render garbled output that requires a manual resize.
    await waitForLayout(el)
    term.open(el)
    fit.fit()

    // Re-wire input handlers since the old term was disposed.
    wireInput(existing)

    if (existing.ws && existing.ws.readyState === WebSocket.OPEN) {
      // Replay buffered output so the terminal isn't blank after re-attach.
      // Queue incoming WS messages during replay to avoid interleaved writes.
      existing._replaying = true
      existing._pendingQueue = []
      if (existing.buffer.length > 0) {
        term.write(existing.buffer.join(''), () => {
          term.scrollToBottom()
          existing._replaying = false
          // Re-apply the tracked DEC private modes (mouse tracking, SGR
          // mouse, bracketed paste, etc.) so the fresh xterm.js instance
          // enters the same input-routing mode the running TUI expects.
          const sync = syncModes(existing)
          if (sync) term.write(sync)
          flushPending(existing)
        })
      } else {
        existing._replaying = false
        const sync = syncModes(existing)
        if (sync) term.write(sync)
        flushPending(existing)
      }

      // Send a resize for the new dimensions.
      const dims = fit.proposeDimensions()
      if (dims) existing.ws.send(JSON.stringify({ type: 'resize', cols: dims.cols, rows: dims.rows }))
    } else {
      // WebSocket is closed, closing, or null. Clear the local buffer to prevent
      // duplication of replayed scrollback from the new WS connection.
      existing.buffer.clear()
      existing._replaying = false
      existing._pendingQueue = []
      if (!existing.exited && existing.backendId) {
        connectWs(existing, existing.backendId)
      }
    }

    return existing
  }

  const { term, fit } = makeTerminal()
  // Wait for the container to have real dimensions before opening
  // xterm.js, so fit.fit() computes correct cols/rows and the terminal
  // doesn't render garbled output that requires a manual resize.
  await waitForLayout(el)
  term.open(el)
  fit.fit()

  const inst: TerminalInstance = { leafId, term, fit, ws: null, backendId: '', buffer: new RingBuffer<string>(), exited: false, _replaying: false, _pendingQueue: [], _pendingOutput: [], _rafId: 0, _modes: new Map(), userScrolling: false, _detached: false, _padCols: 0, _padRows: 0, _focused: false, _released: false }
  registry.set(leafId, inst)
  wireInput(inst)

  try {
    const backendId = await ensureBackend(leafId, cwd, cmd, env)
    inst.backendId = backendId
    connectWs(inst, backendId)
  } catch (err) {
    console.error('terminal backend init failed:', err)
  }

  notify()
  return inst
}

export function setAllTerminalFontSizes(size: number) {
  for (const inst of registry.values()) {
    inst.term.options.fontSize = size
    // Cell pixel dimensions change with the font size, so re-apply the
    // pixel-based padding margins on the next frame after xterm.js
    // updates its renderer dimensions.
    requestAnimationFrame(() => applyPadding(inst))
  }
}

export function setAllTerminalThemes(theme: 'dark' | 'light') {
  const t = theme === 'light' ? lightTerminalTheme : darkTerminalTheme
  for (const inst of registry.values()) {
    inst.term.options.theme = t
  }
  localStorage.setItem('caw:terminalTheme', theme)
}

export function destroyTerminal(leafId: string, deleteBranch?: boolean) {
  const inst = registry.get(leafId)
  if (!inst) return
  cancelFlush(inst)
  try { inst.ws?.close() } catch { /* ignore */ }
  try { inst.term.dispose() } catch { /* ignore */ }
  registry.delete(leafId)
  notify()
  fetch(`/api/terminals/${encodeURIComponent(leafId)}${deleteBranch ? '?deleteBranch=true' : ''}`, {
    method: 'DELETE',
  }).catch(() => {})
}

export function detachTerminal(leafId: string) {
  const inst = registry.get(leafId)
  if (!inst) return
  cancelFlush(inst)
  // Mark the terminal as detached from the DOM so the live WS output
  // handler keeps buffering into the ring buffer WITHOUT writing to the
  // disposed xterm.js instance. The WebSocket is intentionally left open
  // so that switching back to this tab replays the local ring buffer
  // instantly instead of reconnecting and reloading scrollback from the
  // backend. Closing the WS here is what caused the "reload on every tab
  // switch" regression the ring buffer was meant to prevent.
  inst._detached = true
  try { inst.term.dispose() } catch { /* ignore */ }
}

// releaseTerminal drops this client's hold on a terminal (disposes the
// xterm.js instance, closes the WebSocket, removes the registry entry)
// WITHOUT asking the backend to kill the PTY. Use this when a terminal
// disappears from THIS client's view only because another browser
// reshaped the shared workspace state — the shared backend PTY must
// keep running so other clients (or this client, on re-open) can
// reattach. destroyTerminal (which DOES kill) is reserved for explicit
// user-initiated closes (X button, forceClosePane/Tab).
export function releaseTerminal(leafId: string) {
  const inst = registry.get(leafId)
  if (!inst) return
  cancelFlush(inst)
  // Mark as intentionally released so ws.onclose does not auto-reconnect
  // and re-pin the backend PTY (used by the mobile single-active-terminal
  // model). A later attachTerminal call clears this flag and reconnects.
  inst._released = true
  try { inst.ws?.close() } catch { /* ignore */ }
  try { inst.term.dispose() } catch { /* ignore */ }
  registry.delete(leafId)
  notify()
}

export function getTerminal(leafId: string): TerminalInstance | undefined {
  return registry.get(leafId)
}

export function useTerminalIds(): string[] {
  const [ids, setIds] = useState<string[]>(() => Array.from(registry.keys()))
  useEffect(() => {
    const sub = () => setIds(Array.from(registry.keys()))
    subscribers.add(sub)
    sub()
    return () => { subscribers.delete(sub) }
  }, [])
  return ids
}
