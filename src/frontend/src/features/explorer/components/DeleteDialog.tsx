import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '@/components/dialog'
import { Button } from '@/components/button'


export type DeleteDialogTarget =
  | { paths: string[]; name?: string; isDir: boolean }
  | { path: string; name: string; isDir: boolean }

interface DeleteDialogProps {
  target: DeleteDialogTarget | null
  onConfirm: () => void
  onCancel: () => void
}

export function DeleteDialog({
  target,
  onConfirm,
  onCancel,
}: DeleteDialogProps) {
  const paths = target ? ('paths' in target ? target.paths : [target.path]) : []
  const count = paths.length
  const isMulti = count > 1
  const name = target ? ('name' in target ? target.name : undefined) : undefined
  const isDir = target?.isDir ?? false
  return (
    <Dialog open={!!target} onOpenChange={(open) => { if (!open) onCancel() }}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>
            {isMulti ? `Delete ${count} items` : `Delete ${isDir ? 'Folder' : 'File'}`}
          </DialogTitle>
          <DialogDescription>
            {isMulti ? (
              <>
                Are you sure you want to delete the {count} selected items?
                <span className="block mt-1">Folders and all their contents will be permanently removed.</span>
              </>
            ) : (
              <>
                Are you sure you want to delete <span className="font-medium text-foreground">{name}</span>?
                {isDir && <span className="block mt-1">All contents inside will be permanently removed.</span>}
              </>
            )}
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
