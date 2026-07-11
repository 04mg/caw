import { useEffect, useState } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'

export interface TerminalInstance {
  leafId: string
  term: Terminal
  fit: FitAddon
  ws: WebSocket | null
  /** Buffer of output received before the term was ready, replayed on open. */
  buffer: string[]
  exited: boolean
  /** @internal set during buffer replay to queue incoming ws messages */
  _replaying: boolean
  /** @internal messages queued during replay */
  _pendingQueue: string[]
  /**
   * @internal Last-set state of the DEC private modes that affect input
   * routing (mouse tracking, SGR mouse, bracketed paste, focus reporting).
   * Tracked from the live WS stream so a re-attached xterm.js instance can
   * re-enter the same mode the running TUI expects, even after the 10k
   * buffer cap evicted the original mode-set sequence.
   */
  _modes: Map<number, boolean>
  /**
   * @internal Set to true while the user is actively scrolling (touch drag
   * or momentum). When true, safeScrollToBottom is suppressed so incoming
   * output does not yank the view back to the bottom.
   */
  userScrolling: boolean
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

async function ensureBackend(leafId: string, cwd: string, cmd?: string[]): Promise<string> {
  if (!cmd || cmd.length === 0) {
    const customShell = localStorage.getItem('caw:defaultShell')
    if (customShell) cmd = [customShell]
  }
  const res = await fetch('/api/terminals', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id: leafId, cwd: cwd || '', cmd }),
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
    return dims
  }
  return { term, fit }
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
const MODE_RE = /\x1b\[\?(\d+)([hl])/g

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

function flushPending(inst: TerminalInstance) {
  const queue = inst._pendingQueue
  inst._pendingQueue = []
  for (const data of queue) {
    inst.term.write(data, () => {
      safeScrollToBottom(inst)
    })
    inst.buffer.push(data)
    if (inst.buffer.length > 10000) inst.buffer.shift()
  }
}

function connectWs(inst: TerminalInstance, backendId: string) {
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
  const ws = new WebSocket(`${protocol}//${location.host}/ws/terminals/${backendId}`)
  inst.ws = ws

  ws.onopen = () => {
    const dims = inst.fit.proposeDimensions()
    if (dims) ws.send(JSON.stringify({ type: 'resize', cols: dims.cols, rows: dims.rows }))
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
        inst.term.write(msg.data, () => {
          safeScrollToBottom(inst)
        })
        inst.buffer.push(msg.data)
        if (inst.buffer.length > 10000) inst.buffer.shift()
      } else if (msg.type === 'resize') {
        const cols = Number(msg.cols)
        const rows = Number(msg.rows)
        if (cols > 0 && rows > 0) {
          inst.term.resize(cols, rows)
        }
      } else if (msg.type === 'exit') {
        inst.exited = true
        onTerminalExit?.(inst.leafId)
      }
    } catch { /* skip */ }
  }
  ws.onclose = () => {
    if (inst.ws === ws) {
      // The WebSocket closed (page reload, navigation, network drop).
      // Do NOT kill the backend PTY — it must keep running with no
      // clients, so reconnecting later resumes the same session.
      // Actual process exit is reported separately via the "exit" message.
      inst.ws = null
    }
  }
}

export async function attachTerminal(
  leafId: string,
  el: HTMLElement,
  cwd: string,
  cmd?: string[],
): Promise<TerminalInstance> {
  const existing = registry.get(leafId)
  if (existing) {
    // Recreate the xterm instance bound to the new DOM element, while
    // preserving the existing WebSocket/backend connection.
    try { existing.term.dispose() } catch { /* ignore */ }
    const { term, fit } = makeTerminal()
    existing.term = term
    existing.fit = fit
    term.open(el)
    // Fit first so the terminal dimensions are correct before writing.
    fit.fit()
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
        // Without this, clicks and wheel scroll break after switching
        // tabs / remounting, because the new xterm.js starts with all
        // private modes OFF even though the TUI still has them on.
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

    // Re-wire input handlers since the old term was disposed.
    wireInput(existing)

    // Send a resize for the new dimensions.
    if (existing.ws?.readyState === WebSocket.OPEN) {
      const dims = fit.proposeDimensions()
      if (dims) existing.ws.send(JSON.stringify({ type: 'resize', cols: dims.cols, rows: dims.rows }))
    }

    return existing
  }

  const { term, fit } = makeTerminal()
  term.open(el)
  fit.fit()

  const inst: TerminalInstance = { leafId, term, fit, ws: null, buffer: [], exited: false, _replaying: false, _pendingQueue: [], _modes: new Map(), userScrolling: false }
  registry.set(leafId, inst)
  wireInput(inst)

  try {
    const backendId = await ensureBackend(leafId, cwd, cmd)
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
