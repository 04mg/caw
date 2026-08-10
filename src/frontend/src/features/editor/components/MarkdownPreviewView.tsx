import { useEffect, useMemo, useState, useCallback, useRef } from 'react'
import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeRaw from 'rehype-raw'
import rehypeSanitize, { defaultSchema } from 'rehype-sanitize'
import rehypeHighlight from 'rehype-highlight'
import 'highlight.js/styles/github-dark.css'
import { MermaidBlock } from './MermaidBlock'

interface MarkdownPreviewViewProps {
  content: string
  filePath: string
  cwd: string
  onOpenFile?: (filePath: string) => void
}

// Allow className on all elements so rehype-highlight can tag <code> with
// language classes after sanitization, and so raw HTML keeps styling.
const sanitizeSchema = {
  ...defaultSchema,
  attributes: {
    ...defaultSchema.attributes,
    '*': [...(defaultSchema.attributes?.['*'] ?? []), 'className'],
    code: [...(defaultSchema.attributes?.['code'] ?? []), 'className'],
    pre: [...(defaultSchema.attributes?.['pre'] ?? []), 'className'],
    img: [...(defaultSchema.attributes?.['img'] ?? []), 'src', 'alt', 'title', 'width', 'height'],
    source: [...(defaultSchema.attributes?.['source'] ?? []), 'src', 'srcset', 'type', 'media'],
    picture: [...(defaultSchema.attributes?.['picture'] ?? [])],
    a: [...(defaultSchema.attributes?.['a'] ?? []), 'href', 'title', 'target', 'rel'],
    td: [...(defaultSchema.attributes?.['td'] ?? []), 'align', 'colSpan', 'rowSpan', 'width', 'valign'],
    th: [...(defaultSchema.attributes?.['th'] ?? []), 'align', 'colSpan', 'rowSpan', 'width', 'valign'],
    tr: [...(defaultSchema.attributes?.['tr'] ?? []), 'align', 'valign'],
    table: [...(defaultSchema.attributes?.['table'] ?? []), 'align', 'width'],
    div: [...(defaultSchema.attributes?.['div'] ?? []), 'className', 'align'],
    span: [...(defaultSchema.attributes?.['span'] ?? []), 'className', 'style'],
    p: [...(defaultSchema.attributes?.['p'] ?? []), 'align'],
  },
  tagNames: [...(defaultSchema.tagNames ?? []), 'picture', 'source'],
}

// Detect dark theme the same way EditorPanel does.
function useIsDark() {
  const [dark, setDark] = useState(!window.document.documentElement.classList.contains('light'))
  useEffect(() => {
    const observer = new MutationObserver(() => {
      setDark(!window.document.documentElement.classList.contains('light'))
    })
    observer.observe(window.document.documentElement, {
      attributes: true,
      attributeFilter: ['class'],
    })
    return () => observer.disconnect()
  }, [])
  return dark
}

