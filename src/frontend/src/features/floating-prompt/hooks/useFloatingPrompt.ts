import { useCallback, useEffect, useRef, useState } from 'react'

export interface FloatingPromptState {
  open: boolean
  text: string
  mouse: { x: number; y: number }
  history: string[]
}

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
  // A single-character key with no destructive modifier (except Shift).
  // Also accept dead keys (composing accents) so non-English layouts work.
  if (e.ctrlKey || e.altKey || e.metaKey) return false
  if (e.key.length !== 1) return false
  // Reject control characters
  if (e.key.charCodeAt(0) < 32) return false
  return true
}

function isFocusInTextContext(): boolean {
  const el = document.activeElement
  if (!el) return false
  if (el === document.body) return true
  // If the active element matches any of the text-entry selectors, the user
  // is typing into that element, not "into the void".
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

export function useFloatingPrompt() {
  const [open, setOpen] = useState(false)
  const [text, setText] = useState('')
  const [mouse, setMouse] = useState({ x: 0, y: 0 })
  const [history, setHistory] = useState<string[]>(() => loadHistory())
  const mouseRef = useRef(mouse)
  mouseRef.current = mouse

  // Track the mouse position continuously so the bubble can open right next
  // to the cursor the moment a printable key lands while unfocused.
  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      mouseRef.current = { x: e.clientX, y: e.clientY }
      if (open) {
        setMouse({ x: e.clientX, y: e.clientY })
      }
    }
    window.addEventListener('mousemove', onMove)
    return () => window.removeEventListener('mousemove', onMove)
  }, [open])

  const openBubble = useCallback((initialChar: string) => {
    setMouse({ ...mouseRef.current })
    setText(initialChar)
    setOpen(true)
  }, [])

  const closeBubble = useCallback(() => {
    setOpen(false)
    setText('')
  }, [])

  const sendAndClose = useCallback(() => {
    const trimmed = text
    if (trimmed) {
      const next = [trimmed, ...history.filter((h) => h !== trimmed)].slice(0, HISTORY_LIMIT)
      setHistory(next)
      saveHistory(next)
    }
    setOpen(false)
    setText('')
  }, [text, history])

  const clearHistory = useCallback(() => {
    setHistory([])
    saveHistory([])
  }, [])

  const insertFromHistory = useCallback((item: string) => {
    setText(item)
  }, [])

  // Global key listener: open the bubble when a printable key is typed while
  // the focus is not inside a text-entry element. Escape closes it.
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (open) {
        if (e.key === 'Escape') {
          e.preventDefault()
          e.stopPropagation()
          closeBubble()
        }
        // While open, let the textarea handle all other keys — don't intercept.
        return
      }

      if (e.key === 'Escape') return

      if (!isPrintableKey(e)) return
      if (!isFocusInTextContext()) return

      // Don't open while a modifier-driven hotkey is in flight (handled above),
      // or while a drag is happening, etc. The key is printable and the focus
      // is in the void — open the bubble seeded with this character.
      e.preventDefault()
      e.stopPropagation()
      openBubble(e.key)
    }

    // Use capture so we intercept before xterm/Monaco/etc. could claim it.
    // We only act when focus is NOT in those elements, so this is safe.
    window.addEventListener('keydown', onKeyDown, true)
    return () => {
      window.removeEventListener('keydown', onKeyDown, true)
    }
  }, [open, openBubble, closeBubble])

  return {
    open,
    text,
    mouse,
    history,
    offset: OFFSET,
    setText,
    openBubble,
    closeBubble,
    sendAndClose,
    clearHistory,
    insertFromHistory,
  }
}