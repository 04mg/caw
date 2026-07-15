import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '@/components/dialog'
import { Button } from '@/components/button'

interface RenameConflictDialogProps {
  target: { oldPath: string; newName: string; newPath: string } | null
  onConfirm: () => void
  onCancel: () => void
}

export function RenameConflictDialog({
  target,
  onConfirm,
  onCancel,
}: RenameConflictDialogProps) {
  const targetName = target?.newName || ''
  return (
    <Dialog open={!!target} onOpenChange={(open) => { if (!open) onCancel() }}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>Rename Conflict</DialogTitle>
          <DialogDescription>
            A file or folder named <span className="font-medium text-foreground">{targetName}</span> already exists.
            Renaming will overwrite and delete the existing item. This action is destructive and cannot be undone.
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
