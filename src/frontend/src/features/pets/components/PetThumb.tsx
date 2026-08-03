import { useEffect, useState } from 'react'
import { Loader2 } from 'lucide-react'
import { type PetEntry } from '../petsStore'

const THUMB_H = 48
const THUMB_W = Math.round((THUMB_H * 192) / 208)

// Idle-first-frame thumbnail. Frames are laid out in an 8-column atlas, so
// the idle first frame is the top-left cell. Rows are derived from the
// image's aspect ratio (PetStage does the same). The thumbnail keeps a
// fixed-size placeholder (spinner) until the sprite is decoded so the layout
// never shifts while images load.
export function PetThumb({ entry }: { entry: PetEntry }) {
  const [rows, setRows] = useState(9)
  const [ready, setReady] = useState(false)

  useEffect(() => {
    let cancelled = false
    setReady(false)
    const img = new Image()
    img.onload = () => {
      if (cancelled) return
      const w = img.naturalWidth || 192 * 8
      const h = img.naturalHeight || 208 * 9
      setRows(Math.round((h * 1536) / (208 * w)) || 9)
      setReady(true)
    }
    img.onerror = () => {
      if (!cancelled) setReady(true)
    }
    img.src = entry.spritesheetUrl
    return () => {
      cancelled = true
      img.onload = null
      img.onerror = null
      img.src = ''
    }
  }, [entry.spritesheetUrl])

  return (
    <div className="relative" style={{ width: THUMB_W, height: THUMB_H }}>
      {ready ? (
        <div
          className="h-full w-full"
          style={{
            backgroundImage: `url(${entry.spritesheetUrl})`,
            backgroundSize: `${8 * THUMB_W}px ${rows * THUMB_H}px`,
            backgroundPosition: '0 0',
            imageRendering: 'pixelated',
          }}
        />
      ) : (
        <div className="flex h-full w-full items-center justify-center">
          <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
        </div>
      )}
    </div>
  )
}
