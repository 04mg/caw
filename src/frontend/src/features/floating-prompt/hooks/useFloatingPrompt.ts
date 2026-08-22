import { useCallback, useEffect, useRef, useState } from 'react'

const HISTORY_KEY = 'caw:floatingPromptHistory'
const HISTORY_LIMIT = 50
const OFFSET = 16

// Elements that already handle text entry — typing into these should NOT
// open the floating bubble, the user is intentionally writing there.
const FOCUS_SELECTOR = [
  'input',
  'textarea',
  '[contenteditable="true"]',
  '[contenteditable=""]',
  '[role="textbox"]',
  '.xterm',
  '.xterm-helper-textarea',
  '.monaco-editor',
  '[role="dialog"]',
  '.smart-context-menu',
  '.custom-context-menu',
  '[data-floating-prompt]',
].join(',')

function isPrintableKey(e: KeyboardEvent): boolean {
  if (e.ctrlKey || e.altKey || e.metaKey) return false
  if (e.key.length !== 1) return false
  if (e.key.charCodeAt(0) < 32) return false
  return true
}

function isFocusInTextContext(): boolean {
  const el = document.activeElement
  if (!el) return false
  if (el === document.body) return true
  if ((el as HTMLElement).closest?.(FOCUS_SELECTOR)) return false
  return true
}

function loadHistory(): string[] {
  try {
    const raw = localStorage.getItem(HISTORY_KEY)
    if (!raw) return []
    const parsed = JSON.parse(raw)
    if (Array.isArray(parsed)) return parsed.filter((v) => typeof v === 'string').slice(0, HISTORY_LIMIT)
  } catch { /* ignore */ }
  return []
}

function saveHistory(items: string[]) {
  try {
    localStorage.setItem(HISTORY_KEY, JSON.stringify(items.slice(0, HISTORY_LIMIT)))
  } catch { /* ignore */ }
}

export interface FloatingPromptPosition {
  x: number
  y: number
}

export function useFloatingPrompt() {
  const [open, setOpen] = useState(false)
  const [text, setText] = useState('')
  const [mouse, setMouse] = useState({ x: 0, y: 0 })
  const [history, setHistory] = useState<string[]>(() => loadHistory())
  // pinnedPos is set when the user drags the bubble. While null, the
  // component auto-positions next to the cursor. Once set, it sticks.
  // It is cleared when the bubble is fully closed so the next open
  // re-anchors to the cursor — unless reopened via the hotkey, which
  // preserves the last pinned position.
  const [pinnedPos, setPinnedPos] = useState<FloatingPromptPosition | null>(null)
  const mouseRef = useRef(mouse)
  mouseRef.current = mouse

  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      mouseRef.current = { x: e.clientX, y: e.clientY }
    }
    window.addEventListener('mousemove', onMove)
    return () => window.removeEventListener('mousemove', onMove)
  }, [])

  const openBubble = useCallback((initialChar: string) => {
    setMouse({ ...mouseRef.current })
    setText(initialChar)
    setPinnedPos(null)
    setOpen(true)
  }, [])

  // Reopen via hotkey (Alt+Space): open with existing text preserved, at the
  // last pinned position if there is one, otherwise anchor to the cursor.
  const reopenBubble = useCallback(() => {
    if (!pinnedPos) setMouse({ ...mouseRef.current })
    setOpen(true)
  }, [pinnedPos])

  // Close without clearing text (Escape, or clicking outside).
  const closeBubble = useCallback(() => {
    setOpen(false)
  }, [])

  // Close and clear text (after a successful send).
  const sendAndClose = useCallback(() => {
    const trimmed = text
    if (trimmed) {
      const next = [trimmed, ...history.filter((h) => h !== trimmed)].slice(0, HISTORY_LIMIT)
      setHistory(next)
      saveHistory(next)
    }
    setOpen(false)
    setText('')
    setPinnedPos(null)
  }, [text, history])

  const clearHistory = useCallback(() => {
    setHistory([])
    saveHistory([])
  }, [])

  const insertFromHistory = useCallback((item: string) => {
    setText(item)
  }, [])

  // Called by the component when the user finishes dragging — pins the
  // position so it persists until the bubble is closed.
  const pinPosition = useCallback((pos: FloatingPromptPosition) => {
    setPinnedPos(pos)
  }, [])

  // Global key listener:
  //  - Alt+Space: reopen the bubble (regardless of focus).
  //  - Escape (while open): close without clearing.
  //  - printable key (while closed, unfocused): open seeded with the char.
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      // Alt+Space: always reopen (even if focus is in a terminal/editor).
      if (e.altKey && e.code === 'Space') {
        e.preventDefault()
        e.stopPropagation()
        if (!open) {
          reopenBubble()
        }
        return
      }

      if (open) {
        if (e.key === 'Escape') {
          e.preventDefault()
          e.stopPropagation()
          closeBubble()
        }
        return
      }

      if (e.key === 'Escape') return

      if (!isPrintableKey(e)) return
      if (!isFocusInTextContext()) return

      e.preventDefault()
      e.stopPropagation()
      openBubble(e.key)
    }

    window.addEventListener('keydown', onKeyDown, true)
    return () => {
      window.removeEventListener('keydown', onKeyDown, true)
    }
  }, [open, openBubble, closeBubble, reopenBubble])

  return {
    open,
    text,
    mouse,
    pinnedPos,
    history,
    offset: OFFSET,
    setText,
    openBubble,
    reopenBubble,
    closeBubble,
    sendAndClose,
    clearHistory,
    insertFromHistory,
    pinPosition,
  }
}