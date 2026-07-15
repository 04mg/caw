import { useState, useEffect, useRef } from 'react'
import { ZoomIn, ZoomOut, RotateCcw, Loader2, AlertCircle } from 'lucide-react'
import { Button } from '@/components/button'
import { subscribeToFileTree, type FileTreeEvent } from '@/features/explorer/services/fileTreeWs'
import { pathsEqual } from '@/features/shared/utils/path'

interface ImagePreviewViewProps {
  filePath: string
  cwd?: string
}

export function ImagePreviewView({ filePath, cwd }: ImagePreviewViewProps) {
  const [blobUrl, setBlobUrl] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [zoom, setZoom] = useState(1)
  const [refreshCounter, setRefreshCounter] = useState(0)
  const debounceTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    let active = true
    let activeBlobUrl: string | null = null

    setLoading(true)
    setError(null)
    setBlobUrl(null)
    setZoom(1)

    fetch(`/api/workspaces/files?path=${encodeURIComponent(filePath)}&download=true`)
      .then((r) => {
        if (!r.ok) throw new Error(`Failed to load image (${r.status})`)
        return r.blob()
      })
      .then((blob) => {
        if (!active) return
        const isSvg = filePath.toLowerCase().endsWith('.svg')
        const typedBlob = isSvg ? new Blob([blob], { type: 'image/svg+xml' }) : blob
        const url = URL.createObjectURL(typedBlob)
        activeBlobUrl = url
        setBlobUrl(url)
      })
      .catch((e) => {
        if (active) setError(e.message)
      })
      .finally(() => {
        if (active) setLoading(false)
      })

    return () => {
      active = false
      if (activeBlobUrl) {
        URL.revokeObjectURL(activeBlobUrl)
      }
    }
  }, [filePath, refreshCounter])

  useEffect(() => {
    if (!filePath || !cwd) return

    const handleEvent = (event: FileTreeEvent) => {
      if (event.type !== 'file-modified' || event.isDir) return
      if (!pathsEqual(event.path, filePath)) return

      if (debounceTimerRef.current) clearTimeout(debounceTimerRef.current)
      debounceTimerRef.current = setTimeout(() => {
        setRefreshCounter((c) => c + 1)
      }, 300)
    }

    const unsub = subscribeToFileTree(cwd, handleEvent)
    return () => {
      unsub()
      if (debounceTimerRef.current) clearTimeout(debounceTimerRef.current)
    }
  }, [filePath, cwd])

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center gap-2 text-sm text-muted-foreground bg-background">
        <Loader2 className="h-4 w-4 animate-spin text-primary" />
        Loading image...
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-3 p-4 text-center bg-background text-destructive">
        <AlertCircle className="h-8 w-8" />
        <p className="text-sm font-medium">{error}</p>
      </div>
    )
  }

  return (
    <div className="flex h-full w-full flex-col bg-background overflow-hidden">
      {/* Toolbar */}
      <div className="flex items-center gap-1.5 px-3 py-1.5 border-b border-border bg-muted/10 shrink-0">
        <span className="text-[11px] font-mono text-muted-foreground truncate flex-1">
          {filePath}
        </span>
        <span className="text-[11px] text-muted-foreground">{Math.round(zoom * 100)}%</span>
        <Button
          variant="ghost"
          size="sm"
          className="h-6 w-6 p-0"
          onClick={() => setZoom((z) => Math.min(z + 0.25, 4))}
          title="Zoom In"
        >
          <ZoomIn className="h-3.5 w-3.5" />
        </Button>
        <Button
          variant="ghost"
          size="sm"
          className="h-6 w-6 p-0"
          onClick={() => setZoom((z) => Math.max(z - 0.25, 0.25))}
          title="Zoom Out"
        >
          <ZoomOut className="h-3.5 w-3.5" />
        </Button>
        <Button
          variant="ghost"
          size="sm"
          className="h-6 w-6 p-0"
          onClick={() => setZoom(1)}
          title="Reset zoom"
        >
          <RotateCcw className="h-3.5 w-3.5" />
        </Button>
      </div>
      {/* Image canvas */}
      <div
        className="flex-1 min-h-0 overflow-auto flex items-center justify-center p-4 bg-muted/5"
        style={{
          backgroundImage: 'repeating-conic-gradient(#80808015 0% 25%, transparent 0% 50%)',
          backgroundSize: '20px 20px',
        }}
      >
        {blobUrl && (
          <img
            src={blobUrl}
            alt={filePath.split('/').pop()}
            style={{
              transform: `scale(${zoom})`,
              transformOrigin: 'center',
              transition: 'transform 0.15s ease',
              maxWidth: zoom <= 1 ? '100%' : 'none',
              maxHeight: zoom <= 1 ? '100%' : 'none',
              objectFit: 'contain',
            }}
          />
        )}
      </div>
    </div>
  )
}
