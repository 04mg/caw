import { useCallback, useEffect, useRef, useState } from 'react'
import { X, Regex, CaseSensitive, ChevronDown, ChevronRight, Loader2, FileText, Check } from 'lucide-react'
import { Button } from '@/components/button'
import { ScrollArea } from '@/components/scroll-area'
import { Highlight } from './SearchHighlight'

export type SearchPanelMode = 'find' | 'replace'

interface SearchHit {
  path: string
  line: number
  column: number
  preview: string
}

interface SearchResponse {
  results: SearchHit[]
  truncated: boolean
}

interface ReplaceResponse {
  files: string[]
  replacements: number
}

interface SearchPanelProps {
  workspacePath: string
  mode: SearchPanelMode
  onOpenFile: (path: string, line?: number, column?: number) => void
  onRefresh: () => void
  onClose: () => void
}

export function SearchPanel({ workspacePath, mode, onOpenFile, onRefresh, onClose }: SearchPanelProps) {
  const [query, setQuery] = useState('')
  const [replace, setReplace] = useState('')
  const [regex, setRegex] = useState(false)
  const [caseSensitive, setCaseSensitive] = useState(false)
  const [results, setResults] = useState<SearchHit[]>([])
  const [truncated, setTruncated] = useState(false)
  const [searching, setSearching] = useState(false)
  const [replacing, setReplacing] = useState(false)
  const [replaced, setReplaced] = useState(false)
  const [collapsedFiles, setCollapsedFiles] = useState<Set<string>>(new Set())
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const findInputRef = useRef<HTMLInputElement>(null)
  const replaceInputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    findInputRef.current?.focus()
  }, [])

  const runSearch = useCallback(
    async (q: string, isRegex: boolean, isCaseSensitive: boolean) => {
      if (!q.trim() || !workspacePath) {
        setResults([])
        setTruncated(false)
        return
      }
      setSearching(true)
      setReplaced(false)
      try {
        const res = await fetch('/api/workspaces/search', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            root: workspacePath,
            query: q,
            regex: isRegex,
            caseSensitive: isCaseSensitive,
          }),
        })
        if (!res.ok) {
          setResults([])
          setTruncated(false)
          return
        }
        const json = (await res.json()) as { data?: SearchResponse } | SearchResponse
        const payload = (json as { data?: SearchResponse })?.data ?? (json as SearchResponse)
        setResults(payload?.results ?? [])
        setTruncated(!!payload?.truncated)
      } catch {
        setResults([])
        setTruncated(false)
      } finally {
        setSearching(false)
      }
    },
    [workspacePath],
  )

  useEffect(() => {
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => {
      runSearch(query, regex, caseSensitive)
    }, 250)
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current)
    }
  }, [query, regex, caseSensitive, runSearch])

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.preventDefault()
      onClose()
    } else if (e.key === 'Enter') {
      e.preventDefault()
      if (e.currentTarget === findInputRef.current) {
        // Enter in the find input runs the search immediately
        if (debounceRef.current) clearTimeout(debounceRef.current)
        runSearch(query, regex, caseSensitive)
      } else {
        // Enter in the replace input triggers replace all
        handleReplaceAll()
      }
    }
  }

  const handleReplaceAll = async () => {
    if (!query.trim() || !workspacePath || replacing) return
    setReplacing(true)
    setReplaced(false)
    try {
      const res = await fetch('/api/workspaces/replace', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          root: workspacePath,
          query,
          replace,
          regex,
          caseSensitive,
        }),
      })
      if (res.ok) {
        const json = (await res.json()) as { data?: ReplaceResponse } | ReplaceResponse
        const payload = (json as { data?: ReplaceResponse })?.data ?? (json as ReplaceResponse)
        setReplaced(true)
        runSearch(query, regex, caseSensitive)
        onRefresh()
        if (payload?.replacements === 0) {
          setTimeout(() => setReplaced(false), 1500)
        } else {
          setTimeout(() => setReplaced(false), 3000)
        }
      }
    } catch {
      /* ignore */
    } finally {
      setReplacing(false)
    }
  }

  const toggleCollapse = (path: string) => {
    setCollapsedFiles((prev) => {
      const next = new Set(prev)
      if (next.has(path)) next.delete(path)
      else next.add(path)
      return next
    })
  }

  const grouped = new Map<string, SearchHit[]>()
  for (const hit of results) {
    const list = grouped.get(hit.path) ?? []
    list.push(hit)
    grouped.set(hit.path, list)
  }

  const totalFiles = grouped.size
  const totalMatches = results.length

  const searchActive = query !== ''

  return (
    <div className="flex flex-col shrink-0 border-b border-border bg-muted/10">
      <div className="flex items-center gap-1 px-3 h-[33px] shrink-0">
        <div className="flex items-center gap-1 min-w-0 flex-1 rounded-md border border-border bg-background px-2 py-1">
          <input
            ref={findInputRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="Search"
            className="flex-1 min-w-0 bg-transparent text-xs text-foreground outline-none placeholder:text-muted-foreground"
            spellCheck={false}
          />
          {searching ? (
            <Loader2 className="h-3 w-3 animate-spin text-muted-foreground shrink-0" />
          ) : (
            <X
              className="h-3 w-3 text-muted-foreground cursor-pointer hover:text-foreground shrink-0"
              onClick={() => setQuery('')}
            />
          )}
        </div>
        <Button
          variant="ghost"
          size="icon"
          className={`h-5 w-5 shrink-0 ${regex ? 'bg-accent text-foreground' : 'text-muted-foreground'}`}
          onClick={() => setRegex((v) => !v)}
          title="Use regular expression"
        >
          <Regex className="h-3 w-3" />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          className={`h-5 w-5 shrink-0 ${caseSensitive ? 'bg-accent text-foreground' : 'text-muted-foreground'}`}
          onClick={() => setCaseSensitive((v) => !v)}
          title="Match case"
        >
          <CaseSensitive className="h-3 w-3" />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          className="h-5 w-5 text-muted-foreground hover:text-foreground shrink-0"
          onClick={onClose}
          title="Close"
        >
          <X className="h-3.5 w-3.5" />
        </Button>
      </div>

      {searchActive && (
        <div className="flex items-center px-3 pb-1">
          <span className="text-[10px] text-muted-foreground tabular-nums">
            {totalFiles} {totalFiles === 1 ? 'file' : 'files'}, {totalMatches} {totalMatches === 1 ? 'match' : 'matches'}
            {truncated ? ' (truncated)' : ''}
          </span>
        </div>
      )}

      {mode === 'replace' && (
        <div className="flex items-center gap-1 px-3 pb-2">
          <div className="flex items-center gap-1 min-w-0 flex-1 rounded-md border border-border bg-background px-2 py-1">
            <input
              ref={replaceInputRef}
              value={replace}
              onChange={(e) => setReplace(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder="Replace"
              className="flex-1 min-w-0 bg-transparent text-xs text-foreground outline-none placeholder:text-muted-foreground"
              spellCheck={false}
            />
          </div>
          <Button
            size="sm"
            className="h-6 px-2 text-[11px] shrink-0"
            onClick={handleReplaceAll}
            disabled={!query.trim() || replacing}
            title="Replace all occurrences"
          >
            {replacing ? (
              <Loader2 className="h-3 w-3 animate-spin mr-1" />
            ) : replaced ? (
              <Check className="h-3 w-3 mr-1" />
            ) : null}
            {replaced ? 'Replaced' : replacing ? 'Replacing...' : 'Replace All'}
          </Button>
        </div>
      )}

      {searchActive && results.length > 0 && (
        <ScrollArea className="max-h-[300px] border-t border-border">
          {[...grouped.entries()].map(([path, hits]) => {
            const collapsed = collapsedFiles.has(path)
            return (
              <div key={path}>
                <button
                  onClick={() => toggleCollapse(path)}
                  className="flex items-center gap-1 w-full px-2.5 py-1 text-left text-[11px] font-medium text-foreground hover:bg-accent/40"
                >
                  {collapsed ? (
                    <ChevronRight className="h-3 w-3 text-muted-foreground shrink-0" />
                  ) : (
                    <ChevronDown className="h-3 w-3 text-muted-foreground shrink-0" />
                  )}
                  <FileText className="h-3 w-3 text-muted-foreground shrink-0" />
                  <span className="truncate">{path}</span>
                  <span className="text-[10px] text-muted-foreground tabular-nums shrink-0">
                    {hits.length}
                  </span>
                </button>
                {!collapsed &&
                  hits.map((hit, i) => (
                    <button
                      key={`${hit.path}-${hit.line}-${i}`}
                      onClick={() => onOpenFile(hit.path, hit.line, hit.column)}
                      className="flex items-start gap-2 w-full px-2.5 py-0.5 text-left text-[11px] text-muted-foreground hover:bg-accent/40"
                    >
                      <span className="w-6 shrink-0 text-right tabular-nums text-muted-foreground/70">
                        {hit.line}
                      </span>
                      <span className="truncate font-mono">
                        <Highlight text={hit.preview} query={query} regex={regex} />
                      </span>
                    </button>
                  ))}
              </div>
            )
          })}
        </ScrollArea>
      )}

      {searchActive && !searching && query !== '' && results.length === 0 && (
        <div className="px-2.5 py-2 text-[11px] text-muted-foreground border-t border-border">
          No results found.
        </div>
      )}
    </div>
  )
}
