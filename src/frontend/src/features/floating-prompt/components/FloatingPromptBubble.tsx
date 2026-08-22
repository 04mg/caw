import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import { SendHorizonal, X, History, Trash2 } from 'lucide-react'
import { cn } from '@/features/shared/utils/utils'
import { CircleButton } from './CircleButton'
import type { FloatingPromptPosition } from '@/features/floating-prompt/hooks/useFloatingPrompt'

interface FloatingPromptBubbleProps {
  open: boolean
  text: string
  mouse: { x: number; y: number }
  offset: number
  pinnedPos: FloatingPromptPosition | null
  history: string[]
  canSend: boolean
  onTextChange: (text: string) => void
  onClose: () => void
  onSend: () => void
  onInsertFromHistory: (item: string) => void
  onClearHistory: () => void
  onPinPosition: (pos: FloatingPromptPosition) => void
}

const BUBBLE_MIN_W = 200
const BUBBLE_MAX_W = 320
const BUBBLE_MIN_H = 44
const BTN_SIZE = 26
const BTN_GAP = 6
const ROW_GAP = 4

export function FloatingPromptBubble({
  open,
  text,
  mouse,
  offset,
  pinnedPos,
  history,
  canSend,
  onTextChange,
  onClose,
  onSend,
  onInsertFromHistory,
  onClearHistory,
  onPinPosition,
}: FloatingPromptBubbleProps) {
  const taRef = useRef<HTMLTextAreaElement>(null)
  const bubbleRef = useRef<HTMLDivElement>(null)
  const [size, setSize] = useState({ w: BUBBLE_MIN_W, h: BUBBLE_MIN_H })
  const [showHistory, setShowHistory] = useState(false)
  const [flipped, setFlipped] = useState(false)
  // While dragging, the live position is driven here. On mouseup we commit
  // it to the hook via onPinPosition so it persists.
  const [dragPos, setDragPos] = useState<{ x: number; y: number } | null>(null)
  const dragStartRef = useRef<{ mx: number; my: number; originX: number; originY: number } | null>(null)

  useLayoutEffect(() => {
    const el = bubbleRef.current
    if (!el) return
    const r = el.getBoundingClientRect()
    setSize({ w: Math.ceil(r.width), h: Math.ceil(r.height) })
  }, [text, open, showHistory])

  useEffect(() => {
    if (open && taRef.current) {
      const ta = taRef.current
      requestAnimationFrame(() => {
        ta.focus()
        const len = ta.value.length
        ta.setSelectionRange(len, len)
      })
    }
  }, [open])

  useEffect(() => {
    if (!showHistory) return
    const onDown = (e: MouseEvent) => {
      if (bubbleRef.current?.contains(e.target as Node)) return
      setShowHistory(false)
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [showHistory])

  // Auto-computed position (used when no drag and no pin).
  const autoPos = useMemo(() => {
    const topRowW = 2 * BTN_SIZE + BTN_GAP
    const bottomRowW = BTN_SIZE
    const rowH = BTN_SIZE
    const maxRowW = Math.max(topRowW, bottomRowW)
    const totalW = Math.max(size.w, maxRowW)

    const vw = window.innerWidth
    const vh = window.innerHeight

    let x = mouse.x + offset
    let y = mouse.y + offset

    if (x + totalW > vw - 8) {
      x = mouse.x - offset - totalW
    }
    if (x < 8) x = 8

    const bottomRowBottom = y + size.h + ROW_GAP + rowH
    let flip = false
    if (bottomRowBottom > vh - 8) {
      flip = true
    }
    const topRowTop = y - ROW_GAP - rowH
    if (!flip && topRowTop < 8) {
      flip = true
    }

    if (flip) {
      const allAboveH = rowH * 2 + ROW_GAP * 2
      if (y + size.h > vh - 8) y = vh - 8 - size.h
      if (y - allAboveH < 8) y = 8 + allAboveH
    } else {
      if (y < 8) y = 8
      if (y + size.h + ROW_GAP + rowH > vh - 8) {
        y = vh - 8 - size.h - ROW_GAP - rowH
      }
    }

    return { x, y, flip }
  }, [mouse, offset, size])

  useEffect(() => {
    setFlipped(autoPos.flip)
  }, [autoPos.flip])

  // The effective position: drag > pinned > auto.
  const effectivePos = dragPos ?? pinnedPos ?? { x: autoPos.x, y: autoPos.y }

  // Clamp an absolute position into the viewport.
  const clampPos = useCallback((x: number, y: number) => {
    const vw = window.innerWidth
    const vh = window.innerHeight
    const rowH = BTN_SIZE
    const cx = Math.max(8, Math.min(x, vw - size.w - 8))
    const cy = Math.max(8 + rowH * 2 + ROW_GAP * 2, Math.min(y, vh - size.h - 8))
    return { x: cx, y: cy }
  }, [size])

  // Drag machinery: mousedown anywhere on the container (except interactive
  // elements) starts a drag. mousemove updates dragPos; mouseup commits it
  // to the hook so it persists.
  useEffect(() => {
    if (!dragStartRef.current) return
    const onMove = (e: MouseEvent) => {
      const s = dragStartRef.current
      if (!s) return
      const nx = s.originX + (e.clientX - s.mx)
      const ny = s.originY + (e.clientY - s.my)
      setDragPos(clampPos(nx, ny))
    }
    const onUp = () => {
      // Commit the last dragPos to the hook so it persists after release.
      setDragPos((dp) => {
        if (dp) onPinPosition(dp)
        return null
      })
      dragStartRef.current = null
    }
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
    return () => {
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup', onUp)
    }
  }, [clampPos, onPinPosition])

  const startDrag = (e: React.MouseEvent) => {
    // Don't drag when clicking interactive elements.
    const target = e.target as HTMLElement
    if (target.closest('button, textarea, a, [role="button"], input')) return
    e.preventDefault()
    const origin = effectivePos
    dragStartRef.current = { mx: e.clientX, my: e.clientY, originX: origin.x, originY: origin.y }
    setDragPos({ x: origin.x, y: origin.y })
  }

  const isDragging = dragStartRef.current !== null

  const topButtons = (
    <div
      className={cn('flex items-center cursor-grab', isDragging && 'cursor-grabbing')}
      style={{ gap: BTN_GAP }}
      onMouseDown={startDrag}
    >
      <CircleButton
        label="History"
        disabled={history.length === 0}
        active={showHistory}
        onClick={() => setShowHistory((v) => !v)}
      >
        <History className="h-3.5 w-3.5" />
      </CircleButton>
      <CircleButton label="Close" onClick={onClose}>
        <X className="h-3.5 w-3.5" />
      </CircleButton>
    </div>
  )

  const bottomButtons = (
    <div
      className={cn('flex items-center cursor-grab', isDragging && 'cursor-grabbing')}
      style={{ gap: BTN_GAP }}
      onMouseDown={startDrag}
    >
      <CircleButton
        label="Send"
        disabled={!canSend || text.trim().length === 0}
        highlight={canSend && text.trim().length > 0}
        onClick={onSend}
      >
        <SendHorizonal className="h-3.5 w-3.5" />
      </CircleButton>
    </div>
  )

  const bubble = (
    <div
      ref={bubbleRef}
      data-floating-prompt
      className={cn(
        'relative rounded-xl border border-border/70 bg-secondary/90 backdrop-blur-md shadow-xl cursor-grab',
        isDragging && 'cursor-grabbing',
      )}
      style={{ minWidth: BUBBLE_MIN_W, maxWidth: BUBBLE_MAX_W }}
      onMouseDown={(e) => {
        e.stopPropagation()
        startDrag(e)
      }}
    >
      <textarea
        ref={taRef}
        value={text}
        onChange={(e) => onTextChange(e.target.value)}
        onKeyDown={(e) => {
          e.stopPropagation()
          if (e.key === 'Escape') {
            e.preventDefault()
            onClose()
          }
        }}
        onKeyUp={(e) => e.stopPropagation()}
        onKeyPress={(e) => e.stopPropagation()}
        placeholder="Type a prompt…"
        rows={1}
        className={cn(
          'block w-full resize-none bg-transparent px-2.5 py-2 text-xs leading-relaxed text-foreground',
          'placeholder:text-muted-foreground/60 focus:outline-none',
          'max-h-40 overflow-y-auto scrollbar-none',
        )}
        style={{ minHeight: BUBBLE_MIN_H }}
      />

      {showHistory && history.length > 0 && (
        <div className="absolute left-0 right-0 top-full z-10 mt-1 max-h-48 overflow-y-auto rounded-lg border border-border/70 bg-popover/95 backdrop-blur-md shadow-lg">
          <div className="flex items-center justify-between px-2 py-1 border-b border-border/50">
            <span className="text-[10px] font-medium text-muted-foreground uppercase tracking-wide">History</span>
            <button
              onClick={onClearHistory}
              className="text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
              title="Clear history"
            >
              <Trash2 className="h-3 w-3" />
            </button>
          </div>
          {history.map((item, i) => (
            <button
              key={`${i}-${item.slice(0, 20)}`}
              onClick={() => {
                onInsertFromHistory(item)
                setShowHistory(false)
              }}
              className="block w-full px-2 py-1.5 text-left text-[11px] text-foreground/80 hover:bg-accent hover:text-foreground transition-colors whitespace-pre-wrap break-words"
            >
              {item}
            </button>
          ))}
        </div>
      )}
    </div>
  )

  return (
    <AnimatePresence>
      {open && (
        <motion.div
          className="fixed z-[60] pointer-events-none"
          style={{ left: effectivePos.x, top: effectivePos.y }}
          initial={{ opacity: 0, scale: 0.92 }}
          animate={{ opacity: 1, scale: 1 }}
          exit={{ opacity: 0, scale: 0.92 }}
          transition={{ duration: 0.12 }}
        >
          <div className="flex flex-col items-start pointer-events-auto" style={{ gap: ROW_GAP }}>
            {flipped ? (
              <>
                {bottomButtons}
                {topButtons}
                {bubble}
              </>
            ) : (
              <>
                {topButtons}
                {bubble}
                {bottomButtons}
              </>
            )}
          </div>
        </motion.div>
      )}
    </AnimatePresence>
  )
}