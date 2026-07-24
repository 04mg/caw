import { useEffect, useState, useRef, useCallback } from 'react'
import Editor, { DiffEditor, type Monaco } from '@monaco-editor/react'
import { Save, AlertCircle, RefreshCw, Check, GitBranch, Eye, Code } from 'lucide-react'
import { Button } from '@/components/button'
import { getFileCategory, isBinaryContent } from '../utils/fileType'
import { BinaryFileView } from './BinaryFileView'
import { ImagePreviewView } from './ImagePreviewView'
import { MarkdownPreviewView } from './MarkdownPreviewView'
import { subscribeToFileTree, type FileTreeEvent } from '@/features/explorer/services/fileTreeWs'
import { pathsEqual } from '@/features/shared/utils/path'
import { isFileDirty, markFileDirty, clearFileDirty } from '../services/editorDirtyStore'
import { useFileDirty } from '../hooks/useFileDirty'


function defineCawDarkTheme(monaco: Monaco) {
  monaco.languages.typescript.typescriptDefaults.setDiagnosticsOptions({
    noSemanticValidation: true,
    noSyntaxValidation: true,
  })
  monaco.languages.typescript.javascriptDefaults.setDiagnosticsOptions({
    noSemanticValidation: true,
    noSyntaxValidation: true,
  })
  monaco.editor.defineTheme('caw-dark', {
    base: 'vs-dark',
    inherit: true,
    rules: [],
    colors: {
      'editor.background': '#000000',
      'editorGutter.background': '#000000',
      'editorLineNumber.foreground': '#3a3a3a',
      'minimap.background': '#000000',
      'diffEditor.insertedTextBackground': '#1a4a1a55',
      'diffEditor.removedTextBackground': '#4a1a1a55',
      'diffEditorGutter.insertedLineBackground': '#0a3d0a33',
      'diffEditorGutter.removedLineBackground': '#3d0a0a33',
      'editorGutter.modifiedBackground': '#3a3a3a',
      'editorGutter.addedBackground': '#2a5a2a',
      'editorGutter.deletedBackground': '#5a2a2a',
      'editorWidget.background': '#000000',
      'editorWidget.border': '#1a1a1a',
      'editorSuggestWidget.background': '#000000',
      'editorSuggestWidget.border': '#1a1a1a',
      'editorHoverWidget.background': '#000000',
      'editorHoverWidget.border': '#1a1a1a',
      'peekViewResult.background': '#000000',
      'peekViewEditor.background': '#000000',
      'peekViewEditorGutter.background': '#000000',
    },
  })
}

interface EditorPanelProps {
  filePath?: string
  isDiff?: boolean
  cwd: string
  onSaveSuccess?: () => void
  gitStatuses?: Record<string, string>
  onOpenDiff?: (filePath?: string) => void
}

