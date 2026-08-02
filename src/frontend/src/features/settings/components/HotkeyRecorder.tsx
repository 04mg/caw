import { useState, useEffect, useRef } from 'react'

interface HotkeyRecorderProps {
  onSave: (combo: string) => void
  onCancel: () => void
}

export function HotkeyRecorder({ onSave, onCancel }: HotkeyRecorderProps) {
  const [keys, setKeys] = useState<string[]>([])
  const ref = useRef<HTMLSpanElement>(null)

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      e.preventDefault()
      e.stopPropagation()
      if (e.key === 'Escape') {
        setKeys([])
        onCancel()
        return
      }
      const parts: string[] = []
      if (e.altKey) parts.push('Alt')
      if (e.ctrlKey) parts.push('Ctrl')
      if (e.metaKey) parts.push('Meta')
      if (e.shiftKey) parts.push('Shift')
      if (parts.length === 0) return
      parts.push(e.key.length === 1 ? e.key.toUpperCase() : e.key)
      setKeys([parts.join('+')])
    }
    window.addEventListener('keydown', handler, { capture: true })
    return () => window.removeEventListener('keydown', handler, { capture: true })
  }, [onCancel])

  return (
    <span
      ref={ref}
      onClick={() => {
        if (keys.length > 0) {
          onSave(keys[0])
        }
      }}
      className={`px-2.5 py-1 rounded-md border text-xs font-mono select-none ${
        keys.length > 0
          ? 'border-primary bg-accent/40 text-foreground cursor-pointer hover:bg-accent'
          : 'border-primary bg-accent/40 text-muted-foreground animate-pulse'
      }`}
    >
      {keys.length > 0 ? keys[0] : 'Press shortcut...'}
    </span>
  )
}