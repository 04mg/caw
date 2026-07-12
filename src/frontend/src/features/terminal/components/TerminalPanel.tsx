import { useEffect, useMemo, useRef, useState } from 'react'
import { Copy, Clipboard } from 'lucide-react'
import { attachTerminal, detachTerminal, getTerminal, setTerminalUserScrolling, type TerminalInstance } from '@/features/terminal/services/terminalRegistry'
import { SmartContextMenu } from '@/features/explorer/components/SmartContextMenu'

interface TerminalPanelProps {
  terminalId: string
  cwd: string
  cmd?: string[]
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
  } catch (err) {
    document.body.removeChild(textArea)
    return false
  }
}

export function TerminalPanel({ terminalId, cwd, cmd, isActive }: TerminalPanelProps) {
  const elRef = useRef<HTMLDivElement>(null)
  const contextMenuRef = useRef<HTMLDivElement>(null)
  const resizeObsRef = useRef<ResizeObserver | null>(null)
  const fitTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const lastDimsRef = useRef<string>('')
  const isActiveRef = useRef(isActive)
  isActiveRef.current = isActive
  const scrollBarRef = useRef<HTMLDivElement>(null)
  const scrollThumbRef = useRef<HTMLDivElement>(null)
  const scrollDragRef = useRef<{ startY: number; startThumbTop: number } | null>(null)

  const cmdKey = useMemo(() => JSON.stringify(cmd ?? []), [cmd])
  const stableCmd = useMemo(() => cmd, [cmdKey])
  const cwdRef = useRef(cwd)
  cwdRef.current = cwd

  const [contextMenu, setContextMenu] = useState<{ x: number; y: number } | null>(null)
  const [savedSelection, setSavedSelection] = useState('')

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
      }
    }
    document.addEventListener('visibilitychange', onVisibility)

    ;(async () => {
      inst = await attachTerminal(terminalId, el, cwdRef.current, stableCmd)
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

      // After a tab switch the panel remounts and react-resizable-panels
      // applies its final sizes a few frames later. attachTerminal already
      // ran fit() but against a not-yet-laid-out container, so the xterm
      // canvas renders the wrong cols/rows. Schedule deferred re-fits to
      // correct the display once layout settles, even if the element's
      // final size equals the initial one (which would keep the
      // ResizeObserver from firing).
      setTimeout(flushResize, 120)
      setTimeout(flushResize, 300)
    })()

    return () => {
      cancelled = true
      document.removeEventListener('visibilitychange', onVisibility)
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
  }, [terminalId, stableCmd])

  useEffect(() => {
    if (!isActive) return
    const inst = getTerminal(terminalId)
    if (inst) {
      inst.term.focus()
    }
  }, [isActive, terminalId])

  useEffect(() => {
    const el = elRef.current
    const scrollBar = scrollBarRef.current
    const thumb = scrollThumbRef.current
    if (!el || !scrollBar || !thumb) return

    let rafId = 0

    const updateThumb = () => {
      rafId = 0
      const vp = el.querySelector('.xterm-viewport') as HTMLElement | null
      if (!vp) return
      const scrollTop = vp.scrollTop
      const scrollHeight = vp.scrollHeight
      const clientHeight = vp.clientHeight
      const barHeight = scrollBar.clientHeight
      if (scrollHeight <= clientHeight) {
        thumb.style.display = 'none'
        return
      }
      thumb.style.display = ''
      const thumbHeight = Math.max(24, (clientHeight / scrollHeight) * barHeight)
      const maxThumbTop = barHeight - thumbHeight
      const thumbTop = (scrollTop / (scrollHeight - clientHeight)) * maxThumbTop
      thumb.style.height = `${thumbHeight}px`
      thumb.style.top = `${thumbTop}px`
    }

    const scheduleUpdate = () => {
      if (rafId) return
      rafId = requestAnimationFrame(updateThumb)
    }

    const vp = el.querySelector('.xterm-viewport') as HTMLElement | null
    if (vp) {
      vp.addEventListener('scroll', scheduleUpdate, { passive: true })
    }
    const ro = new ResizeObserver(scheduleUpdate)
    ro.observe(el)
    const interval = setInterval(scheduleUpdate, 200)
    scheduleUpdate()

    return () => {
      if (rafId) cancelAnimationFrame(rafId)
      if (vp) vp.removeEventListener('scroll', scheduleUpdate)
      ro.disconnect()
      clearInterval(interval)
    }
  }, [terminalId])

  const handleScrollMouseDown = (e: React.MouseEvent) => {
    e.preventDefault()
    e.stopPropagation()
    const el = elRef.current
    const vp = el?.querySelector('.xterm-viewport') as HTMLElement | null
    if (!vp) return
    const thumb = scrollThumbRef.current
    if (!thumb) return
    scrollDragRef.current = {
      startY: e.clientY,
      startThumbTop: parseFloat(thumb.style.top || '0'),
    }

    const onMove = (ev: MouseEvent) => {
      const drag = scrollDragRef.current
      if (!drag) return
      const scrollBar = scrollBarRef.current
      if (!scrollBar) return
      const barHeight = scrollBar.clientHeight
      const thumbHeight = thumb.offsetHeight
      const maxThumbTop = barHeight - thumbHeight
      const dy = ev.clientY - drag.startY
      const newThumbTop = Math.max(0, Math.min(maxThumbTop, drag.startThumbTop + dy))
      const scrollRatio = maxThumbTop > 0 ? newThumbTop / maxThumbTop : 0
      const scrollHeight = vp.scrollHeight - vp.clientHeight
      vp.scrollTop = scrollRatio * scrollHeight
    }

    const onUp = () => {
      scrollDragRef.current = null
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup', onUp)
    }

    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
  }

  const handleScrollTouchStart = (e: React.TouchEvent) => {
    e.preventDefault()
    e.stopPropagation()
    const el = elRef.current
    const vp = el?.querySelector('.xterm-viewport') as HTMLElement | null
    if (!vp) return
    const thumb = scrollThumbRef.current
    if (!thumb) return
    const touch = e.touches[0]
    scrollDragRef.current = {
      startY: touch.clientY,
      startThumbTop: parseFloat(thumb.style.top || '0'),
    }

    const onMove = (ev: TouchEvent) => {
      const drag = scrollDragRef.current
      if (!drag || ev.touches.length !== 1) return
      const scrollBar = scrollBarRef.current
      if (!scrollBar) return
      const barHeight = scrollBar.clientHeight
      const thumbHeight = thumb.offsetHeight
      const maxThumbTop = barHeight - thumbHeight
      const dy = ev.touches[0].clientY - drag.startY
      const newThumbTop = Math.max(0, Math.min(maxThumbTop, drag.startThumbTop + dy))
      const scrollRatio = maxThumbTop > 0 ? newThumbTop / maxThumbTop : 0
      const scrollHeight = vp.scrollHeight - vp.clientHeight
      vp.scrollTop = scrollRatio * scrollHeight
    }

    const onEnd = () => {
      scrollDragRef.current = null
      window.removeEventListener('touchmove', onMove)
      window.removeEventListener('touchend', onEnd)
      window.removeEventListener('touchcancel', onEnd)
    }

    window.addEventListener('touchmove', onMove, { passive: false })
    window.addEventListener('touchend', onEnd)
    window.addEventListener('touchcancel', onEnd)
  }

  useEffect(() => {
    const el = elRef.current
    if (!el) return

    const LINES_PER_PX = parseFloat(localStorage.getItem('caw:terminalScrollSensitivity') || '0.005')
    const FRICTION = parseFloat(localStorage.getItem('caw:terminalScrollFriction') || '0.80')
    const VELOCITY_THRESHOLD = parseFloat(localStorage.getItem('caw:terminalScrollVelocityThreshold') || '0.025')
    const SCROLL_GRACE_MS = parseInt(localStorage.getItem('caw:terminalScrollGrace') || '1200', 10)

    let lastY = 0
    let lastX = 0
    let lastTime = 0
    let velocity = 0
    let active = false
    let rafId = 0
    let graceTimer: ReturnType<typeof setTimeout> | null = null

    const dispatchWheel = (deltaY: number, clientX: number, clientY: number) => {
      const target = el.querySelector('.xterm-viewport') || el
      if (!target) return
      target.dispatchEvent(new WheelEvent('wheel', {
        deltaY: deltaY,
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
    if (inst) {
      sel = inst.term.getSelection()
    }
    setSavedSelection(sel)
    setContextMenu({ x: e.clientX, y: e.clientY })
  }

  const handleCopy = (e: React.MouseEvent) => {
    e.stopPropagation()
    if (savedSelection) {
      copyToClipboard(savedSelection)
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
      } catch (err) {
        alert('Could not paste from clipboard. Please allow clipboard access or use Ctrl+V / Cmd+V directly in the terminal.')
      }
    }
    setContextMenu(null)
  }

  return (
    <div
      className="relative h-full w-full overflow-hidden custom-context-menu"
      onContextMenu={handleContextMenu}
    >
      <div ref={elRef} className="h-full w-full overflow-hidden" />
      <div
        ref={scrollBarRef}
        className="absolute top-0 right-0 bottom-0 w-2 bg-black/60 hover:bg-black/80 transition-colors cursor-pointer touch-none z-10"
        onMouseDown={handleScrollMouseDown}
        onTouchStart={handleScrollTouchStart}
      >
        <div
          ref={scrollThumbRef}
          className="absolute left-0 right-0 bg-white/25 hover:bg-white/40 rounded-sm transition-colors"
          style={{ height: '40px', top: '0px' }}
        />
      </div>
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