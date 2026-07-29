import { useEffect, useMemo, useRef, useState } from 'react'
import { Copy, Clipboard } from 'lucide-react'
import { attachTerminal, detachTerminal, releaseTerminal, getTerminal, getTerminalBackground, reconnectTerminalWs, setTerminalUserScrolling, type TerminalInstance } from '@/features/terminal/services/terminalRegistry'
import { SmartContextMenu } from '@/features/explorer/components/SmartContextMenu'

interface TerminalPanelProps {
  terminalId: string
  cwd: string
  cmd?: string[]
  env?: [string, string][]
  isActive?: boolean
}

function copyToClipboard(text: string): boolean {
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(text).catch(() => {
      fallbackCopyToClipboard(text)
    })
    return true
  }
  return fallbackCopyToClipboard(text)
}

function fallbackCopyToClipboard(text: string): boolean {
  const textArea = document.createElement('textarea')
  textArea.value = text
  textArea.style.top = '0'
  textArea.style.left = '0'
  textArea.style.position = 'fixed'
  textArea.style.opacity = '0'
  document.body.appendChild(textArea)
  textArea.focus()
  textArea.select()
  try {
    const successful = document.execCommand('copy')
    document.body.removeChild(textArea)
    return successful
  } catch {
    document.body.removeChild(textArea)
    return false
  }
}

