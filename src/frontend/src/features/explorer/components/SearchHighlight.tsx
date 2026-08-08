export function Highlight({ text, query, regex }: { text: string; query: string; regex: boolean }) {
  if (!query) {
    return <>{text}</>
  }
  try {
    const pattern = regex ? query : query.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    const re = new RegExp(`(${pattern})`, 'gi')
    const parts = text.split(re)
    if (parts.length === 1) {
      return <>{text}</>
    }
    return (
      <>
        {parts.map((part, i) =>
          i % 2 === 1 ? (
            <mark key={i} className="bg-yellow-500/40 text-foreground rounded-[2px] px-0.5">{part}</mark>
          ) : (
            <span key={i}>{part}</span>
          ),
        )}
      </>
    )
  } catch {
    return <>{text}</>
  }
}
