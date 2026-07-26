import { useState, useEffect } from 'react'
import { Check } from 'lucide-react'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '@/components/dialog'
import { Input } from '@/components/input'

import { EmojiPicker } from 'frimousse'

interface WorkspaceEditDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  initialName: string
  initialEmoji: string
  onSave: (name: string, emoji: string) => void
}

export function WorkspaceEditDialog({ open, onOpenChange, initialName, initialEmoji, onSave }: WorkspaceEditDialogProps) {
  const [name, setName] = useState(initialName)
  const [emoji, setEmoji] = useState(initialEmoji)

  useEffect(() => {
    if (open) {
      setName(initialName)
      setEmoji(initialEmoji)
    }
  }, [open, initialName, initialEmoji])

  const confirm = () => {
    if (name.trim()) onSave(name.trim(), emoji)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>Edit workspace</DialogTitle>
          <DialogDescription>
            Update the name and emoji for this workspace.
          </DialogDescription>
        </DialogHeader>

        <div className="flex gap-4 py-2 items-start">
          <div className="flex flex-col gap-1.5 shrink-0">
            <label className="text-xs text-muted-foreground font-medium">Emoji</label>
            <div className="h-9 w-9 rounded-md border border-border flex items-center justify-center text-2xl shrink-0 cursor-pointer">
              {emoji}
            </div>
          </div>

          <div className="flex-1 flex flex-col gap-1.5 min-w-0">
            <label className="text-xs text-muted-foreground font-medium">Name</label>
            <Input
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Workspace name"
              className="w-full"
            />
          </div>

          <div className="flex flex-col gap-1.5 shrink-0">
            <label className="text-xs text-muted-foreground font-medium">Emoji</label>
            <div className="rounded-md border border-border overflow-hidden">
              <EmojiPicker.Root
                onEmojiSelect={({ emoji }) => setEmoji(emoji)}
                className="isolate flex h-[240px] flex-col bg-background"
                columns={6}
              >
                <EmojiPicker.Search className="z-10 mx-2 mt-2 mb-1 appearance-none rounded-md bg-secondary px-2.5 py-1.5 text-sm text-foreground placeholder-muted-foreground outline-hidden" />
                <EmojiPicker.Viewport className="relative flex-1 outline-hidden">
                  <EmojiPicker.Loading className="absolute inset-0 flex items-center justify-center text-xs text-muted-foreground">
                    Loading…
                  </EmojiPicker.Loading>
                  <EmojiPicker.Empty className="absolute inset-0 flex items-center justify-center text-xs text-muted-foreground">
                    No emoji found.
                  </EmojiPicker.Empty>
                  <EmojiPicker.List
                    className="select-none pb-1"
                    components={{
                      CategoryHeader: ({ category, ...props }) => (
                        <div
                          className="bg-background px-3 pt-2 pb-1 text-xs font-medium text-muted-foreground"
                          {...props}
                        >
                          {category.label}
                        </div>
                      ),
                      Row: ({ children, ...props }) => (
                        <div className="scroll-my-1 px-1 w-full flex" {...props}>{children}</div>
                      ),
                      Emoji: ({ emoji, ...props }) => (
                        <button
                          className="flex items-center justify-center rounded-md text-lg data-[active]:bg-accent aspect-square min-w-0"
                          style={{ width: 'calc(100% / 6)' }}
                          {...props}
                        >
                          {emoji.emoji}
                        </button>
                      ),
                    }}
                  />
                </EmojiPicker.Viewport>
              </EmojiPicker.Root>
            </div>
          </div>
        </div>

        <div className="flex items-center justify-end gap-2 shrink-0">
          <button
            onClick={() => onOpenChange(false)}
            className="px-3 py-1.5 text-xs rounded-md border border-border hover:bg-accent/40"
          >
            Cancel
          </button>
          <button
            onClick={confirm}
            disabled={!name.trim()}
            className="px-3 py-1.5 text-xs rounded-md bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50 flex items-center gap-1"
          >
            <Check className="h-3.5 w-3.5" />
            Save
          </button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