export function TerminalPanel({ terminalId, cwd, cmd, env, isActive }: TerminalPanelProps) {
  const elRef = useRef<HTMLDivElement>(null)
  const contextMenuRef = useRef<HTMLDivElement>(null)
  const resizeObsRef = useRef<ResizeObserver | null>(null)
  const fitTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const lastDimsRef = useRef<string>('')
  const isActiveRef = useRef(isActive)
  isActiveRef.current = isActive
  const keyboardOpenRef = useRef(false)

  const cmdKey = useMemo(() => JSON.stringify(cmd ?? []), [cmd])
  const cmdRef = useRef(cmd)
  cmdRef.current = cmd
  const stableCmd = useMemo(() => {
    void cmdKey
    return cmdRef.current
  }, [cmdKey])
  const cwdRef = useRef(cwd)
  cwdRef.current = cwd

  const [contextMenu, setContextMenu] = useState<{ x: number; y: number } | null>(null)
  const [savedSelection, setSavedSelection] = useState('')
  const [tuiClipboard, setTuiClipboard] = useState('')

  useEffect(() => {
    const el = elRef.current
    if (!el) return
    let cancelled = false
    let inst: TerminalInstance | null = null

    const flushResize = () => {
      fitTimerRef.current = null
      if (!inst) return
      try {
        inst.fit.fit()
        const dims = inst.fit.proposeDimensions()
        if (!dims) return
        const key = `${dims.cols}x${dims.rows}`
        if (key === lastDimsRef.current) return
        lastDimsRef.current = key
        if (inst.ws?.readyState === WebSocket.OPEN) {
          inst.ws.send(JSON.stringify({ type: 'resize', cols: dims.cols, rows: dims.rows }))
        }
      } catch { /* ignore */ }
    }

    const handleRightClickMousedown = (e: MouseEvent) => {
      if (e.button === 2) {
        e.stopPropagation()
      }
    }
    el.addEventListener('mousedown', handleRightClickMousedown, true)
    el.addEventListener('mouseup', handleRightClickMousedown, true)

    const onVisibility = () => {
      if (document.visibilityState === 'visible') {
        flushResize()
        const t = getTerminal(terminalId)
        if (t) {
          if (t.ws === null && !t.exited) {
            reconnectTerminalWs(terminalId)
          }
          if (isActiveRef.current) {
            t.term.focus()
          }
        }
      }
    }
    document.addEventListener('visibilitychange', onVisibility)
    document.addEventListener('pageshow', onVisibility)
    window.addEventListener('focus', onVisibility)

    ;(async () => {
      // Wait one paint frame before attaching the new terminal. The cleanup
      // phase (which runs synchronously before this effect) strips the old
      // canvas and paints a solid placeholder div in its place. Without this
      // deferral, the async attachTerminal resolves before the browser paints
      // a single frame — the placeholder is added and removed without ever
      // reaching the GPU compositor, so the old terminal's last canvas frame
      // ghosts through (visible as stale "white text") until the new canvas
      // gets its first content paint. Waiting one rAF guarantees the browser
      // composites the placeholder frame, replacing the stale GPU texture,
      // before the new terminal's canvas is created.
      await new Promise<void>((r) => requestAnimationFrame(() => r()))
      inst = await attachTerminal(terminalId, el, cwdRef.current, stableCmd, env)
      if (cancelled) return

      if (isActiveRef.current) {
        inst.term.focus()
      }

      const ro = new ResizeObserver(() => {
        if (fitTimerRef.current) clearTimeout(fitTimerRef.current)
        fitTimerRef.current = setTimeout(flushResize, 80)
      })
      ro.observe(el)
      resizeObsRef.current = ro

      // Force a resize on the first mount. waitForLayout inside
      // attachTerminal resolves as soon as the container has non-zero
      // dimensions, but the final layout often hasn't settled yet (sidebar
      // transitions, flex sizing, panel drag handles still animating). The
      // initial fit() / WS resize therefore runs against stale intermediate
      // dimensions and the terminal renders garbled until something else
      // triggers a ResizeObserver callback (collapsing a sidebar, etc.).
      // Double-rAF defers the re-fit past the browser's layout + paint pass
      // so proposeDimensions reads the final, settled container size.
      requestAnimationFrame(() => {
        requestAnimationFrame(() => {
          flushResize()
        })
      })
    })()

    return () => {
      cancelled = true
      document.removeEventListener('visibilitychange', onVisibility)
      document.removeEventListener('pageshow', onVisibility)
      window.removeEventListener('focus', onVisibility)
      el.removeEventListener('mousedown', handleRightClickMousedown, true)
      el.removeEventListener('mouseup', handleRightClickMousedown, true)
      if (fitTimerRef.current) {
        clearTimeout(fitTimerRef.current)
        fitTimerRef.current = null
      }
      resizeObsRef.current?.disconnect()
      resizeObsRef.current = null
      // Immediately strip the terminal's canvas/textarea from the DOM so the
      // previous terminal's last-rendered frame can't ghost through the React
      // commit when the container <div> is reused for the next tab's terminal.
      // detachTerminal/releaseTerminal also dispose the term (which removes the
      // canvas), but doing it here first guarantees it happens synchronously in
      // the cleanup phase, before the new terminal's async open() runs.
      while (el.firstChild) el.removeChild(el.firstChild)
      // Hide the container immediately in the cleanup phase — before the
      // browser composites the next frame. The new terminal's attachTerminal
      // will un-hide it once content has been written and painted. Combined
      // with the rAF deferral in the effect body, this guarantees no stale
      // GPU texture from the previous terminal's canvas can ghost through:
      // the container is invisible for at least one full composite cycle.
      el.style.visibility = 'hidden'
      // Paint a solid placeholder that matches the terminal's background over
      // the cleared container. Even though the container is hidden, this
      // gives the GPU compositor a solid layer to composite (instead of a
      // transparent hole) when visibility is restored.
      const placeholder = document.createElement('div')
      placeholder.style.position = 'absolute'
      placeholder.style.inset = '0'
      placeholder.style.background = getTerminalBackground()
      el.appendChild(placeholder)
      // Desktop buffers non-active terminals (detach keeps the WS open so
      // re-attaching replays the local ring buffer instantly). Mobile
      // renders only the selected terminal, so switching terminals must
      // fully release the previous one — closing the WS without killing
      // the backend PTY — so the backend's smallest-viewer resize can grow
      // the PTY back to desktop size when the mobile viewer drops off.
      if (window.innerWidth < 768) {
        releaseTerminal(terminalId)
      } else {
        detachTerminal(terminalId)
      }
    }
  }, [terminalId, stableCmd, env])

  useEffect(() => {
    if (!isActive) return
    const inst = getTerminal(terminalId)
    if (inst) {
      inst.term.focus()
    }
  }, [isActive, terminalId])

  // Notify the backend when this pane gains or loses the user's focus so the
  // agent status heuristics (idle-timeout watchdog and the watchers' re-bind
  // pass) can account for which terminal the user is currently driving. We
  // send a `focus`/`blur` message on the terminal WS rather than a separate
  // HTTP/WS channel because the lifecycle of the signal is bound to the
  // terminal connection itself. The backend's agent package tracks this in a
  // single-focus map keyed by leaf id. We also mirror the flag onto the
  // TerminalInstance so connectWs.onopen can re-send it after a reconnect,
  // preventing a dropped socket from leaving the backend with a stale
  // unfocused state.
  useEffect(() => {
    const inst = getTerminal(terminalId)
    const focused = isActive === true
    if (inst) inst._focused = focused
    if (inst?.ws?.readyState === WebSocket.OPEN) {
      inst.ws.send(JSON.stringify({ type: 'focus', focused }))
    }
  }, [isActive, terminalId])

  // Listen for app-level focus requests (e.g. when the Command Center closes
  // and focus should return to the last active terminal pane).
  useEffect(() => {
    const onFocusRequest = (e: Event) => {
      const detail = (e as CustomEvent).detail
      if (!detail || detail.paneId !== terminalId) return
      const inst = getTerminal(terminalId)
      if (inst) {
        inst.term.focus()
      }
    }
    window.addEventListener('caw:focus-terminal', onFocusRequest as EventListener)
    return () => window.removeEventListener('caw:focus-terminal', onFocusRequest as EventListener)
  }, [terminalId])

  useEffect(() => {
    const vv = window.visualViewport
    if (!vv) return
    const baseline = vv.height
    const el = elRef.current
    const onResize = () => {
      const shrunk = baseline - vv.height
      const open = shrunk > baseline * 0.18
      keyboardOpenRef.current = open
      if (el) {
        if (open) el.classList.add('terminal-keyboard-open')
        else el.classList.remove('terminal-keyboard-open')
      }
    }
    vv.addEventListener('resize', onResize)
    vv.addEventListener('scroll', onResize)
    return () => {
      vv.removeEventListener('resize', onResize)
      vv.removeEventListener('scroll', onResize)
      if (el) el.classList.remove('terminal-keyboard-open')
    }
  }, [])

  useEffect(() => {
    const el = elRef.current
    if (!el) return

    const LINES_PER_PX = parseFloat(localStorage.getItem('caw:terminalScrollSensitivity') || '0.02')
    const FRICTION = parseFloat(localStorage.getItem('caw:terminalScrollFriction') || '0.85')
    const VELOCITY_THRESHOLD = parseFloat(localStorage.getItem('caw:terminalScrollVelocityThreshold') || '0.05')
    const SCROLL_GRACE_MS = parseInt(localStorage.getItem('caw:terminalScrollGrace') || '1200', 10)

    let lastY = 0
    let lastX = 0
    let lastTime = 0
    let velocity = 0
    let active = false
    let rafId = 0
    let graceTimer: ReturnType<typeof setTimeout> | null = null
    let accumDelta = 0
    // True once a touchmove actually scrolled (vs. a tap that goes straight to
    // touchend). Used by touchend to decide whether to suppress the terminal's
    // focus-on-tap behavior: a tap should still focus (open the keyboard), but
    // ending a scroll should NOT yank focus / pop the keyboard.
    let moved = false

    // SGR mouse wheel encoding: when a TUI enables mouse tracking (modes
    // 1000/1002/1003 + SGR 1006), wheel events must be sent as SGR mouse
    // escape sequences (ESC[<button;col;row M/m), NOT arrow keys — a TUI like
    // OpenCode interprets arrow keys as chat-history navigation, while SGR
    // wheel sequences scroll its own viewport. ghostty-web's handleWheel only
    // sends arrow keys for alt-screen, so we encode SGR mouse ourselves when
    // mouse tracking is active. Wheel button codes: 64 = up, 65 = down.
    const encodeSgrWheel = (inst: TerminalInstance, button: number, clientX: number, clientY: number) => {
      const canvas = inst.term.renderer?.getCanvas?.()
      if (!canvas) return null
      const rect = canvas.getBoundingClientRect()
      const cw = inst.term.renderer?.charWidth
      const ch = inst.term.renderer?.charHeight
      if (!cw || !ch || cw <= 0 || ch <= 0) return null
      const col = Math.max(1, Math.min(inst.term.cols, Math.floor((clientX - rect.left) / cw) + 1))
      const row = Math.max(1, Math.min(inst.term.rows, Math.floor((clientY - rect.top) / ch) + 1))
      // SGR mouse: ESC[<button;col;row M (press) then m (release) for a wheel
      // tick. Some TUIs want the release; sending both is safest.
      return `\x1b[<${button};${col};${row}M\x1b[<${button};${col};${row}m`
    }

    const dispatchWheel = (deltaY: number, clientX: number, clientY: number) => {
      // ghostty-web has no DOM scroll container — scrolling is programmatic
      // via the Terminal's viewport API. Drive the terminal directly:
      //  - mouse tracking active (TUI like OpenCode): encode SGR mouse wheel
      //    sequences so the TUI scrolls its own viewport.
      //  - alt-screen without mouse tracking: send up/down arrow keys.
      //  - normal buffer (shell): call scrollLines to move the scrollback.
      accumDelta += deltaY
      const wholeLines = Math.trunc(accumDelta)
      if (wholeLines === 0) return
      accumDelta -= wholeLines
      const inst = getTerminal(terminalId)
      if (!inst) return
      const term = inst.term
      if (!inst.ws || inst.ws.readyState !== WebSocket.OPEN) return
      try {
        // Query the terminal's own mode state (authoritative — kept in sync by
        // the backend's mode-sync sequence on reconnect and by the live stream).
        // Relying on inst._modes breaks after a mobile re-attach creates a fresh
        // terminal: the client-tracked map is empty until the sync sequence is
        // processed, while the terminal already knows its real mode state.
        const mouseTracking = term.hasMouseTracking()
        const sgr = term.getMode(1006)
        if (mouseTracking && sgr) {
          const button = wholeLines > 0 ? 65 : 64
          const count = Math.min(Math.abs(wholeLines), 5)
          const seq = encodeSgrWheel(inst, button, clientX, clientY)
          if (seq) inst.ws.send(JSON.stringify({ type: 'input', data: seq.repeat(count) }))
          return
        }
        if (term.buffer.active.type === 'alternate') {
          const dir = wholeLines > 0 ? '\x1B[B' : '\x1B[A'
          const count = Math.min(Math.abs(wholeLines), 5)
          inst.ws.send(JSON.stringify({ type: 'input', data: dir.repeat(count) }))
        } else {
          term.scrollLines(wholeLines)
        }
      } catch { /* ignore if not attached to DOM */ }
    }

    const beginGrace = () => {
      if (graceTimer) clearTimeout(graceTimer)
      graceTimer = setTimeout(() => {
        setTerminalUserScrolling(terminalId, false)
        graceTimer = null
      }, SCROLL_GRACE_MS)
    }

    const momentum = () => {
      if (Math.abs(velocity) < VELOCITY_THRESHOLD) {
        rafId = 0
        beginGrace()
        return
      }
      const delta = velocity * 16 * LINES_PER_PX
      dispatchWheel(delta, lastX, lastY)
      velocity *= FRICTION
      rafId = requestAnimationFrame(momentum)
    }

    const onTouchStart = (e: TouchEvent) => {
      if (e.touches.length !== 1) return
      if (keyboardOpenRef.current) return
      if (rafId) {
        cancelAnimationFrame(rafId)
        rafId = 0
      }
      if (graceTimer) {
        clearTimeout(graceTimer)
        graceTimer = null
      }
      const t = e.touches[0]
      lastY = t.clientY
      lastX = t.clientX
      lastTime = Date.now()
      velocity = 0
      accumDelta = 0
      active = true
      moved = false
      setTerminalUserScrolling(terminalId, true)
    }

    const onTouchMove = (e: TouchEvent) => {
      if (!active || e.touches.length !== 1) return
      // Prevent the browser's native touch scrolling/zoom and stop the event
      // from reaching ghostty-web's canvas touchend (focus) listener, so our
      // programmatic scrollLines/arrow-key dispatch is the sole scroll
      // authority for both alt-buffer TUIs (arrow-key / mouse protocol) and
      // normal-buffer shells (viewport scrollback).
      e.preventDefault()
      e.stopPropagation()
      moved = true
      const t = e.touches[0]
      const now = Date.now()
      const dy = lastY - t.clientY
      const dt = Math.max(1, now - lastTime)
      velocity = dy / dt
      lastY = t.clientY
      lastX = t.clientX
      lastTime = now
      dispatchWheel(dy * LINES_PER_PX, t.clientX, t.clientY)
    }

    const onTouchEnd = (e: TouchEvent) => {
      if (!active) return
      active = false
      // If this was a real scroll gesture (not a tap), stop the event so
      // ghostty-web's canvas touchend listener (which focuses the input
      // textarea and pops the mobile keyboard) doesn't fire. A plain tap with
      // no movement should still focus — the user expects the keyboard on tap.
      if (moved) {
        e.preventDefault()
        e.stopPropagation()
      }
      if (Math.abs(velocity) >= VELOCITY_THRESHOLD) {
        rafId = requestAnimationFrame(momentum)
      } else {
        beginGrace()
      }
    }

    el.addEventListener('touchstart', onTouchStart, { passive: true, capture: true })
    el.addEventListener('touchmove', onTouchMove, { passive: false, capture: true })
    el.addEventListener('touchend', onTouchEnd, { passive: false, capture: true })
    el.addEventListener('touchcancel', onTouchEnd, { passive: false, capture: true })

    return () => {
      if (rafId) cancelAnimationFrame(rafId)
      if (graceTimer) clearTimeout(graceTimer)
      el.removeEventListener('touchstart', onTouchStart, { capture: true } as EventListenerOptions)
      el.removeEventListener('touchmove', onTouchMove, { capture: true } as EventListenerOptions)
      el.removeEventListener('touchend', onTouchEnd, { capture: true } as EventListenerOptions)
      el.removeEventListener('touchcancel', onTouchEnd, { capture: true } as EventListenerOptions)
      setTerminalUserScrolling(terminalId, false)
    }
  }, [terminalId])

  useEffect(() => {
    if (!contextMenu) return
    const handleClose = (e: MouseEvent) => {
      if (contextMenuRef.current && contextMenuRef.current.contains(e.target as Node)) {
        return
      }
      setContextMenu(null)
    }
    document.addEventListener('mousedown', handleClose)
    document.addEventListener('click', handleClose)
    return () => {
      document.removeEventListener('mousedown', handleClose)
      document.removeEventListener('click', handleClose)
    }
  }, [contextMenu])

  const inst = getTerminal(terminalId)

  const handleContextMenu = (e: React.MouseEvent) => {
    e.preventDefault()
    let sel = ''
    let tuiClip = ''
    if (inst) {
      sel = inst.term.getSelection()
      tuiClip = (inst.term as any)._tuiClipboard || ''
    }
    setSavedSelection(sel)
    setTuiClipboard(tuiClip)
    setContextMenu({ x: e.clientX, y: e.clientY })
  }

  const handleCopy = (e: React.MouseEvent) => {
    e.stopPropagation()
    if (savedSelection) {
      copyToClipboard(savedSelection)
    }
    setContextMenu(null)
  }

  const handleCopyTui = (e: React.MouseEvent) => {
    e.stopPropagation()
    if (tuiClipboard) {
      copyToClipboard(tuiClipboard)
    }
    setContextMenu(null)
  }

  const handlePaste = async (e: React.MouseEvent) => {
    e.stopPropagation()
    if (inst) {
      try {
        if (!navigator.clipboard || !navigator.clipboard.readText) {
          alert('Clipboard paste is only supported in secure contexts (HTTPS/localhost). Please use Ctrl+V / Cmd+V directly in the terminal.')
          setContextMenu(null)
          return
        }
        const text = await navigator.clipboard.readText()
        if (text && inst.ws?.readyState === WebSocket.OPEN) {
          inst.ws.send(JSON.stringify({ type: 'input', data: text }))
        }
      } catch {
        alert('Could not paste from clipboard. Please allow clipboard access or use Ctrl+V / Cmd+V directly in the terminal.')
      }
    }
    setContextMenu(null)
  }

  return (
    <div
      className="relative h-full w-full overflow-hidden custom-context-menu"
      onContextMenu={handleContextMenu}
      data-testid={`terminal-panel-${terminalId}`}
    >
      <div ref={elRef} className="terminal-container h-full w-full overflow-hidden" style={{ backgroundColor: getTerminalBackground() }} />
      {contextMenu && (
        <SmartContextMenu x={contextMenu.x} y={contextMenu.y} ref={contextMenuRef}>
          <button
            onClick={handleCopy}
            disabled={!savedSelection}
            className={`flex items-center w-full px-3 py-1.5 text-xs text-left cursor-pointer gap-2 ${
              savedSelection ? 'text-foreground/80 hover:bg-accent hover:text-foreground' : 'text-foreground/30 cursor-not-allowed'
            }`}
          >
            <Copy size={14} />
            <span>Copy</span>
          </button>
          <button
            onClick={handleCopyTui}
            disabled={!tuiClipboard}
            className={`flex items-center w-full px-3 py-1.5 text-xs text-left cursor-pointer gap-2 ${
              tuiClipboard ? 'text-foreground/80 hover:bg-accent hover:text-foreground' : 'text-foreground/30 cursor-not-allowed'
            }`}
          >
            <Copy size={14} />
            <span>Copy TUI Clipboard</span>
          </button>
          <button
            onClick={handlePaste}
            className="flex items-center w-full px-3 py-1.5 text-xs text-foreground/80 hover:bg-accent hover:text-foreground text-left cursor-pointer gap-2"
          >
            <Clipboard size={14} />
            <span>Paste</span>
          </button>
        </SmartContextMenu>
      )}
    </div>
  )
}