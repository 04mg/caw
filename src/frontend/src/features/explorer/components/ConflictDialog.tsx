import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '@/components/dialog'
import { Button } from '@/components/button'

export type ConflictOperation = 'create' | 'paste' | 'rename'

export interface ConflictTarget {
  name: string
  operation: ConflictOperation
}

interface ConflictDialogProps {
  target: ConflictTarget | null
  onConfirm: () => void
  onCancel: () => void
}

const titles: Record<ConflictOperation, string> = {
  create: 'File Already Exists',
  paste: 'Paste Conflict',
  rename: 'Rename Conflict',
}

const descriptions: Record<ConflictOperation, string> = {
  create: 'already exists. Creating this file will overwrite it.',
  paste: 'already exists in this location. Pasting will overwrite it.',
  rename: 'already exists. Renaming will overwrite it.',
}

export function ConflictDialog({
  target,
  onConfirm,
  onCancel,
}: ConflictDialogProps) {
  const name = target?.name || ''
  const operation = target?.operation || 'rename'
  return (
    <Dialog open={!!target} onOpenChange={(open) => { if (!open) onCancel() }}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>{titles[operation]}</DialogTitle>
          <DialogDescription>
            <span className="font-medium text-foreground">{name}</span>{' '}
            {descriptions[operation]}
          </DialogDescription>
        </DialogHeader>
        <div className="flex justify-end gap-2">
          <Button variant="outline" size="sm" onClick={onCancel}>
            Cancel
          </Button>
          <Button variant="destructive" size="sm" onClick={onConfirm}>
            Replace
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
