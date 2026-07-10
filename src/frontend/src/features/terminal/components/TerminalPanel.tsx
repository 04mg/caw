import { useEffect, useRef, useState } from 'react'
import { Copy, Clipboard } from 'lucide-react'
import { attachTerminal, detachTerminal, getTerminal, type TerminalInstance } from '@/features/terminal/services/terminalRegistry'
import { SmartContextMenu } from '@/features/explorer/components/SmartContextMenu'

interface TerminalPanelProps {
  terminalId: string
  cwd: string
  cmd?: string[]
  isActive?: boolean
}

export function TerminalPanel({ terminalId, cwd, cmd, isActive }: TerminalPanelProps) {
  const elRef = useRef<HTMLDivElement>(null)
  const contextMenuRef = useRef<HTMLDivElement>(null)
  const resizeObsRef = useRef<ResizeObserver | null>(null)
  const fitTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const lastDimsRef = useRef<string>('')
  const isActiveRef = useRef(isActive)
  isActiveRef.current = isActive

  const [contextMenu, setContextMenu] = useState<{ x: number; y: number } | null>(null)

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

  useEffect(() => {
    if (!contextMenu) return
    const handleClose = () => setContextMenu(null)
    document.addEventListener('mousedown', handleClose)
    document.addEventListener('click', handleClose)
    return () => {
      document.removeEventListener('mousedown', handleClose)
      document.removeEventListener('click', handleClose)
    }
  }, [contextMenu])

  const handleContextMenu = (e: React.MouseEvent) => {
    e.preventDefault()
    setContextMenu({ x: e.clientX, y: e.clientY })
  }

  const inst = getTerminal(terminalId)
  const hasSelection = inst ? inst.term.hasSelection() : false

  const handleCopy = (e: React.MouseEvent) => {
    e.stopPropagation()
    if (inst) {
      const text = inst.term.getSelection()
      if (text) {
        navigator.clipboard.writeText(text)
      }
    }
    setContextMenu(null)
  }

  const handlePaste = async (e: React.MouseEvent) => {
    e.stopPropagation()
    if (inst) {
      try {
        const text = await navigator.clipboard.readText()
        if (text && inst.ws?.readyState === WebSocket.OPEN) {
          inst.ws.send(JSON.stringify({ type: 'input', data: text }))
        }
      } catch (err) {
        console.error('Failed to read from clipboard:', err)
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
      {contextMenu && (
        <SmartContextMenu x={contextMenu.x} y={contextMenu.y} ref={contextMenuRef}>
          <button
            onClick={handleCopy}
            disabled={!hasSelection}
            className={`flex items-center w-full px-3 py-1.5 text-xs text-left cursor-pointer gap-2 ${
              hasSelection ? 'text-foreground/80 hover:bg-accent hover:text-foreground' : 'text-foreground/30 cursor-not-allowed'
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