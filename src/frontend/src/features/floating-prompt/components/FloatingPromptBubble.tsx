import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import { SendHorizonal, X, History, Trash2 } from 'lucide-react'
import { cn } from '@/features/shared/utils/utils'
import { CircleButton } from './CircleButton'

interface FloatingPromptBubbleProps {
  open: boolean
  text: string
  mouse: { x: number; y: number }
  offset: number
  history: string[]
  canSend: boolean
  onTextChange: (text: string) => void
  onClose: () => void
  onSend: () => void
  onInsertFromHistory: (item: string) => void
  onClearHistory: () => void
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
  history,
  canSend,
  onTextChange,
  onClose,
  onSend,
  onInsertFromHistory,
  onClearHistory,
}: FloatingPromptBubbleProps) {
  const taRef = useRef<HTMLTextAreaElement>(null)
  const bubbleRef = useRef<HTMLDivElement>(null)
  const [size, setSize] = useState({ w: BUBBLE_MIN_W, h: BUBBLE_MIN_H })
  const [showHistory, setShowHistory] = useState(false)
  const [flipped, setFlipped] = useState(false) // buttons-below => buttons-above

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

  const position = useMemo(() => {
    // One button row has up to 2 buttons (History + Close) on top, 1 (Send) on bottom.
    const topRowW = 2 * BTN_SIZE + BTN_GAP
    const bottomRowW = BTN_SIZE
    const rowH = BTN_SIZE
    const maxRowW = Math.max(topRowW, bottomRowW)
    const totalW = Math.max(size.w, maxRowW)

    const vw = window.innerWidth
    const vh = window.innerHeight

    // Preferred: bubble to the right of the cursor.
    let x = mouse.x + offset
    let y = mouse.y + offset

    // Horizontal clamp: if it overflows the right edge, place to the left.
    if (x + totalW > vw - 8) {
      x = mouse.x - offset - totalW
    }
    if (x < 8) x = 8

    // Decide whether the button row goes above or below the bubble.
    // Default: buttons below (top row above bubble, bottom row below bubble)?
    // We'll put: top row (History, Close) ABOVE bubble, bottom row (Send) BELOW bubble.
    // If there's no room below for the bottom row, flip everything above.
    const bottomRowBottom = y + size.h + ROW_GAP + rowH
    let flip = false
    if (bottomRowBottom > vh - 8) {
      flip = true
    }
    // Also flip if the top row would overflow the top.
    const topRowTop = y - ROW_GAP - rowH
    if (!flip && topRowTop < 8) {
      // not enough room above for the top row — flip to put all buttons below
      flip = true
    }

    // Vertical clamp
    if (flip) {
      // All buttons above the bubble: [bottom row][top row][bubble]
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
    setFlipped(position.flip)
  }, [position.flip])

  const topButtons = (
    <div className="flex items-center gap-1.5" style={{ gap: BTN_GAP }}>
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
    <div className="flex items-center" style={{ gap: BTN_GAP }}>
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
      className="relative rounded-xl border border-border/70 bg-secondary/90 backdrop-blur-md shadow-xl"
      style={{ minWidth: BUBBLE_MIN_W, maxWidth: BUBBLE_MAX_W }}
      onMouseDown={(e) => e.stopPropagation()}
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
              className="text-muted-foreground hover:text-destructive transition-colors"
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
          style={{ left: position.x, top: position.y }}
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