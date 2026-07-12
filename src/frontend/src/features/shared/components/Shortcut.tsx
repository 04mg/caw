const kbd =
  'px-1.5 py-0.5 text-xs font-semibold bg-muted text-muted-foreground rounded border border-border font-mono'

export function Shortcut({ keys, label }: { keys: string; label: string }) {
  return (
    <div className="flex items-center gap-6 text-sm text-muted-foreground">
      <kbd className={kbd}>{keys}</kbd>
      <span>{label}</span>
    </div>
  )
}
