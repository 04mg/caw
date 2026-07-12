import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '@/components/dialog'
import { Button } from '@/components/button'


interface DeleteDialogProps {
  target: { path: string; name: string; isDir: boolean } | null
  onConfirm: () => void
  onCancel: () => void
}

export function DeleteDialog({
  target,
  onConfirm,
  onCancel,
}: DeleteDialogProps) {
  return (
    <Dialog open={!!target} onOpenChange={(open) => { if (!open) onCancel() }}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>Delete {target?.isDir ? 'Folder' : 'File'}</DialogTitle>
          <DialogDescription>
            Are you sure you want to delete <span className="font-medium text-foreground">{target?.name}</span>?
            {target?.isDir && <span className="block mt-1">All contents inside will be permanently removed.</span>}
          </DialogDescription>
        </DialogHeader>
        <div className="flex justify-end gap-2">
          <Button variant="outline" size="sm" onClick={onCancel}>
            Cancel
          </Button>
          <Button variant="destructive" size="sm" onClick={onConfirm}>
            Delete
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
