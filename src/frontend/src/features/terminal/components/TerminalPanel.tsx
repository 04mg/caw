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

function copyToClipboard(text: string): boolean {
  console.log('[TerminalPanel] copyToClipboard:', text)
  if (navigator.clipboard && navigator.clipboard.writeText) {
    console.log('[TerminalPanel] using navigator.clipboard.writeText')
    navigator.clipboard.writeText(text).catch((err) => {
      console.error('[TerminalPanel] navigator.clipboard error:', err)
      fallbackCopyToClipboard(text)
    })
    return true
  }
  console.log('[TerminalPanel] using fallbackCopyToClipboard')
  return fallbackCopyToClipboard(text)
}

function fallbackCopyToClipboard(text: string): boolean {
  console.log('[TerminalPanel] fallbackCopyToClipboard')
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
    console.log('[TerminalPanel] execCommand copy successful:', successful)
    document.body.removeChild(textArea)
    return successful
  } catch (err) {
    console.error('[TerminalPanel] Fallback copy failed:', err)
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
      console.log('[TerminalPanel] handleRightClickMousedown:', e.type, 'button:', e.button)
      if (e.button === 2) {
        console.log('[TerminalPanel] stopping propagation of right-click event')
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
      console.log('[TerminalPanel] handleContextMenu - active selection:', sel)
    } else {
      console.log('[TerminalPanel] handleContextMenu - no terminal instance found')
    }
    setSavedSelection(sel)
    setContextMenu({ x: e.clientX, y: e.clientY })
  }

  const handleCopy = (e: React.MouseEvent) => {
    e.stopPropagation()
    console.log('[TerminalPanel] handleCopy triggered, savedSelection:', savedSelection)
    if (savedSelection) {
      const res = copyToClipboard(savedSelection)
      console.log('[TerminalPanel] copyToClipboard result:', res)
    }
    setContextMenu(null)
  }

  const handlePaste = async (e: React.MouseEvent) => {
    e.stopPropagation()
    console.log('[TerminalPanel] handlePaste triggered')
    if (inst) {
      try {
        console.log('[TerminalPanel] checking clipboard access')
        if (!navigator.clipboard || !navigator.clipboard.readText) {
          console.warn('[TerminalPanel] navigator.clipboard.readText is undefined')
          alert('Clipboard paste is only supported in secure contexts (HTTPS/localhost). Please use Ctrl+V / Cmd+V directly in the terminal.')
          setContextMenu(null)
          return
        }
        const text = await navigator.clipboard.readText()
        console.log('[TerminalPanel] clipboard text read:', text)
        if (text && inst.ws?.readyState === WebSocket.OPEN) {
          inst.ws.send(JSON.stringify({ type: 'input', data: text }))
          console.log('[TerminalPanel] paste input sent to WebSocket')
        } else {
          console.warn('[TerminalPanel] websocket not open or no text')
        }
      } catch (err) {
        console.error('[TerminalPanel] Failed to read from clipboard:', err)
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