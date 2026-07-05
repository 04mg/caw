import { useEffect, useRef } from 'react'
import { attachTerminal, detachTerminal, type TerminalInstance } from '@/lib/terminalRegistry'

interface TerminalPanelProps {
  terminalId: string
  cwd: string
  cmd?: string[]
}

export function TerminalPanel({ terminalId, cwd, cmd }: TerminalPanelProps) {
  const elRef = useRef<HTMLDivElement>(null)
  const resizeObsRef = useRef<ResizeObserver | null>(null)
  const fitTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const lastDimsRef = useRef<string>('')

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
      if (fitTimerRef.current) {
        clearTimeout(fitTimerRef.current)
        fitTimerRef.current = null
      }
      resizeObsRef.current?.disconnect()
      resizeObsRef.current = null
      detachTerminal(terminalId)
    }
  }, [terminalId, cwd, cmd])

  return (
    <div className="relative h-full w-full overflow-hidden">
      <div ref={elRef} className="h-full w-full overflow-hidden" />
    </div>
  )
}