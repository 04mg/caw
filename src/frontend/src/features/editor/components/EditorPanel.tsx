import { useEffect, useState, useRef, useCallback } from 'react'
import Editor, { DiffEditor, type Monaco } from '@monaco-editor/react'
import { Save, AlertCircle, RefreshCw, Check, GitBranch } from 'lucide-react'
import { Button } from '@/components/button'
import { getFileCategory, isBinaryContent } from '../utils/fileType'
import { BinaryFileView } from './BinaryFileView'
import { ImagePreviewView } from './ImagePreviewView'
import { subscribeToFileTree, type FileTreeEvent } from '@/features/explorer/services/fileTreeWs'
import { pathsEqual } from '@/features/shared/utils/path'


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

  // Reset binary override when file changes
  useEffect(() => {
    setForceOpenBinary(false)
    setIsBinaryRuntime(false)
  }, [filePath])

  const originalContentRef = useRef('')
  const editorRef = useRef<any>(null)
  const lastSavedAtRef = useRef(0)
  const debounceTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

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
        // Normal file read
        const res = await fetch(`/api/workspaces/files?path=${encodeURIComponent(filePath)}`)
        if (res.ok) {
          const json = await res.json()
          const text = json?.data?.content ?? ''
          setContent(text)
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
        if (editorRef.current) editorRef.current.setValue(text)
      }
    } catch { /* ignore */ }
  }, [filePath, isDiff])

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
        const dirty = !isDiff && editedContent !== originalContentRef.current
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
  }, [filePath, cwd, isDiff, silentReload, editedContent])

  const handleSave = useCallback(async () => {
    if (!filePath || isDiff || saving) return
    setSaving(true)
    setSaveStatus('idle')
    try {
      const res = await fetch('/api/workspaces/files', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          path: filePath,
          content: editedContent,
        }),
      })

      if (res.ok) {
        originalContentRef.current = editedContent
        setContent(editedContent)
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
  }, [filePath, isDiff, saving, editedContent, onSaveSuccess])

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

  const isDirty = !isDiff && filePath && editedContent !== originalContentRef.current

  const handleEditorChange = (value?: string) => {
    if (value !== undefined) {
      setEditedContent(value)
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

  return (
    <div className="flex h-full w-full flex-col bg-background overflow-hidden">
      {/* Editor Action Header */}
      {!isDiff && filePath && (
        <div className="flex items-center justify-between px-3 py-1.5 border-b border-border bg-muted/10 shrink-0">
          <span className="text-[11px] font-mono text-muted-foreground truncate max-w-[80%]">
            {filePath}
            {isDirty && <span className="ml-1.5 text-amber-500 font-bold">* unsaved</span>}
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
        ) : (
          <Editor
            height="100%"
            language={getLanguage(filePath)}
            theme={isDarkTheme ? 'caw-dark' : 'light'}
            beforeMount={defineCawDarkTheme}
            value={content}
            onChange={handleEditorChange}
            onMount={(editor) => {
              editorRef.current = editor
            }}
            options={{
              fontSize: 12,
              fontFamily: 'JetBrainsMono Nerd Font, Courier New, monospace',
              minimap: { enabled: true },
              automaticLayout: true,
              scrollbar: { vertical: 'visible', horizontal: 'visible' },
            }}
          />
        )}
      </div>
    </div>
  )
}
