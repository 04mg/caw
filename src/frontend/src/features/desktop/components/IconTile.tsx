import { type ReactNode } from 'react'

export function IconTile({ children, selected, title, onClick, fill }: {
  children: ReactNode
  selected?: boolean
  title: string
  onClick: () => void
  fill?: string
}): ReactNode {
  return (
    <button
      type="button"
      title={title}
      onClick={onClick}
      style={fill ? { color: fill } : undefined}
      className={`cursor-pointer flex items-center justify-center h-8 w-8 rounded-md border transition-colors ${
        selected
          ? 'border-ring bg-accent/50 text-foreground'
          : 'border-transparent text-muted-foreground hover:border-border hover:text-foreground hover:bg-accent/30'
      }`}
    >
      {children}
    </button>
  )
}
