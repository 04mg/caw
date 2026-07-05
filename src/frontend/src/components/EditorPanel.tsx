import { useEffect, useState, useRef } from 'react'
import Editor, { DiffEditor } from '@monaco-editor/react'
import { Save, AlertCircle, RefreshCw, Check } from 'lucide-react'
import { Button } from '@/components/ui/button'

interface EditorPanelProps {
  filePath?: string
  isDiff?: boolean
  cwd: string
  onSaveSuccess?: () => void
}

export function EditorPanel({ filePath, isDiff, cwd, onSaveSuccess }: EditorPanelProps) {
  const [content, setContent] = useState('')
  const [originalContent, setOriginalContent] = useState('')
  const [editedContent, setEditedContent] = useState('')
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [saveStatus, setSaveStatus] = useState<'idle' | 'success' | 'error'>('idle')

  const originalContentRef = useRef('')
  const editorRef = useRef<any>(null)

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

  const loadFile = async () => {
    if (!filePath) {
      if (isDiff) {
        // Load global git diff
        setLoading(true)
        setError(null)
        try {
          const res = await fetch(`/api/git/diff?path=${encodeURIComponent(cwd)}`)
          if (res.ok) {
            const text = await res.text()
            setContent(text)
          } else {
            setError('Failed to load git diff.')
          }
        } catch {
          setError('Network error loading git diff.')
        } finally {
          setLoading(false)
        }
      }
      return
    }

    setLoading(true)
    setError(null)
    setSaveStatus('idle')

    try {
      if (isDiff) {
        // Fetch both original (from git HEAD) and current modified file content
        const [resOrig, resCurr] = await Promise.all([
          fetch(`/api/git/original?path=${encodeURIComponent(filePath)}`),
          fetch(`/api/workspace/file/read?path=${encodeURIComponent(filePath)}`),
        ])

        const origText = resOrig.ok ? await resOrig.text() : ''
        const currText = resCurr.ok ? await resCurr.text() : ''

        setOriginalContent(origText)
        setEditedContent(currText)
      } else {
        // Normal file read
        const res = await fetch(`/api/workspace/file/read?path=${encodeURIComponent(filePath)}`)
        if (res.ok) {
          const text = await res.text()
          setContent(text)
          setEditedContent(text)
          originalContentRef.current = text
        } else {
          setError(`Failed to read file: ${res.statusText}`)
        }
      }
    } catch (err: any) {
      setError(err?.message || 'Error loading file content.')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadFile()
  }, [filePath, isDiff, cwd])

  const handleSave = async () => {
    if (!filePath || isDiff || saving) return
    setSaving(true)
    setSaveStatus('idle')
    try {
      const res = await fetch('/api/workspace/file/write', {
        method: 'POST',
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
  }

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
  }, [editedContent, filePath, isDiff])

  const isDirty = !isDiff && filePath && editedContent !== originalContentRef.current

  const handleEditorChange = (value?: string) => {
    if (value !== undefined) {
      setEditedContent(value)
    }
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

      {/* Editor view */}
      <div className="flex-1 min-h-0 relative">
        {isDiff && filePath ? (
          <DiffEditor
            height="100%"
            language={getLanguage(filePath)}
            theme={isDarkTheme ? 'vs-dark' : 'light'}
            original={originalContent}
            modified={editedContent}
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
            theme={isDarkTheme ? 'vs-dark' : 'light'}
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
            theme={isDarkTheme ? 'vs-dark' : 'light'}
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
