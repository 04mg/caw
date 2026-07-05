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
  const res = await fetch('/api/terminal/create', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id: leafId, cwd: cwd || '', cmd }),
  })
  const { id } = await res.json()
  return id
}

function makeTerminal(): { term: Terminal; fit: FitAddon } {
  const savedSize = parseInt(localStorage.getItem('caw:terminalFontSize') || '13', 10)
  const fontSize = isNaN(savedSize) ? 13 : Math.max(8, Math.min(32, savedSize))
  const term = new Terminal({
    cursorBlink: true,
    fontSize,
    fontFamily: "'JetBrainsMono Nerd Font', ui-monospace, SFMono-Regular, 'Cascadia Code', 'Fira Code', monospace",
    scrollback: 10000,
    theme: {
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
    },
  })
  const fit = new FitAddon()
  term.loadAddon(fit)
  return { term, fit }
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
      inst.ws.send(JSON.stringify({ type: 'input', data }))
    }
  })
}

function flushPending(inst: TerminalInstance) {
  const queue = inst._pendingQueue
  inst._pendingQueue = []
  for (const data of queue) {
    inst.term.write(data, () => {
      inst.term.scrollToBottom()
    })
    inst.buffer.push(data)
    if (inst.buffer.length > 10000) inst.buffer.shift()
  }
}

function connectWs(inst: TerminalInstance, backendId: string) {
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
  const ws = new WebSocket(`${protocol}//${location.host}/ws/terminal/${backendId}`)
  inst.ws = ws

  ws.onopen = () => {
    const dims = inst.fit.proposeDimensions()
    if (dims) ws.send(JSON.stringify({ type: 'resize', cols: dims.cols, rows: dims.rows }))
  }
  ws.onmessage = (event) => {
    try {
      const msg = JSON.parse(event.data)
      if (msg.type === 'output') {
        if (inst._replaying) {
          inst._pendingQueue.push(msg.data)
          return
        }
        inst.term.write(msg.data, () => {
          inst.term.scrollToBottom()
        })
        inst.buffer.push(msg.data)
        if (inst.buffer.length > 10000) inst.buffer.shift()
      } else if (msg.type === 'exit') {
        inst.exited = true
        onTerminalExit?.(inst.leafId)
      }
    } catch { /* skip */ }
  }
  ws.onclose = () => {
    if (inst.ws === ws) {
      inst.ws = null
      if (!inst.exited && registry.has(inst.leafId)) {
        onTerminalExit?.(inst.leafId)
      }
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
        flushPending(existing)
      })
    } else {
      existing._replaying = false
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

  const inst: TerminalInstance = { leafId, term, fit, ws: null, buffer: [], exited: false, _replaying: false, _pendingQueue: [] }
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

export function destroyTerminal(leafId: string) {
  const inst = registry.get(leafId)
  if (!inst) return
  try { inst.ws?.close() } catch { /* ignore */ }
  try { inst.term.dispose() } catch { /* ignore */ }
  registry.delete(leafId)
  notify()
}

export function detachTerminal(leafId: string) {
  const inst = registry.get(leafId)
  if (!inst) return
  try { inst.term.dispose() } catch { /* ignore */ }
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
