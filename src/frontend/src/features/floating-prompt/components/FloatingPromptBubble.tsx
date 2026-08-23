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
// Upper bounds once the user manually resizes: a wider, taller bubble for
// long prompts. Height is additionally capped at 60% of the viewport.
const BUBBLE_MAX_W_RESIZED = 520
const TEXTAREA_AUTO_MAX_H = 220
const BUBBLE_MIN_H = 44
const BTN_SIZE = 26
const BTN_GAP = 6
const ROW_GAP = 4
const MARGIN = 8
// Pointer must move this far (px) before a press becomes a drag, so clicks
// still reach buttons and place the textarea caret.
const DRAG_THRESHOLD = 4

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
  const wrapRef = useRef<HTMLDivElement>(null)
  const [size, setSize] = useState({ w: BUBBLE_MIN_W, h: BUBBLE_MIN_H })
  const [showHistory, setShowHistory] = useState(false)
  const [isDragging, setIsDragging] = useState(false)
  // Manual size set by dragging the corner grip. While null the bubble
  // auto-sizes to its content; while set it overrides both default width and
  // auto-grow height. Reset when the bubble closes so every open starts
  // compact (mirroring how the pinned position resets).
  const [userSize, setUserSize] = useState<{ w: number; h: number } | null>(null)

  // Auto-grow the textarea with its content (measure natural height, clamp
  // between the bubble minimum and the auto-grow cap), then measure the
  // resulting bubble so position clamping tracks the real size. A manual
  // resize fixes the height instead — content scrolls inside it.
  useLayoutEffect(() => {
    const ta = taRef.current
    if (ta) {
      if (userSize) {
        ta.style.height = `${Math.max(userSize.h, BUBBLE_MIN_H)}px`
      } else {
        ta.style.height = 'auto'
        const h = Math.min(Math.max(ta.scrollHeight, BUBBLE_MIN_H), TEXTAREA_AUTO_MAX_H)
        ta.style.height = `${h}px`
      }
    }
    const el = bubbleRef.current
    if (!el) return
    const r = el.getBoundingClientRect()
    setSize({ w: Math.ceil(r.width), h: Math.ceil(r.height) })
  }, [text, open, showHistory, userSize])

  useEffect(() => {
    if (!open) setUserSize(null)
  }, [open])

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

  // Close history dropdown when clicking outside. The "inside" area is the
  // whole composite (button rows included) — the History button itself lives
  // OUTSIDE the bubble div, so testing only the bubble would treat pressing
  // History as an outside click: mousedown closes, then the click handler
  // toggles it straight back open, making the button unable to close it.
  useEffect(() => {
    if (!showHistory) return
    const onDown = (e: MouseEvent) => {
      if (wrapRef.current?.contains(e.target as Node)) return
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
  // Dims default to the measured bubble size; the resize grip passes the
  // in-progress size so the position re-clamps as the bubble grows.
  const clampToViewport = useCallback(
    (x: number, y: number, w?: number, h?: number): FloatingPromptPosition => {
      const { fullW, fullH } = compositeBounds(w ?? size.w, h ?? size.h)
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

  // Unified pointer-drag controller. A single handler on the composite
  // wrapper makes the ENTIRE bubble draggable from anywhere — textarea,
  // buttons, padding — instead of relying on scattered onMouseDown handlers
  // with per-element exclusions.
  //
  // A small movement threshold keeps plain clicks working (caret placement,
  // button clicks): a press without movement is left untouched, any real
  // gesture becomes a drag. Once dragging starts we capture the pointer so
  // tracking cannot be lost outside the bubble, cancel whatever text
  // selection that press began inside the textarea, and swallow the trailing
  // click so dragged buttons don't also fire their onClick.
  const startDrag = (e: React.PointerEvent<HTMLDivElement>) => {
    if (!e.isPrimary) return
    if (e.pointerType === 'mouse' && e.button !== 0) return
    const target = e.target as HTMLElement
    if (target.closest('[data-no-drag]')) return

    const el = e.currentTarget
    const startX = e.clientX
    const startY = e.clientY
    const originX = effectivePos.x
    const originY = effectivePos.y
    let lastPos: FloatingPromptPosition = { x: originX, y: originY }
    let dragging = false

    // After a real drag, eat the click that follows release so buttons under
    // the pointer don't trigger; auto-cleans if no click is dispatched.
    const swallowClick = (ev: MouseEvent) => {
      ev.preventDefault()
      ev.stopPropagation()
      window.removeEventListener('click', swallowClick, true)
    }

    const onMove = (ev: PointerEvent) => {
      const dx = ev.clientX - startX
      const dy = ev.clientY - startY
      if (!dragging && Math.hypot(dx, dy) < DRAG_THRESHOLD) return
      if (!dragging) {
        dragging = true
        try { el.setPointerCapture(ev.pointerId) } catch { /* noop */ }
        window.getSelection()?.removeAllRanges()
        setIsDragging(true)
      }
      ev.preventDefault()
      lastPos = clampToViewport(originX + dx, originY + dy)
      onPinPosition(lastPos)
    }
    const onUp = (ev: PointerEvent) => {
      window.removeEventListener('pointermove', onMove)
      window.removeEventListener('pointerup', onUp)
      window.removeEventListener('pointercancel', onUp)
      if (dragging) {
        try { el.releasePointerCapture(ev.pointerId) } catch { /* noop */ }
        onPinPosition(lastPos)
        setIsDragging(false)
        window.addEventListener('click', swallowClick, true)
        setTimeout(() => window.removeEventListener('click', swallowClick, true), 0)
      }
    }
    window.addEventListener('pointermove', onMove, { passive: false })
    window.addEventListener('pointerup', onUp)
    window.addEventListener('pointercancel', onUp)
  }

  // Corner-grip resize, mirroring startDrag's pointer-capture controller.
  // Deltas apply to the size captured at press and clamp to sensible bounds
  // (and the viewport). While pinned, the position is re-clamped against the
  // growing size so the composite never hangs off-screen.
  const startResize = (e: React.PointerEvent<HTMLDivElement>) => {
    if (!e.isPrimary) return
    if (e.pointerType === 'mouse' && e.button !== 0) return
    const el = bubbleRef.current
    if (!el) return
    e.preventDefault()
    e.stopPropagation()

    const startX = e.clientX
    const startY = e.clientY
    const rect = el.getBoundingClientRect()
    const originW = rect.width
    const originH = rect.height
    const vw = window.innerWidth
    const vh = window.innerHeight
    const maxW = Math.min(BUBBLE_MAX_W_RESIZED, Math.max(BUBBLE_MIN_W, vw - MARGIN * 2))
    const maxH = Math.max(BUBBLE_MIN_H, vh - MARGIN * 2 - BTN_SIZE * 2 - ROW_GAP * 2)
    let lastSize = { w: originW, h: originH }

    const apply = (w: number, h: number) => {
      lastSize = {
        w: Math.min(Math.max(w, BUBBLE_MIN_W), maxW),
        h: Math.min(Math.max(h, BUBBLE_MIN_H), maxH),
      }
      setUserSize(lastSize)
      if (pinnedPos) {
        onPinPosition(clampToViewport(pinnedPos.x, pinnedPos.y, lastSize.w, lastSize.h))
      }
    }

    const onMove = (ev: PointerEvent) => {
      ev.preventDefault()
      try { el.setPointerCapture(ev.pointerId) } catch { /* noop */ }
      apply(originW + (ev.clientX - startX), originH + (ev.clientY - startY))
    }
    const onUp = (ev: PointerEvent) => {
      window.removeEventListener('pointermove', onMove)
      window.removeEventListener('pointerup', onUp)
      window.removeEventListener('pointercancel', onUp)
      try { el.releasePointerCapture(ev.pointerId) } catch { /* noop */ }
      apply(lastSize.w, lastSize.h)
    }
    window.addEventListener('pointermove', onMove, { passive: false })
    window.addEventListener('pointerup', onUp)
    window.addEventListener('pointercancel', onUp)
  }

  const topButtons = (
    <div className="flex items-center" style={{ gap: BTN_GAP }}>
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
      className="relative z-10 rounded-xl border border-border/70 bg-secondary/90 backdrop-blur-md shadow-xl"
      style={{
        minWidth: BUBBLE_MIN_W,
        maxWidth: userSize ? BUBBLE_MAX_W_RESIZED : BUBBLE_MAX_W,
        width: userSize?.w,
      }}
    >
      <textarea
        ref={taRef}
        data-no-drag
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
          'overflow-y-auto scrollbar-none',
        )}
        style={{ minHeight: BUBBLE_MIN_H, touchAction: 'auto' }}
      />

      {/* Corner grip: drag to resize the bubble. */}
      <div
        data-no-drag
        onPointerDown={startResize}
        title="Resize"
        className="absolute bottom-0 right-0 z-20 cursor-nwse-resize touch-none p-1 text-muted-foreground/40 hover:text-muted-foreground"
      >
        <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
          <path d="M9 1 L1 9 M9 5 L5 9" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" fill="none" />
        </svg>
      </div>

      {showHistory && history.length > 0 && (
        <div data-no-drag className="absolute left-0 right-0 top-full z-10 mt-1 max-h-48 overflow-y-auto rounded-lg border border-border/70 bg-popover/95 backdrop-blur-md shadow-lg">
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
          <div
            ref={wrapRef}
            className={cn('flex flex-col items-start pointer-events-auto touch-none', isDragging ? 'cursor-grabbing select-none' : 'cursor-grab')}
            style={{ gap: ROW_GAP }}
            onPointerDown={startDrag}
          >
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