export function EditorPanel({ filePath, isDiff, cwd, onSaveSuccess, gitStatuses, onOpenDiff }: EditorPanelProps) {
  const [content, setContent] = useState('')
  const [originalContent, setOriginalContent] = useState('')
  const [editedContent, setEditedContent] = useState('')
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [saveStatus, setSaveStatus] = useState<'idle' | 'success' | 'error'>('idle')
  const [forceOpenBinary, setForceOpenBinary] = useState(false)
  const [isBinaryRuntime, setIsBinaryRuntime] = useState(false)
  const [diskConflict, setDiskConflict] = useState(false)
  // Initial value for the model the first time a path is seen. After that,
  // Monaco keeps the model alive across unmounts and we must NOT overwrite it.
  const [initialValue, setInitialValue] = useState<string | undefined>(undefined)
  // Toggle between source editor and rendered markdown preview for .md files.
  const [view, setView] = useState<'editor' | 'preview'>('editor')
  const isMarkdown = !!filePath && filePath.toLowerCase().endsWith('.md')

  // Reset binary override when file changes
  useEffect(() => {
    setForceOpenBinary(false)
    setIsBinaryRuntime(false)
    setInitialValue(undefined)
    setView('editor')
  }, [filePath])

  const originalContentRef = useRef('')
  const editorRef = useRef<any>(null)
  const monacoRef = useRef<Monaco | null>(null)
  const lastSavedAtRef = useRef(0)
  const debounceTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Helper: get the live Monaco model for this filePath (if any).
  const getLiveModel = useCallback(() => {
    if (!filePath || !monacoRef.current) return null
    const uri = monacoRef.current.Uri.parse(`file://${filePath}`)
    return monacoRef.current.editor.getModel(uri)
  }, [filePath])

  // Determine file language based on extension
  const getLanguage = (path?: string) => {
    if (!path) return 'plaintext'
    const ext = path.split('.').pop()?.toLowerCase()
    switch (ext) {
      case 'ts':
      case 'tsx':
        return 'typescript'
      case 'js':
      case 'jsx':
        return 'javascript'
      case 'json':
        return 'json'
      case 'go':
        return 'go'
      case 'css':
        return 'css'
      case 'html':
        return 'html'
      case 'md':
        return 'markdown'
      case 'py':
        return 'python'
      case 'sh':
      case 'bash':
        return 'shell'
      case 'yml':
      case 'yaml':
        return 'yaml'
      case 'xml':
        return 'xml'
      default:
        return 'plaintext'
    }
  }

  const loadFile = useCallback(async () => {
    if (!filePath) {
      if (isDiff) {
        // Load global git diff
        setLoading(true)
        setError(null)
        try {
          const res = await fetch(`/api/git/diffs?path=${encodeURIComponent(cwd)}`)
          if (res.ok) {
            const json = await res.json()
            setContent(json?.data?.content ?? '')
          } else {
            const json = await res.json().catch(() => null)
            setError(json?.error?.message ?? 'Failed to load git diff.')
          }
        } catch {
          setError('Network error loading git diff.')
        } finally {
          setLoading(false)
        }
      }
      return
    }

    const category = getFileCategory(filePath)
    if (!isDiff && category === 'image') {
      setLoading(false)
      setError(null)
      return
    }

    if (!isDiff && category === 'binary-likely' && !forceOpenBinary) {
      setLoading(false)
      setError(null)
      return
    }

    setLoading(true)
    setError(null)
    setSaveStatus('idle')

    try {
      if (isDiff) {
        // Fetch both original (from git HEAD) and current modified file content
        const [resOrig, resCurr] = await Promise.all([
          fetch(`/api/git/originals?path=${encodeURIComponent(filePath)}`),
          fetch(`/api/workspaces/files?path=${encodeURIComponent(filePath)}`),
        ])

        const origJson = resOrig.ok ? await resOrig.json() : null
        const currJson = resCurr.ok ? await resCurr.json() : null
        const origText = origJson?.data?.content ?? ''
        const currText = currJson?.data?.content ?? ''

        setOriginalContent(origText)
        setEditedContent(currText)
      } else {
        // Normal file read. We always fetch to get the on-disk content for
        // disk-conflict detection (originalContentRef). If a Monaco model
        // already exists for this path (keepCurrentModel), the Editor will
        // use it and ignore `defaultValue` — preserving undo/cursor/scroll.
        const res = await fetch(`/api/workspaces/files?path=${encodeURIComponent(filePath)}`)
        if (res.ok) {
          const json = await res.json()
          const text = json?.data?.content ?? ''
          setInitialValue(text)
          setEditedContent(text)
          originalContentRef.current = text
          setIsBinaryRuntime(isBinaryContent(text))
        } else {
          const json = await res.json().catch(() => null)
          setError(json?.error?.message ?? `Failed to read file: ${res.statusText}`)
        }
      }
    } catch (err: any) {
      setError(err?.message || 'Error loading file content.')
    } finally {
      setLoading(false)
    }
  }, [filePath, isDiff, cwd, forceOpenBinary])

  useEffect(() => {
    loadFile()
  }, [loadFile])

  const silentReload = useCallback(async () => {
    if (!filePath) return
    try {
      if (isDiff) {
        const [resOrig, resCurr] = await Promise.all([
          fetch(`/api/git/originals?path=${encodeURIComponent(filePath)}`),
          fetch(`/api/workspaces/files?path=${encodeURIComponent(filePath)}`),
        ])
        const origText = resOrig.ok ? (await resOrig.json())?.data?.content ?? '' : ''
        const currText = resCurr.ok ? (await resCurr.json())?.data?.content ?? '' : ''
        setOriginalContent(origText)
        setEditedContent(currText)
      } else {
        const res = await fetch(`/api/workspaces/files?path=${encodeURIComponent(filePath)}`)
        if (!res.ok) return
        const text = (await res.json())?.data?.content ?? ''
        setContent(text)
        setEditedContent(text)
        originalContentRef.current = text
        clearFileDirty(filePath)
        const model = getLiveModel()
        if (model) model.setValue(text)
        if (editorRef.current) editorRef.current.setValue(text)
      }
    } catch { /* ignore */ }
  }, [filePath, isDiff, getLiveModel])

  const handleReloadFromDisk = useCallback(() => {
    setDiskConflict(false)
    silentReload()
  }, [silentReload])

  useEffect(() => {
    if (!filePath || !cwd) return
    setDiskConflict(false)

    const handleEvent = (event: FileTreeEvent) => {
      if (event.type !== 'file-modified' || event.isDir) return
      if (!pathsEqual(event.path, filePath)) return
      if (Date.now() - lastSavedAtRef.current < 500) return

      if (debounceTimerRef.current) clearTimeout(debounceTimerRef.current)
      debounceTimerRef.current = setTimeout(() => {
        const dirty = !isDiff && isFileDirty(filePath)
        if (dirty) {
          setDiskConflict(true)
        } else {
          setDiskConflict(false)
          silentReload()
        }
      }, 300)
    }

    const unsub = subscribeToFileTree(cwd, handleEvent)
    return () => {
      unsub()
      if (debounceTimerRef.current) clearTimeout(debounceTimerRef.current)
    }
  }, [filePath, cwd, isDiff, silentReload])

  const handleSave = useCallback(async () => {
    if (!filePath || isDiff || saving) return
    setSaving(true)
    setSaveStatus('idle')
    try {
      // Prefer reading from the live Monaco model (it's the source of truth
      // and survives across tab switches).
      const model = getLiveModel()
      const bodyContent = model ? model.getValue() : editedContent

      const res = await fetch('/api/workspaces/files', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          path: filePath,
          content: bodyContent,
        }),
      })

      if (res.ok) {
        originalContentRef.current = bodyContent
        setContent(bodyContent)
        setEditedContent(bodyContent)
        // Keep the Monaco model's undo/redo history intact across saves
        // (VS Code parity). We only push a fresh undo stop so the just-saved
        // state is a clean checkpoint; Ctrl+Z still walks back through prior
        // edits until the file (model) is closed.
        try { model?.pushStackElement() } catch { /* ignore */ }
        clearFileDirty(filePath)
        setSaveStatus('success')
        lastSavedAtRef.current = Date.now()
        if (onSaveSuccess) onSaveSuccess()
        setTimeout(() => setSaveStatus('idle'), 2000)
      } else {
        setSaveStatus('error')
      }
    } catch {
      setSaveStatus('error')
    } finally {
      setSaving(false)
    }
  }, [filePath, isDiff, saving, editedContent, onSaveSuccess, getLiveModel])

  // Handle Ctrl+S keybinding
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key === 's') {
        e.preventDefault()
        handleSave()
      }
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [handleSave])

  const isDirty = useFileDirty(filePath) && !isDiff

  const handleEditorChange = (value?: string) => {
    if (value !== undefined && filePath && !isDiff) {
      setEditedContent(value)
      const dirty = value !== originalContentRef.current
      if (dirty) markFileDirty(filePath)
      else clearFileDirty(filePath)
    }
  }

  const fileCategory = filePath ? getFileCategory(filePath) : 'text'

  if (!isDiff && filePath && fileCategory === 'image') {
    return <ImagePreviewView filePath={filePath} cwd={cwd} />
  }

  if (!isDiff && filePath && !forceOpenBinary && (fileCategory === 'binary-likely' || isBinaryRuntime)) {
    return <BinaryFileView filePath={filePath} onOpenAnyway={() => setForceOpenBinary(true)} />
  }

  if (loading) {
    return (
      <div className="flex h-full w-full items-center justify-center gap-2 text-sm text-muted-foreground bg-background">
        <RefreshCw className="h-4 w-4 animate-spin text-primary" />
        Loading file...
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex h-full w-full flex-col items-center justify-center gap-3 p-4 text-center bg-background text-destructive">
        <AlertCircle className="h-8 w-8" />
        <p className="text-sm font-medium">{error}</p>
        <Button variant="outline" size="sm" onClick={loadFile}>
          Retry
        </Button>
      </div>
    )
  }

  const isDarkTheme = !window.document.documentElement.classList.contains('light')

  // Build a stable Monaco URI for the model so it persists across unmounts.
  const monacoPath = filePath ? `file://${filePath}` : undefined

  return (
    <div className="flex h-full w-full flex-col bg-background overflow-hidden">
      {/* Editor Action Header */}
      {!isDiff && filePath && (
        <div className="flex items-center justify-between px-3 py-1.5 border-b border-border bg-muted/10 shrink-0">
          <span className="text-[11px] text-muted-foreground truncate max-w-[80%]">
            {filePath}
          </span>
          <div className="flex items-center gap-1.5">
            {saveStatus === 'success' && (
              <span className="text-[10px] text-emerald-500 flex items-center gap-1">
                <Check className="h-3 w-3" /> Saved
              </span>
            )}
            {saveStatus === 'error' && (
              <span className="text-[10px] text-destructive flex items-center gap-1">
                <AlertCircle className="h-3 w-3" /> Save failed
              </span>
            )}
            {isMarkdown && (
              <div className="flex items-center rounded-md border border-border overflow-hidden">
                <button
                  type="button"
                  onClick={() => setView('editor')}
                  title="Edit source"
                  className={`flex items-center gap-1 px-2 h-6 text-[11px] font-medium transition-colors ${
                    view === 'editor'
                      ? 'bg-primary text-primary-foreground'
                      : 'bg-transparent text-muted-foreground hover:text-foreground hover:bg-muted/40'
                  }`}
                >
                  <Code className="h-3.5 w-3.5" />
                  Edit
                </button>
                <button
                  type="button"
                  onClick={() => setView('preview')}
                  title="Preview rendered markdown"
                  className={`flex items-center gap-1 px-2 h-6 text-[11px] font-medium transition-colors border-l border-border ${
                    view === 'preview'
                      ? 'bg-primary text-primary-foreground'
                      : 'bg-transparent text-muted-foreground hover:text-foreground hover:bg-muted/40'
                  }`}
                >
                  <Eye className="h-3.5 w-3.5" />
                  Preview
                </button>
              </div>
            )}
            <Button
              variant="ghost"
              size="sm"
              className="h-6 px-2 text-[11px] font-medium"
              onClick={() => onOpenDiff?.(filePath)}
              disabled={!filePath || !gitStatuses?.[filePath]?.includes('M')}
              title={gitStatuses?.[filePath]?.includes('M') ? 'View diff' : !gitStatuses ? 'Not a git repository' : 'File is not modified'}
            >
              <GitBranch className="h-3.5 w-3.5 mr-1" />
              Diff
            </Button>
            <Button
              variant={isDirty ? 'default' : 'secondary'}
              size="sm"
              className="h-6 px-2.5 text-[11px] font-medium"
              onClick={handleSave}
              disabled={!isDirty || saving}
            >
              <Save className="h-3.5 w-3.5 mr-1" />
              {saving ? 'Saving...' : 'Save'}
            </Button>
          </div>
        </div>
      )}

      {diskConflict && !isDiff && (
        <div className="flex items-center justify-between gap-2 px-3 py-1.5 border-b border-amber-500/40 bg-amber-500/10 shrink-0">
          <span className="text-[11px] text-amber-400 flex items-center gap-1.5">
            <AlertCircle className="h-3.5 w-3.5" />
            File changed on disk
          </span>
          <div className="flex items-center gap-1.5">
            <Button
              variant="outline"
              size="sm"
              className="h-6 px-2 text-[11px]"
              onClick={handleReloadFromDisk}
            >
              <RefreshCw className="h-3 w-3 mr-1" />
              Reload
            </Button>
            <Button
              variant="ghost"
              size="sm"
              className="h-6 px-2 text-[11px]"
              onClick={() => setDiskConflict(false)}
            >
              Keep mine
            </Button>
          </div>
        </div>
      )}

      {/* Editor view */}
      <div className="flex-1 min-h-0 relative">
        {isDiff && filePath ? (
          <DiffEditor
            height="100%"
            language={getLanguage(filePath)}
            theme={isDarkTheme ? 'caw-dark' : 'light'}
            original={originalContent}
            modified={editedContent}
            beforeMount={defineCawDarkTheme}
            options={{
              readOnly: true,
              fontSize: 12,
              fontFamily: 'JetBrainsMono Nerd Font, Courier New, monospace',
              minimap: { enabled: false },
              scrollbar: { vertical: 'visible', horizontal: 'visible' },
              renderSideBySide: true,
            }}
          />
        ) : isDiff ? (
          // Global git diff text editor view
          <Editor
            height="100%"
            language="diff"
            theme={isDarkTheme ? 'caw-dark' : 'light'}
            beforeMount={defineCawDarkTheme}
            value={content}
            options={{
              readOnly: true,
              fontSize: 12,
              fontFamily: 'JetBrainsMono Nerd Font, Courier New, monospace',
              minimap: { enabled: true },
              scrollbar: { vertical: 'visible', horizontal: 'visible' },
            }}
          />
        ) : initialValue !== undefined ? (
          <div className="absolute inset-0 flex">
            {/* Monaco editor — kept mounted (hidden) when preview is shown so
                the undo stack, cursor and scroll state are preserved. */}
            <div className={view === 'editor' ? 'h-full w-full' : 'hidden'}>
              <Editor
                key={filePath}
                height="100%"
                path={monacoPath}
                language={getLanguage(filePath)}
                theme={isDarkTheme ? 'caw-dark' : 'light'}
                beforeMount={(monaco) => {
                  defineCawDarkTheme(monaco)
                  monacoRef.current = monaco
                }}
                defaultValue={initialValue}
                saveViewState
                keepCurrentModel
                onChange={handleEditorChange}
                onMount={(editor, monaco) => {
                  editorRef.current = editor
                  monacoRef.current = monaco
                }}
                options={{
                  fontSize: 12,
                  fontFamily: 'JetBrainsMono Nerd Font, Courier New, monospace',
                  minimap: { enabled: true },
                  automaticLayout: true,
                  scrollbar: { vertical: 'visible', horizontal: 'visible' },
                }}
              />
            </div>
            {isMarkdown && (
              <div className={view === 'preview' ? 'h-full w-full' : 'hidden'}>
                <MarkdownPreviewView content={editedContent} filePath={filePath!} />
              </div>
            )}
          </div>
        ) : null}
      </div>
    </div>
  )
}