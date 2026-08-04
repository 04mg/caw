import { AlertCircle, Check } from 'lucide-react'

export type SaveStatus = 'idle' | 'success' | 'error'

interface SaveToastProps {
  status: SaveStatus
}

// Floating save feedback that fades in/out at the bottom of the settings
// content area. It overlays content (pointer-events-none) so it never shifts
// the layout, and stays mounted so only opacity animates.
export function SaveToast({ status }: SaveToastProps) {
  const visible = status !== 'idle'
  const isError = status === 'error'
  return (
    <div
      data-state={visible ? 'visible' : 'hidden'}
      className={`pointer-events-none absolute inset-x-0 bottom-0 z-10 flex justify-center pb-6 pt-10 transition-opacity duration-300 ${
        isError
          ? 'bg-gradient-to-t from-red-500/15 via-red-500/5 to-transparent'
          : 'bg-gradient-to-t from-emerald-500/15 via-emerald-500/5 to-transparent'
      } data-[state=hidden]:opacity-0 data-[state=visible]:opacity-100`}
      aria-live="polite"
    >
      <span
        className={`inline-flex items-center gap-1.5 rounded-full border bg-background/80 px-3 py-1 text-[11px] font-medium shadow-sm backdrop-blur-sm ${
          isError ? 'border-destructive/20 text-destructive' : 'border-emerald-500/20 text-emerald-500'
        }`}
      >
        {isError ? <AlertCircle className="h-3 w-3" /> : <Check className="h-3 w-3" />}
        {isError ? 'Save failed' : 'Saved'}
      </span>
    </div>
  )
}
