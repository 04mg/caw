import { useEffect, useRef, useState } from 'react'

// Render a Mermaid diagram. Mermaid is dynamically imported so the heavy
// library is only loaded when a ```mermaid block is present.
export function MermaidBlock({ code, isDark }: { code: string; isDark: boolean }) {
  const [svg, setSvg] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const idRef = useRef(`mermaid-${Math.random().toString(36).slice(2)}`)

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        const mermaid = (await import('mermaid')).default
        mermaid.initialize({
          startOnLoad: false,
          theme: isDark ? 'dark' : 'default',
          securityLevel: 'strict',
          fontFamily: 'inherit',
        })
        const result = await mermaid.render(idRef.current, code)
        if (!cancelled) {
          setSvg(result.svg)
          setError(null)
        }
      } catch (e: any) {
        if (!cancelled) setError(e?.message ?? 'Failed to render diagram')
      }
    })()
    return () => {
      cancelled = true
    }
  }, [code, isDark])

  if (error) {
    return (
      <pre className="my-4 overflow-x-auto rounded-md border border-destructive/40 bg-destructive/5 p-3 text-xs font-mono leading-relaxed text-destructive">
        <code>{code}</code>
      </pre>
    )
  }

  return svg ? (
    <div
      className="my-4 overflow-x-auto rounded-md border border-border bg-background p-4 [&_svg]:max-w-full"
      // mermaid output is our own rendered SVG, not user-controlled HTML
      dangerouslySetInnerHTML={{ __html: svg }}
    />
  ) : (
    <div className="my-4 flex items-center justify-center p-4 text-xs text-muted-foreground border border-border rounded-md">
      Rendering diagram...
    </div>
  )
}