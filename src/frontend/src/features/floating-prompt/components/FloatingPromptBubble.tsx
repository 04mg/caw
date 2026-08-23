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
const MARGIN = 8

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
  const [isDragging, setIsDragging] = useState(false)

  // Measure the bubble after it renders / text changes so we can clamp.
  useLayoutEffect(() => {
    const el = bubbleRef.current
    if (!el) return
    const r = el.getBoundingClientRect()
    setSize({ w: Math.ceil(r.width), h: Math.ceil(r.height) })
  }, [text, open, showHistory])

  // Focus the textarea when the bubble opens and place the caret at the end.
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

  // Close history dropdown when clicking outside.
  useEffect(() => {
    if (!showHistory) return
    const onDown = (e: MouseEvent) => {
      if (bubbleRef.current?.contains(e.target as Node)) return
      setShowHistory(false)
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [showHistory])

  // Full composite bounds: the bubble plus a button row (and gap) on each
  // vertical side. Used to guarantee everything stays inside the viewport.
  const compositeBounds = useCallback(
    (w: number, h: number) => ({
      fullW: Math.max(w, BTN_SIZE * 2 + BTN_GAP),
      fullH: h + BTN_SIZE * 2 + ROW_GAP * 2,
    }),
    [],
  )

  // Auto-computed position next to the cursor. Always resolves to a spot
  // where the ENTIRE composite (buttons + bubble) is fully visible.
  const autoPos = useMemo(() => {
    const { fullW, fullH } = compositeBounds(size.w, size.h)
    const vw = window.innerWidth
    const vh = window.innerHeight

    const minY = MARGIN
    const maxY = Math.max(minY, vh - MARGIN - fullH)
    const minX = MARGIN
    const maxX = Math.max(minX, vw - MARGIN - fullW)

    // Preferred: below-right of the cursor; fall back to above-left.
    let y = mouse.y + offset
    const aboveY = mouse.y - offset - fullH
    const flip = y + fullH > vh - MARGIN && aboveY >= minY
    if (flip) y = aboveY

    let x = mouse.x + offset
    if (x + fullW > vw - MARGIN) x = mouse.x - offset - fullW

    // Hard clamp: the whole composite must be fully on screen.
    y = Math.min(Math.max(y, minY), maxY)
    x = Math.min(Math.max(x, minX), maxX)

    return { x, y, flip }
  }, [mouse, offset, size, compositeBounds])

  // Clamp an arbitrary absolute position so the composite stays visible.
  const clampToViewport = useCallback(
    (x: number, y: number): FloatingPromptPosition => {
      const { fullW, fullH } = compositeBounds(size.w, size.h)
      const vw = window.innerWidth
      const vh = window.innerHeight
      const cx = Math.min(Math.max(x, MARGIN), Math.max(MARGIN, vw - MARGIN - fullW))
      const cy = Math.min(Math.max(y, MARGIN), Math.max(MARGIN, vh - MARGIN - fullH))
      return { x: cx, y: cy }
    },
    [size, compositeBounds],
  )

  // The effective position: pinned (dragged) wins over auto.
  const effectivePos = pinnedPos ?? { x: autoPos.x, y: autoPos.y }

  // Drag: attach window listeners imperatively on mousedown so the drag
  // starts immediately, independent of React effect timing. Every move pins
  // the live position in the hook, so it persists after release.
  const startDrag = (e: React.MouseEvent) => {
    if (e.button !== 0) return
    const target = e.target as HTMLElement
    if (target.closest('button, textarea, a, input, [role="button"]')) return
    e.preventDefault()

    const startX = e.clientX
    const startY = e.clientY
    const originX = effectivePos.x
    const originY = effectivePos.y
    let lastPos: FloatingPromptPosition = { x: originX, y: originY }

    const onMove = (ev: MouseEvent) => {
      lastPos = clampToViewport(originX + ev.clientX - startX, originY + ev.clientY - startY)
      onPinPosition(lastPos)
    }
    const onUp = () => {
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup', onUp)
      onPinPosition(lastPos)
      setIsDragging(false)
    }
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
    setIsDragging(true)
  }

  const topButtons = (
    <div className={cn('flex items-center', isDragging ? 'cursor-grabbing' : 'cursor-grab')} style={{ gap: BTN_GAP }} onMouseDown={startDrag}>
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
    <div className={cn('flex items-center', isDragging ? 'cursor-grabbing' : 'cursor-grab')} style={{ gap: BTN_GAP }} onMouseDown={startDrag}>
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
        'relative rounded-xl border border-border/70 bg-secondary/90 backdrop-blur-md shadow-xl',
        isDragging ? 'cursor-grabbing' : 'cursor-grab',
      )}
      style={{ minWidth: BUBBLE_MIN_W, maxWidth: BUBBLE_MAX_W }}
      onMouseDown={startDrag}
    >
      <textarea
        ref={taRef}
        value={text}
        onChange={(e) => onTextChange(e.target.value)}
        onKeyDown={(e) => {
          // Enter => newline (default textarea behavior). Stop propagation
          // so the global listener doesn't react. Escape is handled globally.
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
          <div className={cn('flex flex-col items-start pointer-events-auto', isDragging ? 'cursor-grabbing' : 'cursor-grab')} style={{ gap: ROW_GAP }}>
            {autoPos.flip ? (
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