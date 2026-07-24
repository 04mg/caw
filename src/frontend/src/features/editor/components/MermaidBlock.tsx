import { memo, useEffect, useRef, useState } from 'react'

// Render a Mermaid diagram. Mermaid is dynamically imported so the heavy
// library is only loaded when a ```mermaid block is present.
// Memoized so parent re-renders (e.g. clicking inside the preview) do not
// trigger a re-render of an unchanged diagram.
export const MermaidBlock = memo(function MermaidBlock({ code, isDark }: { code: string; isDark: boolean }) {
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
      <pre className="my-4 overflow-x-auto text-xs font-mono leading-relaxed text-destructive">
        <code>{code}</code>
      </pre>
    )
  }

  return svg ? (
    <div
      className="my-4 overflow-x-auto flex justify-center [&_svg]:max-w-full"
      dangerouslySetInnerHTML={{ __html: svg }}
    />
  ) : null
})