import { useState, useEffect, useRef } from 'react'

interface HotkeyRecorderProps {
  onSave: (combo: string) => void
  onCancel: () => void
}

export function HotkeyRecorder({ onSave, onCancel }: HotkeyRecorderProps) {
  const [combo, setCombo] = useState('')
  const [completeCombo, setCompleteCombo] = useState('')
  const ref = useRef<HTMLSpanElement>(null)

  useEffect(() => {
    document.documentElement.dataset.cawHotkeyRecording = '1'

    const handler = (e: KeyboardEvent) => {
      e.preventDefault()
      e.stopPropagation()
      if (e.key === 'Escape') {
        setCombo('')
        setCompleteCombo('')
        onCancel()
        return
      }
      const isModifierKey = e.key === 'Alt' || e.key === 'Control' || e.key === 'Meta' || e.key === 'Shift'
      const parts: string[] = []
      if (e.altKey) parts.push('Alt')
      if (e.ctrlKey) parts.push('Ctrl')
      if (e.metaKey) parts.push('Meta')
      if (e.shiftKey) parts.push('Shift')
      if (parts.length === 0) return
      if (!isModifierKey) parts.push(e.key.length === 1 ? e.key.toUpperCase() : e.key)
      const nextCombo = parts.join('+')
      setCombo(nextCombo)
      setCompleteCombo(isModifierKey ? '' : nextCombo)
    }
    window.addEventListener('keydown', handler, { capture: true })
    return () => {
      window.removeEventListener('keydown', handler, { capture: true })
      delete document.documentElement.dataset.cawHotkeyRecording
    }
  }, [onCancel])

  return (
    <span
      ref={ref}
      onClick={() => {
        if (completeCombo) {
          onSave(completeCombo)
        }
      }}
      className={`px-2.5 py-1 rounded-md border border-primary bg-accent/40 text-xs font-mono select-none animate-pulse ${
        completeCombo ? 'text-foreground cursor-pointer hover:bg-accent' : 'text-muted-foreground'
      }`}
    >
      {combo || 'Press shortcut...'}
    </span>
  )
}
