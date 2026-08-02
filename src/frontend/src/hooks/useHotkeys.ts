import { useEffect, useRef } from 'react'

type HotkeyMap = Record<string, () => void>

export function useHotkeys(map: HotkeyMap) {
  const mapRef = useRef(map)
  mapRef.current = map

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (document.documentElement.dataset.cawHotkeyRecording === '1') return

      const parts: string[] = []
      if (e.altKey) parts.push('Alt')
      if (e.ctrlKey) parts.push('Ctrl')
      if (e.metaKey) parts.push('Meta')
      if (e.shiftKey) parts.push('Shift')
      parts.push(e.key.length === 1 ? e.key.toUpperCase() : e.key)

      const combo = parts.join('+')
      const fn = mapRef.current[combo]
      if (fn) {
        e.preventDefault()
        e.stopPropagation()
        fn()
      }
    }

    window.addEventListener('keydown', handler, { capture: true })
    return () => window.removeEventListener('keydown', handler, { capture: true })
  }, [])
}
