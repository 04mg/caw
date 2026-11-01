import { useEffect, useMemo, useRef, useState } from 'react'
import { Copy, Clipboard, RotateCw, ArrowDown } from 'lucide-react'
import { attachTerminal, parkTerminal, releaseTerminal, getTerminal, getTerminalBackground, reconnectTerminalWs, setTerminalUserScrolling, isTerminalReplaying, type TerminalInstance } from '@/features/terminal/services/terminalRegistry'
import { SmartContextMenu } from '@/features/explorer/components/SmartContextMenu'
import { getCustomization, subscribePrefs } from '@/features/prefs/stores/prefsStore'
import { setAllTerminalBackgroundTransparency } from '../services/terminalRegistry'
import { attachTouchScroll, isTerminalAtBottom } from '../services/touchScroll'

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

// Rows the view must be scrolled up before the "jump to bottom" button shows.
const JUMP_TO_BOTTOM_MIN_ROWS = 2

export function TerminalPanel({ terminalId, cwd, cmd, env, isActive }: TerminalPanelProps) {
  const elRef = useRef<HTMLDivElement>(null)
  const contextMenuRef = useRef<HTMLDivElement>(null)
  const resizeObsRef = useRef<ResizeObserver | null>(null)
  const fitTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const lastDimsRef = useRef<string>('')
  const lastResizeAtRef = useRef<number>(0)
  // True until the first flushResize that proceeds after attach. The
  // terminalRegistry replay-end handler forces a full-viewport repaint once
  // the scrollback replay drains, but on a fresh attach in a new tab —
  // especially right after a workspace switch — that repaint can land before
  // the freshly-mounted panel has been rasterized, so the canvas stays
  // stale/black. flushResize's own short-circuit path (dims already match)
  // never calls term.refresh, so nothing else re-paints until a real grid
  // change (manual resize) or a visibility change. Forcing one repaint here
  // once the layout has settled recovers that wasted early first-paint.
  const firstPaintRef = useRef(true)
  const reloadRef = useRef<() => void>(() => {})
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
  const [background, setBackground] = useState(() => getCustomization().terminal.background)
  // "Jump to bottom" button, shown while the scrollback is scrolled away from
  // the latest output.
  const [showJump, setShowJump] = useState(false)

  useEffect(() => subscribePrefs(() => {
    const next = getCustomization().terminal.background
    setBackground(next)
    setAllTerminalBackgroundTransparency(Boolean(next.assetId) || next.applyToPage)
  }), [])
  useEffect(() => {
    const onCustomizationUpdated = (event: Event) => {
      const next = (event as CustomEvent<{ terminal?: { background?: typeof background } }>).detail?.terminal?.background
      if (!next) return
      setBackground(next)
      setAllTerminalBackgroundTransparency(Boolean(next.assetId) || next.applyToPage)
    }
    window.addEventListener('caw:customization-updated', onCustomizationUpdated)
    return () => window.removeEventListener('caw:customization-updated', onCustomizationUpdated)
  }, [])

  useEffect(() => {
    const el = elRef.current
    if (!el) return
    let cancelled = false
    let inst: TerminalInstance | null = null
    // Each attach gets its own first-paint recovery pass.
    firstPaintRef.current = true

    const forceResizeTimers: ReturnType<typeof setTimeout>[] = []
    const scrollSubs: { dispose: () => void }[] = []
    let jumpRaf = 0
    const checkJump = () => {
      if (jumpRaf) return
      jumpRaf = requestAnimationFrame(() => {
        jumpRaf = 0
        if (cancelled || !inst) return
        const buf = inst.term.buffer.active
        setShowJump(buf.type === 'normal' && buf.baseY - buf.viewportY > JUMP_TO_BOTTOM_MIN_ROWS)
      })
    }

    const flushResize = (force = false) => {
      fitTimerRef.current = null
      if (cancelled || !inst) return
      // xterm.js resize() reflows the buffer synchronously. While the
      // terminal is still parsing a write (scrollback replay on re-attach,
      // or a batched output flush from a fresh connect), that reflow runs
      // against a half-parsed buffer and corrupts the scrollback + scrollbar
      // (the artifacts seen when returning to a terminal). Defer the fit
      // and retry once the buffer is quiescent.
      if (isTerminalReplaying(terminalId)) {
        fitTimerRef.current = setTimeout(() => flushResize(force), 100)
        return
      }
      // First-paint recovery: the registry's replay-end repaint is one-shot
      // and can fire before this freshly-mounted panel is rasterized (new
      // tab / workspace switch), leaving a stale frame that the unchanged-
      // dims short-circuit below never re-paints. Force one full-viewport
      // repaint on the first flushResize that proceeds after attach — after
      // the double-rAF / staggered timers, so the layout has settled and
      // the repaint hits a live, sized canvas. Idempotent with the
      // replay-end refresh; a no-op when that refresh already landed.
      if (firstPaintRef.current) {
        firstPaintRef.current = false
        try { inst.term.refresh(0, inst.term.rows - 1) } catch { /* ignore */ }
      }
      // A fresh fit supersedes any resize that was deferred while the
      // terminal was busy replaying; if the backend still wants a different
      // grid it will echo the authoritative size via a new resize message.
      inst._pendingResize = null
      try {
        // Wrap the reflow + renderer resize in DEC private mode 2026
        // (Synchronized Output). While enabled, the renderer buffers
        // frames and swaps the new one atomically when the mode is reset,
        // so the buffer reflow that fit()/resize() triggers doesn't
        // appear as a visible intermediate frame where content is wrapped
        // or looped to cover the in-between grid size. The mode is set
        // before fit() and cleared right after; both writes parse on the
        // next microtask, before the renderer's next animation frame, so
        // the reflow renders as a single clean swap instead of a glitchy
        // reflow-then-settle sequence.
        inst.term.write('\x1b[?2026h')
        inst.fit.fit()
        const dims = inst.fit.proposeDimensions()
        if (!dims) {
          inst.term.write('\x1b[?2026l')
          return
        }
        if (force) {
          inst.term.refresh(0, inst.term.rows - 1)
        }
        const key = `${dims.cols}x${dims.rows}`
        if (!force && key === lastDimsRef.current) {
          inst.term.write('\x1b[?2026l')
          return
        }
        lastDimsRef.current = key
        if (inst.ws?.readyState === WebSocket.OPEN) {
          inst.ws.send(JSON.stringify({ type: 'resize', cols: dims.cols, rows: dims.rows, force }))
        }
        inst.term.write('\x1b[?2026l')
      } catch { /* ignore */ }
    }
    reloadRef.current = () => flushResize(true)

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
          // Returning to a backgrounded window/tab can leave the xterm.js
          // DOM renderer's viewport in a stale state (the browser throttles
          // / skips paints for hidden documents, and the canvas-backed
          // renderers can lose their GPU context). The terminal buffer is
          // intact, so a full viewport refresh re-renders the current frame
          // from the buffer and clears the garbled frame without needing a
          // manual resize. The backend's first-resize SIGWINCH (fired by
          // the reconnect below or by flushResize) makes the TUI program
          // redraw too, but this xterm refresh covers the case where the
          // PTY dims are unchanged and no SIGWINCH would otherwise fire.
          try {
            t.term.refresh(0, t.term.rows - 1)
          } catch { /* ignore */ }
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
      inst = await attachTerminal(terminalId, el, cwdRef.current, stableCmd, env)
      if (cancelled) return

      if (isActiveRef.current) {
        inst.term.focus()
      }

      scrollSubs.push(
        inst.term.onScroll(checkJump),
        inst.term.onWriteParsed(checkJump),
        inst.term.buffer.onBufferChange(checkJump),
      )
      checkJump()

      const ro = new ResizeObserver(() => {
        if (fitTimerRef.current) clearTimeout(fitTimerRef.current)
        // Coalesce rapid resizes (e.g. dragging a panel separator or the
        // browser window edge). The observer fires for every intermediate
        // size; reflowing the xterm buffer at each one is what causes the
        // wrapping/looping glitch during a resize. If the previous resize
        // fired less than 250ms ago we're in an active drag, so keep
        // extending the debounce until the motion settles — the terminal
        // stays at its current grid (the container's background fills the
        // newly-gained area) and only reflows once at the final size.
        const now = Date.now()
        const dragging = now - lastResizeAtRef.current < 250
        lastResizeAtRef.current = now
        fitTimerRef.current = setTimeout(flushResize, dragging ? 150 : 80)
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

      // Re-attach / page reload case: when reopening an agent (e.g. by
      // reloading the page or returning from another workspace) the terminal
      // container is mounted into a panel whose CSS transitions and flex
      // sizing settle *after* the double-rAF above — sometimes only after
      // several hundred milliseconds. The ResizeObserver fires while these
      // are still animating, so proposeDimensions reads the in-between
      // dimensions and the terminal only renders a partial grid with large
      // empty/black areas until something else (toggling a sidebar, making
      // the panel a touch wider) nudges a real resize. To make the terminal
      // reliably fill its container on every reload, schedule a few
      // staggered re-fits past the common transition durations. Each is
      // cheap (a no-op if the dimensions haven't changed) and the last one
      // catches the fully settled layout.
      const FORCE_RESIZE_DELAYS_MS = [250, 500, 1000]
      for (const delay of FORCE_RESIZE_DELAYS_MS) {
        const t = setTimeout(() => { flushResize() }, delay)
        forceResizeTimers.push(t)
      }
    })()

    return () => {
      cancelled = true
      reloadRef.current = () => {}
      document.removeEventListener('visibilitychange', onVisibility)
      document.removeEventListener('pageshow', onVisibility)
      window.removeEventListener('focus', onVisibility)
      el.removeEventListener('mousedown', handleRightClickMousedown, true)
      el.removeEventListener('mouseup', handleRightClickMousedown, true)
      if (fitTimerRef.current) {
        clearTimeout(fitTimerRef.current)
        fitTimerRef.current = null
      }
      for (const t of forceResizeTimers) clearTimeout(t)
      forceResizeTimers.length = 0
      for (const d of scrollSubs) d.dispose()
      scrollSubs.length = 0
      if (jumpRaf) cancelAnimationFrame(jumpRaf)
      setShowJump(false)
      resizeObsRef.current?.disconnect()
      resizeObsRef.current = null
      // Desktop parks non-active terminals: the live xterm.js instance is
      // moved into an off-screen parking lot (WS stays open, output keeps
      // flowing into its buffer) so switching back re-attaches instantly with
      // no ring-buffer replay / backend scrollback replay. Terminals parked
      // past the configured limit are demoted to the plain detach behavior.
      // Mobile renders only the selected terminal, so switching terminals must
      // fully release the previous one — closing the WS without killing
      // the backend PTY — so the backend's smallest-viewer resize can grow
      // the PTY back to desktop size when the mobile viewer drops off.
      if (window.innerWidth < 768) {
        releaseTerminal(terminalId)
      } else {
        parkTerminal(terminalId)
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
        if (open) el.classList.add('xterm-keyboard-open')
        else el.classList.remove('xterm-keyboard-open')
      }
    }
    vv.addEventListener('resize', onResize)
    vv.addEventListener('scroll', onResize)
    return () => {
      vv.removeEventListener('resize', onResize)
      vv.removeEventListener('scroll', onResize)
      if (el) el.classList.remove('xterm-keyboard-open')
    }
  }, [])

  // Touch scrolling (phones/tablets): pixel-smooth drag with momentum for
  // the scrollback, wheel notches for TUIs. See touchScroll.ts. Like the
  // desktop wheel below, scrolling up pins the view (incoming output can't
  // yank it back) until the user returns to the bottom, taps "jump to
  // bottom" or types.
  useEffect(() => {
    const el = elRef.current
    if (!el) return
    const detach = attachTouchScroll(el, {
      getTerm: () => getTerminal(terminalId)?.term,
      isEnabled: () => !keyboardOpenRef.current,
      onScrollStart: () => setTerminalUserScrolling(terminalId, true),
      onScrollEnd: (atBottom) => setTerminalUserScrolling(terminalId, !atBottom),
    })
    return () => {
      detach()
      setTerminalUserScrolling(terminalId, false)
    }
  }, [terminalId])

  // Desktop wheel/trackpad scroll: once the user scrolls up, keep the view
  // pinned there — incoming output must not yank it back to the bottom. The
  // flag is cleared (auto-follow resumes) only when the user scrolls back
  // down to the bottom or sends input (see wireInput in terminalRegistry).
  // We only observe the wheel event (passive, no preventDefault): xterm.js
  // v6's own handler on .xterm-scrollable-element does the actual scrolling,
  // and safeScrollToBottom already respects userScrolling.
  //
  // The at-bottom check uses xterm's buffer API (ydisp === ybase) rather than
  // DOM scroll measurements. In xterm.js v6 the .xterm-scrollable-element is
  // the real scroller while .xterm-viewport is a legacy element whose
  // scrollTop/scrollHeight don't reflect the actual scroll position, so
  // measuring it would always report "at bottom" and prematurely clear the
  // pin — causing the view to jump to the bottom on the second scroll-up.
  useEffect(() => {
    const el = elRef.current
    if (!el) return

    const onWheel = (e: WheelEvent) => {
      if (e.deltaY < 0) {
        // Scrolling up: pin the view so new output can't push us to the bottom.
        setTerminalUserScrolling(terminalId, true)
        return
      }
      // Scrolling down: resume auto-follow only once the buffer reaches the
      // bottom. Defer to the next frame so xterm.js has applied the wheel
      // delta to the buffer before we measure ydisp/ybase.
      requestAnimationFrame(() => {
        const inst = getTerminal(terminalId)
        if (!inst) {
          setTerminalUserScrolling(terminalId, true)
          return
        }
        if (isTerminalAtBottom(inst.term)) {
          setTerminalUserScrolling(terminalId, false)
        }
      })
    }

    el.addEventListener('wheel', onWheel, { passive: true })
    return () => {
      el.removeEventListener('wheel', onWheel)
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

  const handleJumpToBottom = () => {
    const t = getTerminal(terminalId)
    if (!t) return
    t.term.scrollToBottom()
    setTerminalUserScrolling(terminalId, false)
    setShowJump(false)
  }

  const handleReload = (e: React.MouseEvent) => {
    e.stopPropagation()
    setContextMenu(null)
    reloadRef.current()
  }

  return (
    <div
      className="relative h-full w-full overflow-hidden custom-context-menu"
      onContextMenu={handleContextMenu}
      data-testid={`terminal-panel-${terminalId}`}
    >
      {background.assetId && !background.applyToPage && (
        <div className="absolute inset-0 overflow-hidden pointer-events-none" aria-hidden="true">
          <video src={`/api/terminal/background-assets/${encodeURIComponent(background.assetId)}/content`} className="absolute inset-0 h-full w-full object-cover" autoPlay muted loop playsInline style={{ filter: `blur(${background.blur}px)`, transform: background.blur ? 'scale(1.04)' : undefined, willChange: 'filter' }} />
          <img src={`/api/terminal/background-assets/${encodeURIComponent(background.assetId)}/content`} className="absolute inset-0 h-full w-full object-cover" style={{ filter: `blur(${background.blur}px)`, transform: background.blur ? 'scale(1.04)' : undefined, willChange: 'filter' }} />
          <div className="absolute inset-0 bg-background" style={{ opacity: background.overlay }} />
        </div>
      )}
      <div ref={elRef} className="relative z-[1] h-full w-full overflow-hidden" style={{ backgroundColor: background.assetId || background.applyToPage ? 'transparent' : getTerminalBackground() }} />
      {showJump && (
        <button
          type="button"
          onClick={handleJumpToBottom}
          // Don't steal focus from the terminal (or pop the soft keyboard).
          onMouseDown={(e) => e.preventDefault()}
          onContextMenu={(e) => e.stopPropagation()}
          aria-label="Jump to bottom"
          title="Jump to bottom"
          data-testid={`terminal-jump-bottom-${terminalId}`}
          className="absolute bottom-3 right-3 z-[2] flex h-9 w-9 items-center justify-center rounded-full border border-border bg-secondary/85 text-foreground/80 shadow-md backdrop-blur-sm transition-colors hover:bg-accent hover:text-foreground cursor-pointer"
        >
          <ArrowDown size={16} />
        </button>
      )}
      {contextMenu && (
        <SmartContextMenu x={contextMenu.x} y={contextMenu.y} ref={contextMenuRef}>
          <button
            onClick={handleReload}
            className="flex items-center w-full px-3 py-1.5 text-xs text-foreground/80 hover:bg-accent hover:text-foreground text-left cursor-pointer gap-2"
          >
            <RotateCw size={14} />
            <span>Reload</span>
          </button>
          <div className="my-0.5 border-t border-border" />
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
          <div className="my-0.5 border-t border-border" />
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
          <div className="my-0.5 border-t border-border" />
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
