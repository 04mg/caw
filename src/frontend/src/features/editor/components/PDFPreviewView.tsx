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
  const [zoom, setZoom] = useState(1)
  const [refreshCounter, setRefreshCounter] = useState(0)
  const [downloadUrl, setDownloadUrl] = useState<string | null>(null)
  const pdfRef = useRef<any>(null)
  const scrollRef = useRef<HTMLDivElement | null>(null)
  const pageRefs = useRef<Array<HTMLCanvasElement | null>>([])
  const renderTasksRef = useRef(new Map<number, any>())
  const renderedRef = useRef(new Set<number>())
  const debounceTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const disposePdfRef = useRef<() => void>(() => {})
  const zoomRef = useRef(zoom)
  zoomRef.current = zoom

  const renderPage = useCallback((pageNum: number) => {
    const pdf = pdfRef.current
    const canvas = pageRefs.current[pageNum - 1]
    if (!pdf || !canvas) return

    const zoomVal = zoomRef.current
    const scale = window.devicePixelRatio * zoomVal

    pdf.getPage(pageNum).then((page: any) => {
      const viewport = page.getViewport({ scale })
      canvas.width = Math.floor(viewport.width)
      canvas.height = Math.floor(viewport.height)
      canvas.style.width = `${Math.floor(viewport.width / window.devicePixelRatio)}px`
      canvas.style.height = `${Math.floor(viewport.height / window.devicePixelRatio)}px`
      const ctx = canvas.getContext('2d')
      if (!ctx) return
      const task = page.render({ canvasContext: ctx, viewport })
      renderTasksRef.current.set(pageNum, task)
      task.promise
        .then(() => {
          renderTasksRef.current.delete(pageNum)
          renderedRef.current.add(pageNum)
        })
        .catch((err: any) => {
          if (err?.name !== 'RenderingCancelledException') {
            console.error(err)
          }
        })
    })
  }, [])

  // Render pages lazily as they enter the viewport.
  useEffect(() => {
    if (status !== 'ready') return
    const scrollEl = scrollRef.current
    if (!scrollEl) return

    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (!entry.isIntersecting) continue
          const idx = Number((entry.target as HTMLElement).dataset.pageIndex)
          if (renderedRef.current.has(idx)) continue
          renderPage(idx)
        }
      },
      { root: scrollEl, rootMargin: '200px 0px' },
    )

    pageRefs.current.forEach((canvas) => {
      if (canvas) observer.observe(canvas)
    })

    return () => observer.disconnect()
  }, [status, numPages, renderPage])

  useEffect(() => {
    let active = true

    setStatus('loading')
    setError(null)
    setNumPages(0)

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
            task.destroy()
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
      renderTasksRef.current.forEach((task) => task.cancel())
      renderTasksRef.current.clear()
      renderedRef.current.clear()
      disposePdfRef.current()
      disposePdfRef.current = () => {}
      pdfRef.current = null
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

  const scrollToPage = useCallback((pageNum: number) => {
    const canvas = pageRefs.current[pageNum - 1]
    if (canvas) canvas.scrollIntoView({ block: 'start', behavior: 'auto' })
  }, [])

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

  const pages = Array.from({ length: numPages }, (_, i) => i + 1)

  return (
    <div className="flex h-full w-full flex-col bg-background overflow-hidden">
      {/* Toolbar */}
      <div className="flex items-center gap-1.5 px-3 py-1.5 border-b border-border bg-muted/10 shrink-0">
        <span className="text-[11px] font-mono text-muted-foreground truncate flex-1">
          {filePath}
        </span>
        <span className="text-[11px] text-muted-foreground">{numPages} pages</span>
        <Button
          variant="ghost"
          size="sm"
          className="h-6 w-6 p-0"
          onClick={() => scrollToPage(1)}
          title="First page"
        >
          <ChevronLeft className="h-3.5 w-3.5" />
        </Button>
        <Button
          variant="ghost"
          size="sm"
          className="h-6 w-6 p-0"
          onClick={() => scrollToPage(numPages)}
          title="Last page"
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
      {/* PDF document */}
      <div
        ref={scrollRef}
        className="flex-1 min-h-0 overflow-auto bg-muted/5"
        style={{
          backgroundImage: 'repeating-conic-gradient(#80808015 0% 25%, transparent 0% 50%)',
          backgroundSize: '20px 20px',
        }}
      >
        <div className="mx-auto flex flex-col items-center gap-4 py-4">
          {pages.map((pageNum) => (
            <div
              key={pageNum}
              className="shrink-0 bg-background shadow-lg border border-border"
              style={{ padding: '12px' }}
            >
              <canvas
                ref={(el) => {
                  pageRefs.current[pageNum - 1] = el
                }}
                data-page-index={pageNum}
              />
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
