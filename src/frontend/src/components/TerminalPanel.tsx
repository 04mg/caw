import { useEffect, useRef } from 'react'
import { attachTerminal, detachTerminal, getTerminal, type TerminalInstance } from '@/lib/terminalRegistry'

interface TerminalPanelProps {
  terminalId: string
  cwd: string
  cmd?: string[]
  isActive?: boolean
}

export function TerminalPanel({ terminalId, cwd, cmd, isActive }: TerminalPanelProps) {
  const elRef = useRef<HTMLDivElement>(null)
  const resizeObsRef = useRef<ResizeObserver | null>(null)
  const fitTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const lastDimsRef = useRef<string>('')
  const isActiveRef = useRef(isActive)
  isActiveRef.current = isActive

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

    const onVisibility = () => {
      if (document.visibilityState === 'visible') {
        flushResize()
      }
    }
    document.addEventListener('visibilitychange', onVisibility)

    ;(async () => {
      inst = await attachTerminal(terminalId, el, cwd, cmd)
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
      if (fitTimerRef.current) {
        clearTimeout(fitTimerRef.current)
        fitTimerRef.current = null
      }
      resizeObsRef.current?.disconnect()
      resizeObsRef.current = null
      detachTerminal(terminalId)
    }
  }, [terminalId, cwd, cmd])

  useEffect(() => {
    if (!isActive) return
    const inst = getTerminal(terminalId)
    if (inst) {
      inst.term.focus()
    }
  }, [isActive, terminalId])

  return (
    <div className="relative h-full w-full overflow-hidden">
      <div ref={elRef} className="h-full w-full overflow-hidden" />
    </div>
  )
}