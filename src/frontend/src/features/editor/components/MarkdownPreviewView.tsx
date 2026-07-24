import { useMemo } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeHighlight from 'rehype-highlight'
import 'highlight.js/styles/github-dark.css'

interface MarkdownPreviewViewProps {
  content: string
  filePath: string
}

export function MarkdownPreviewView({ content, filePath }: MarkdownPreviewViewProps) {
  const filename = filePath.split('/').pop() ?? filePath

  const plugins = useMemo(
    () => ({
      remarkPlugins: [remarkGfm],
      rehypePlugins: [[rehypeHighlight, { detect: true, ignoreMissing: true }]],
    }),
    [],
  )

  return (
    <div className="flex h-full w-full flex-col bg-background overflow-hidden">
      {/* Toolbar */}
      <div className="flex items-center gap-1.5 px-3 py-1.5 border-b border-border bg-muted/10 shrink-0">
        <span className="text-[11px] font-mono text-muted-foreground truncate flex-1">
          {filePath}
        </span>
        <span className="text-[11px] text-muted-foreground">{filename}</span>
      </div>
      {/* Markdown body */}
      <div className="flex-1 min-h-0 overflow-auto markdown-body px-8 py-6 text-sm leading-relaxed text-foreground">
        <ReactMarkdown
          remarkPlugins={plugins.remarkPlugins}
          rehypePlugins={plugins.rehypePlugins as never}
          components={{
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
              <h5 className="text-sm font-semibold mt-3 mb-1.5 text-muted-foreground">{children}</h5>
            ),
            h6: ({ children }) => (
              <h6 className="text-xs font-semibold mt-3 mb-1.5 uppercase tracking-wide text-muted-foreground">{children}</h6>
            ),
            p: ({ children }) => <p className="my-3 text-foreground/90">{children}</p>,
            a: ({ children, href }) => (
              <a
                href={href}
                target="_blank"
                rel="noreferrer noopener"
                className="text-primary underline underline-offset-2 hover:text-primary/80"
              >
                {children}
              </a>
            ),
            ul: ({ children }) => <ul className="my-3 pl-6 list-disc space-y-1">{children}</ul>,
            ol: ({ children }) => <ol className="my-3 pl-6 list-decimal space-y-1">{children}</ol>,
            li: ({ children }) => <li className="text-foreground/90">{children}</li>,
            blockquote: ({ children }) => (
              <blockquote className="my-4 pl-4 border-l-4 border-border italic text-muted-foreground">
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
              <pre className="my-4 overflow-x-auto rounded-md border border-border bg-[#0d1117] p-3 text-xs font-mono leading-relaxed">
                {children}
              </pre>
            ),
            code: ({ className, children, ...props }) => {
              const isInline = !className
              if (isInline) {
                return (
                  <code className="rounded bg-muted/60 px-1.5 py-0.5 text-xs font-mono text-foreground">
                    {children}
                  </code>
                )
              }
              return <code className={className} {...props}>{children}</code>
            },
            img: ({ src, alt }) => (
              <img
                src={typeof src === 'string' ? src : undefined}
                alt={alt}
                className="my-4 max-w-full rounded-md border border-border"
              />
            ),
            input: ({ checked, ...props }) => {
              void checked
              return (
                <input
                  type="checkbox"
                  checked={checked}
                  disabled
                  className="mr-2 accent-primary"
                  {...props}
                />
              )
            },
            section: ({ children }) => <section className="my-2">{children}</section>,
          }}
        >
          {content}
        </ReactMarkdown>
      </div>
    </div>
  )
}