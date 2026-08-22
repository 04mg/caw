import { cn } from '@/features/shared/utils/utils'

interface CircleButtonProps {
  children: React.ReactNode
  label: string
  disabled?: boolean
  active?: boolean
  highlight?: boolean
  onClick: () => void
}

export function CircleButton({ children, label, disabled, active, highlight, onClick }: CircleButtonProps) {
  return (
    <button
      title={label}
      aria-label={label}
      disabled={disabled}
      onClick={(e) => {
        e.stopPropagation()
        onClick()
      }}
      onMouseDown={(e) => e.stopPropagation()}
      className={cn(
        'flex items-center justify-center rounded-full border transition-all',
        'disabled:opacity-40 disabled:cursor-not-allowed disabled:pointer-events-none',
        highlight
          ? 'bg-primary text-primary-foreground border-primary shadow-sm'
          : active
            ? 'bg-accent text-foreground border-ring'
            : 'bg-secondary/80 text-muted-foreground border-border/60 hover:text-foreground hover:border-border',
      )}
      style={{ width: 26, height: 26 }}
    >
      {children}
    </button>
  )
}