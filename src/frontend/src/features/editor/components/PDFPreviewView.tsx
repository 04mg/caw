import { useState, useEffect, useRef, useCallback } from 'react'
import {
  ChevronLeft,
  ChevronRight,
  ZoomIn,
  ZoomOut,
  RotateCcw,
  Loader2,
  AlertCircle,
  Download,
} from 'lucide-react'
import { Button } from '@/components/button'
import { subscribeToFileTree, type FileTreeEvent } from '@/features/explorer/services/fileTreeWs'
import { pathsEqual } from '@/features/shared/utils/path'
import { pdfjsLib } from './pdfjsSetup'

interface PDFPreviewViewProps {
  filePath: string
  cwd?: string
}

type PreviewStatus = 'loading' | 'error' | 'ready'

export function PDFPreviewView({ filePath, cwd }: PDFPreviewViewProps) {
  const [status, setStatus] = useState<PreviewStatus>('loading')
  const [error, setError] = useState<string | null>(null)
  const [numPages, setNumPages] = useState(0)
  const [currentPage, setCurrentPage] = useState(1)
  const [zoom, setZoom] = useState(1)
  const [refreshCounter, setRefreshCounter] = useState(0)
  const [downloadUrl, setDownloadUrl] = useState<string | null>(null)
  const pdfRef = useRef<any>(null)
  const canvasRef = useRef<HTMLCanvasElement | null>(null)
  const renderTaskRef = useRef<any>(null)
  const debounceTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const disposePdfRef = useRef<() => void>(() => {})

  const renderPage = useCallback(
    (pageNum: number) => {
      const pdf = pdfRef.current
      const canvas = canvasRef.current
      if (!pdf || !canvas) return

      if (renderTaskRef.current) {
        renderTaskRef.current.cancel()
      }

      const scale = window.devicePixelRatio * zoom
      pdf.getPage(pageNum).then((page: any) => {
        const viewport = page.getViewport({ scale })
        canvas.width = Math.floor(viewport.width)
        canvas.height = Math.floor(viewport.height)
        canvas.style.width = '100%'
        canvas.style.maxWidth = `${Math.floor(viewport.width / (window.devicePixelRatio * zoom))}px`
        canvas.style.height = 'auto'
        const ctx = canvas.getContext('2d')
        if (!ctx) return
        const task = page.render({ canvasContext: ctx, viewport })
        renderTaskRef.current = task
        task.promise
          .then(() => {
            renderTaskRef.current = null
          })
          .catch((err: any) => {
            if (err?.name !== 'RenderingCancelledException') {
              console.error(err)
            }
          })
      })
    },
    [zoom],
  )

  useEffect(() => {
    let active = true

    setStatus('loading')
    setError(null)
    setNumPages(0)
    setCurrentPage(1)

    fetch(`/api/workspaces/files?path=${encodeURIComponent(filePath)}&download=true`)
      .then((r) => {
        if (!r.ok) throw new Error(`Failed to load PDF (${r.status})`)
        return r.blob()
      })
      .then((blob) => {
        if (!active) return null
        const blobUrl = URL.createObjectURL(blob)
        setDownloadUrl(blobUrl)
        return blobUrl
      })
      .then((blobUrl) => {
        if (!active || !blobUrl) return
        const task = pdfjsLib.getDocument({ url: blobUrl })
        return task.promise.then((doc: any) => {
          if (!active) return
          pdfRef.current = doc
          disposePdfRef.current = () => {
            doc.destroy()
            URL.revokeObjectURL(blobUrl)
          }
          setNumPages(doc.numPages)
          setStatus('ready')
        })
      })
      .catch((e) => {
        if (active) setError(e.message)
      })

    return () => {
      active = false
      if (renderTaskRef.current) renderTaskRef.current.cancel()
      disposePdfRef.current()
      disposePdfRef.current = () => {}
      pdfRef.current = null
    }
  }, [filePath, refreshCounter])

  useEffect(() => {
    if (status === 'ready' && currentPage > 0 && currentPage <= numPages) {
      renderPage(currentPage)
    }
  }, [status, currentPage, numPages, renderPage])

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

  if (status === 'loading') {
    return (
      <div className="flex h-full items-center justify-center gap-2 text-sm text-muted-foreground bg-background">
        <Loader2 className="h-4 w-4 animate-spin text-primary" />
        Loading PDF...
      </div>
    )
  }

  if (status === 'error') {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-3 p-4 text-center bg-background text-destructive">
        <AlertCircle className="h-8 w-8" />
        <p className="text-sm font-medium">{error}</p>
        <Button variant="outline" size="sm" onClick={() => setRefreshCounter((c) => c + 1)}>
          Retry
        </Button>
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
        <span className="text-[11px] text-muted-foreground">
          {currentPage}/{numPages}
        </span>
        <Button
          variant="ghost"
          size="sm"
          className="h-6 w-6 p-0"
          onClick={() => setCurrentPage((p) => Math.max(p - 1, 1))}
          disabled={currentPage <= 1}
          title="Previous page"
        >
          <ChevronLeft className="h-3.5 w-3.5" />
        </Button>
        <Button
          variant="ghost"
          size="sm"
          className="h-6 w-6 p-0"
          onClick={() => setCurrentPage((p) => Math.min(p + 1, numPages))}
          disabled={currentPage >= numPages}
          title="Next page"
        >
          <ChevronRight className="h-3.5 w-3.5" />
        </Button>
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
        {downloadUrl && (
          <a href={downloadUrl} download={filePath.split('/').pop()}>
            <Button variant="ghost" size="sm" className="h-6 w-6 p-0" title="Download">
              <Download className="h-3.5 w-3.5" />
            </Button>
          </a>
        )}
      </div>
      {/* PDF canvas */}
      <div
        className="flex-1 min-h-0 overflow-auto flex items-start justify-center p-4 bg-muted/5"
        style={{
          backgroundImage: 'repeating-conic-gradient(#80808015 0% 25%, transparent 0% 50%)',
          backgroundSize: '20px 20px',
        }}
      >
        <canvas ref={canvasRef} />
      </div>
    </div>
  )
}
