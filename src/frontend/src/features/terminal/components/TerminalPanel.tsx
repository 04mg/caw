import { useEffect, useMemo, useRef, useState } from 'react'
import { Copy, Clipboard } from 'lucide-react'
import { attachTerminal, detachTerminal, getTerminal, reconnectTerminalWs, setTerminalUserScrolling, type TerminalInstance } from '@/features/terminal/services/terminalRegistry'
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
      detachTerminal(terminalId)
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

    const dispatchWheel = (deltaY: number, clientX: number, clientY: number) => {
      const target = el.querySelector('.xterm-viewport') || el
      if (!target) return
      accumDelta += deltaY
      const wholeLines = Math.trunc(accumDelta)
      if (wholeLines === 0) return
      accumDelta -= wholeLines
      target.dispatchEvent(new WheelEvent('wheel', {
        deltaY: wholeLines,
        deltaMode: WheelEvent.DOM_DELTA_LINE,
        bubbles: true,
        cancelable: true,
        clientX,
        clientY,
      }))
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
      setTerminalUserScrolling(terminalId, true)
    }

    const onTouchMove = (e: TouchEvent) => {
      if (!active || e.touches.length !== 1) return
      e.preventDefault()
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

    const onTouchEnd = () => {
      if (!active) return
      active = false
      if (Math.abs(velocity) >= VELOCITY_THRESHOLD) {
        rafId = requestAnimationFrame(momentum)
      } else {
        beginGrace()
      }
    }

    el.addEventListener('touchstart', onTouchStart, { passive: true })
    el.addEventListener('touchmove', onTouchMove, { passive: false })
    el.addEventListener('touchend', onTouchEnd, { passive: true })
    el.addEventListener('touchcancel', onTouchEnd, { passive: true })

    return () => {
      if (rafId) cancelAnimationFrame(rafId)
      if (graceTimer) clearTimeout(graceTimer)
      el.removeEventListener('touchstart', onTouchStart)
      el.removeEventListener('touchmove', onTouchMove)
      el.removeEventListener('touchend', onTouchEnd)
      el.removeEventListener('touchcancel', onTouchEnd)
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
      <div ref={elRef} className="h-full w-full overflow-hidden" />
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