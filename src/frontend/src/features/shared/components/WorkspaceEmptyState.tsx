import { type ReactNode } from 'react'

import cawSvg from '@/assets/logo.svg'
import { getCustomization } from '@/features/prefs/stores/prefsStore'
import { Shortcut } from './Shortcut'

// Shared "nothing open" state: the Caw logo plus the keyboard shortcut
// cheat-sheet. Used by the main content area in AppLayout and by workspace
// hover previews when a workspace has no open panes.
export function WorkspaceEmptyState(): ReactNode {
  const logoFilter = getCustomization().logo.filter
  return (
    <div className="flex h-full w-full flex-col items-center justify-center gap-2 text-sm text-muted-foreground">
      <img src={cawSvg} alt="" className="w-[35%] h-auto max-w-[300px]" style={{ filter: logoFilter }} />
      <div className="grid grid-cols-2 gap-x-10 gap-y-3 mt-4">
        <div className="flex flex-col gap-3">
          <Shortcut keys="Alt+→" label="Switch pane" />
          <Shortcut keys="Alt+T" label="New terminal" />
          <Shortcut keys="Alt+W" label="Close pane" />
        </div>
        <div className="flex flex-col gap-3">
          <Shortcut keys="Alt+H" label="Horizontal split" />
          <Shortcut keys="Alt+V" label="Vertical split" />
          <Shortcut keys="Alt+P" label="Command palette" />
        </div>
      </div>
    </div>
  )
}
