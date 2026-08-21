import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '@/components/dialog'
import { Button } from '@/components/button'

export type ConflictOperation = 'create' | 'paste' | 'rename' | 'move' | 'batchMove' | 'batchPaste'

export interface ConflictTarget {
  name: string
  operation: ConflictOperation
  conflictNames?: string[]
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
  move: 'Move Conflict',
  batchMove: 'Move Conflict',
  batchPaste: 'Paste Conflict',
}

const descriptions: Record<ConflictOperation, string> = {
  create: 'already exists. Creating this file will overwrite it.',
  paste: 'already exists in this location. Pasting will overwrite it.',
  rename: 'already exists. Renaming will overwrite it.',
  move: 'already exists in the destination folder. Moving will overwrite it.',
  batchMove: 'already exist in the destination folder. Moving will overwrite them all.',
  batchPaste: 'already exist in this location. Pasting will overwrite them all.',
}

export function ConflictDialog({
  target,
  onConfirm,
  onCancel,
}: ConflictDialogProps) {
  const name = target?.name || ''
  const operation = target?.operation || 'rename'
  const conflictNames = target?.conflictNames
  const isBatch = operation === 'batchMove' || operation === 'batchPaste'
  return (
    <Dialog open={!!target} onOpenChange={(open) => { if (!open) onCancel() }}>
      <DialogContent className="max-w-sm gap-4">
        <DialogHeader>
          <DialogTitle>{titles[operation]}</DialogTitle>
          <DialogDescription>
            {isBatch && conflictNames && conflictNames.length > 0 ? (
              <>
                <span className="font-medium text-foreground">{name}</span>{' '}
                {descriptions[operation]}
                <ul className="mt-2 max-h-28 overflow-auto rounded border border-border bg-muted/40 p-2 text-xs">
                  {conflictNames.map((c) => (
                    <li key={c} className="truncate text-foreground">{c}</li>
                  ))}
                </ul>
              </>
            ) : (
              <>
                <span className="font-medium text-foreground">{name}</span>{' '}
                {descriptions[operation]}
              </>
            )}
          </DialogDescription>
        </DialogHeader>
        <div className="flex justify-end gap-2">
          <Button variant="outline" size="sm" onClick={onCancel}>
            Cancel
          </Button>
          <Button variant="destructive" size="sm" onClick={onConfirm}>
            {isBatch ? 'Replace All' : 'Replace'}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
