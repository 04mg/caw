import { AlertTriangle, Eye } from 'lucide-react'
import { Button } from '@/components/button'

interface BinaryFileViewProps {
  filePath: string
  onOpenAnyway: () => void
}

export function BinaryFileView({ filePath, onOpenAnyway }: BinaryFileViewProps) {
  const filename = filePath.split('/').pop() ?? filePath
  return (
    <div className="flex h-full w-full flex-col items-center justify-center gap-4 p-8 text-center bg-background">
      <div className="p-4 rounded-full bg-amber-500/10 border border-amber-500/20">
        <AlertTriangle className="h-8 w-8 text-amber-500" />
      </div>
      <div className="flex flex-col items-center gap-1">
        <p className="text-sm font-semibold text-foreground">{filename}</p>
        <p className="text-xs text-muted-foreground max-w-sm">
          This file appears to be a binary file. Opening it in a text editor may display
          garbled content.
        </p>
      </div>
      <Button variant="outline" size="sm" onClick={onOpenAnyway} className="gap-1.5">
        <Eye className="h-3.5 w-3.5" />
        Open Anyway
      </Button>
    </div>
  )
}