// Resolve a relative path (as written in markdown) to an absolute workspace
// path, using the directory of the markdown file as the base.
function resolvePath(rel: string, baseDir: string): string {
  if (/^(https?:|data:|mailto:|tel:|ftp:|#|\/)/.test(rel)) return rel
  // Strip any leading "./"
  const cleaned = rel.replace(/^\.\//, '')
  // Normalize the base dir and split into segments
  const base = baseDir.replace(/\/+$/, '').split('/')
  for (const seg of cleaned.split('/')) {
    if (seg === '..') base.pop()
    else if (seg !== '.') base.push(seg)
  }
  return base.join('/')
}

function joinBase(dir: string, rel: string): string {
  const resolved = resolvePath(rel, dir)
  // If still external, return as-is.
  if (/^(https?:|data:|mailto:|tel:|ftp:|#)/.test(resolved)) return resolved
  // Leading-slash absolute path on the host is treated as absolute fs path.
  if (resolved.startsWith('/')) return resolved
  // Otherwise it's a workspace-relative path; ensure it starts with /.
  return '/' + resolved
}

export function MarkdownPreviewView({ content, filePath, cwd, onOpenFile }: MarkdownPreviewViewProps) {
  const isDark = useIsDark()
  // Directory of the markdown file, used to resolve relative links/images.
  const baseDir = filePath.includes('/') ? filePath.slice(0, filePath.lastIndexOf('/')) : cwd

  // Keep onOpenFile in a ref so handleLinkClick (and therefore the components
  // memo below) never depend on its identity. A parent that passes an inline
  // arrow (creating a new function each render) would otherwise invalidate the
  // components memo on every render, causing ReactMarkdown to unmount/remount
  // the whole subtree and making Mermaid diagrams flash/disappear.
  const onOpenFileRef = useRef(onOpenFile)
  onOpenFileRef.current = onOpenFile

  // Build an inline-serving URL for a (possibly relative) src/srcset path.
  const toInlineUrl = useCallback(
    (raw: string): string => {
      if (!raw) return raw
      if (/^(https?:|data:|mailto:|tel:|ftp:)/.test(raw)) return raw
      const abs = joinBase(baseDir, raw)
      return `/api/workspaces/files?path=${encodeURIComponent(abs)}&inline=true`
    },
    [baseDir],
  )

  // Resolve a (possibly relative) link href to an absolute workspace path.
  const toAbsPath = useCallback(
    (href: string): string => joinBase(baseDir, href),
    [baseDir],
  )

  const handleLinkClick = useCallback(
    (e: React.MouseEvent<HTMLAnchorElement>, href?: string) => {
      if (!href) return
      // External links: let the browser open them in a new tab.
      if (/^(https?:|mailto:|tel:|ftp:)/.test(href)) return
      // Anchor links within the same doc: ignore.
      if (href.startsWith('#')) return
      // Internal/relative link: open in the editor.
      e.preventDefault()
      const abs = toAbsPath(href)
      const fn = onOpenFileRef.current
      if (fn) fn(abs)
    },
    [toAbsPath],
  )

  const plugins = useMemo(
    () => ({
      remarkPlugins: [remarkGfm],
      // Order: parse raw HTML -> sanitize -> highlight code blocks.
      rehypePlugins: [
        rehypeRaw,
        [rehypeSanitize, sanitizeSchema],
        [rehypeHighlight, { detect: true, ignoreMissing: true }],
      ] as never,
    }),
    [],
  )

  // Memoize the components map so its renderer function references stay
  // stable across re-renders. Without this, every parent re-render (e.g. a
  // git-status refresh when any repo file changes, or a click that focuses
  // the pane) hands ReactMarkdown brand-new element types, causing it to
  // unmount/remount the whole subtree — which makes Mermaid diagrams flash
  // and disappear while their async render restarts.
  const components = useMemo<Components>(
    () => ({
      h1: ({ children }) => (
        <h1 className="text-2xl font-bold mt-6 mb-4 pb-2 border-b border-border">{children}</h1>
      ),
      h2: ({ children }) => (
        <h2 className="text-xl font-bold mt-5 mb-3 pb-1 border-b border-border/60">{children}</h2>
      ),
      h3: ({ children }) => (
        <h3 className="text-lg font-semibold mt-4 mb-2">{children}</h3>
      ),
      h4: ({ children }) => (
        <h4 className="text-base font-semibold mt-3 mb-2">{children}</h4>
      ),
      h5: ({ children }) => (
        <h5 className="text-sm font-semibold mt-3 mb-1.5 opacity-70">{children}</h5>
      ),
      h6: ({ children }) => (
        <h6 className="text-xs font-semibold mt-3 mb-1.5 uppercase tracking-wide opacity-70">{children}</h6>
      ),
      p: ({ children, ...props }) => {
        // Map the deprecated align="center" attribute to text-align so
        // raw HTML like <p align="center"> renders centered like GitHub.
        const align = (props as { align?: string }).align
        return (
          <p
            className="my-3"
            style={align ? { textAlign: align as 'left' | 'center' | 'right' | 'justify' } : undefined}
          >
            {children}
          </p>
        )
      },
      a: ({ children, href, ...rest }) => (
        <a
          href={href}
          target={href && /^(https?:|mailto:|tel:|ftp:)/.test(href) ? '_blank' : undefined}
          rel={href && /^(https?:|mailto:|tel:|ftp:)/.test(href) ? 'noreferrer noopener' : undefined}
          onClick={(e) => handleLinkClick(e, href)}
          className="text-blue-500 underline underline-offset-2 hover:opacity-80 cursor-pointer"
          {...rest}
        >
          {children}
        </a>
      ),
      ul: ({ children }) => <ul className="my-3 pl-6 list-disc space-y-1">{children}</ul>,
      ol: ({ children }) => <ol className="my-3 pl-6 list-decimal space-y-1">{children}</ol>,
      li: ({ children }) => <li>{children}</li>,
      blockquote: ({ children }) => (
        <blockquote className="my-4 pl-4 border-l-4 border-border italic opacity-70">
          {children}
        </blockquote>
      ),
      hr: () => <hr className="my-6 border-t border-border" />,
      table: ({ children }) => (
        <div className="my-4 overflow-x-auto">
          <table className="min-w-full border-collapse border border-border text-xs">
            {children}
          </table>
        </div>
      ),
      thead: ({ children }) => <thead className="bg-muted/40">{children}</thead>,
      th: ({ children }) => (
        <th className="border border-border px-3 py-1.5 text-left font-semibold">{children}</th>
      ),
      td: ({ children }) => <td className="border border-border px-3 py-1.5">{children}</td>,
      pre: ({ children }) => (
        <pre className="my-4 overflow-x-auto p-3 text-xs font-mono leading-relaxed">
          {children}
        </pre>
      ),
      code: ({ className, children, ...props }) => {
        const isMermaid = className?.includes('language-mermaid')
        if (isMermaid) {
          const text = String(children).replace(/\n$/, '')
          return <MermaidBlock code={text} isDark={isDark} />
        }
        const isInline = !className
        if (isInline) {
          return (
            <code className="rounded bg-muted/60 px-1.5 py-0.5 text-xs font-mono">
              {children}
            </code>
          )
        }
        return <code className={className} {...props}>{children}</code>
      },
      img: ({ src, alt, ...props }) => {
        const finalSrc = typeof src === 'string' ? toInlineUrl(src) : src
        return (
          <img
            src={finalSrc}
            alt={alt}
            style={{ display: 'inline', verticalAlign: 'middle' }}
            {...props}
          />
        )
      },
      // Raw HTML <img> inside <picture> etc. — same rewriting.
      source: ({ src, srcSet, ...props }) => (
        <source
          src={typeof src === 'string' ? toInlineUrl(src) : src}
          srcSet={typeof srcSet === 'string' ? srcSet.split(',').map((s) => {
            const parts = s.trim().split(/\s+/)
            if (parts[0]) parts[0] = toInlineUrl(parts[0])
            return parts.join(' ')
          }).join(', ') : srcSet}
          {...props}
        />
      ),
      input: ({ checked, ...props }) => {
        void checked
        return (
          <input
            type="checkbox"
            checked={checked}
            disabled
            className="mr-2 accent-blue-500"
            {...props}
          />
        )
      },
      div: ({ children, ...props }) => {
        const align = (props as { align?: string }).align
        return (
          <div style={align ? { textAlign: align as 'left' | 'center' | 'right' | 'justify' } : undefined}>
            {children}
          </div>
        )
      },
      section: ({ children }) => <section className="my-2">{children}</section>,
    }),
    [isDark, toInlineUrl, handleLinkClick],
  )

  return (
    <div
      className="flex h-full w-full flex-col overflow-hidden"
      style={{ background: isDark ? '#000' : '#fff' }}
    >
      <style>{`
        .md-preview-selection ::selection {
          background: ${isDark ? 'rgba(255,255,255,0.25)' : 'rgba(0,0,0,0.18)'};
        }
        .md-preview-selection *::selection {
          background: ${isDark ? 'rgba(255,255,255,0.25)' : 'rgba(0,0,0,0.18)'};
        }
      `}</style>
      {/* Markdown body — text selectable, black/white background */}
      <div
        className="flex-1 min-h-0 overflow-auto px-8 py-6 text-sm leading-relaxed md-preview-selection"
        style={{
          color: isDark ? '#fff' : '#000',
          background: isDark ? '#000' : '#fff',
          userSelect: 'text',
          WebkitUserSelect: 'text',
        }}
      >
        <ReactMarkdown
          remarkPlugins={plugins.remarkPlugins}
          rehypePlugins={plugins.rehypePlugins}
          components={components}
        >
          {content}
        </ReactMarkdown>
      </div>
    </div>
  )
}