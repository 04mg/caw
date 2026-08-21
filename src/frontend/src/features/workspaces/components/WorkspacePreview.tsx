import { useMemo } from 'react'
import { createPortal } from 'react-dom'

import { TerminalGrid } from '@/features/terminal/components/TerminalGrid'
import { ensureTabGroups, findGroupById, collectGroups } from '../utils/tabGroups'
import { type Workspace } from '../types'

// The preview renders the workspace's real UI (terminals / editor panes) at
// full desktop size inside a fixed virtual viewport, then scales it down with
// a CSS transform. Rendering at full size means xterm.js grids and Monaco
// lay out exactly as they would when the workspace is actually opened, and
// the PTY resize lands on a normal desktop grid instead of thumbnail cells.
const VIRTUAL_WIDTH = 1024
const VIRTUAL_HEIGHT = 640
const THUMB_WIDTH = 320
const SCALE = THUMB_WIDTH / VIRTUAL_WIDTH
const THUMB_HEIGHT = Math.round(VIRTUAL_HEIGHT * SCALE)
const GAP = 8
const VIEWPORT_MARGIN = 8

export interface PreviewAnchor {
  /** Viewport Y of the hovered row's top edge. */
  top: number
  /** Sidebar edge X the preview anchors against (row right edge for a
   * left-hand sidebar, row left edge for a right-hand one). */
  edge: number
  /** Which side of the sidebar the thumbnail appears on. */
  side: 'left' | 'right'
}

interface WorkspacePreviewProps {
  workspace: Workspace
  anchor: PreviewAnchor
}

function noop() {}

export function WorkspacePreview({ workspace, anchor }: WorkspacePreviewProps) {
  // Resolve the tab the workspace would open on: the active tab of its
  // active tab group, mirroring AppLayout's selection logic.
  const activeTab = useMemo(() => {
    if (!workspace.layouts.length) return null
    const { tree, activeGroupId } = ensureTabGroups(workspace)
    const group = findGroupById(tree, activeGroupId) ?? collectGroups(tree)[0] ?? null
    const tabId = group
      ? group.tabs[group.activeTabIndex]
      : workspace.layouts[workspace.activeTabIndex]?.id
    return workspace.layouts.find((l) => l.id === tabId) ?? null
  }, [workspace])

  const left = anchor.side === 'right'
    ? anchor.edge + GAP
    : anchor.edge - GAP - THUMB_WIDTH
  const maxTop = Math.max(VIEWPORT_MARGIN, window.innerHeight - THUMB_HEIGHT - VIEWPORT_MARGIN)
  const top = Math.min(Math.max(VIEWPORT_MARGIN, anchor.top), maxTop)

  return createPortal(
    <div
      className="fixed z-50"
      style={{ left, top }}
      data-testid="workspace-preview"
    >
      <div
        className="rounded-lg border border-border bg-background shadow-xl overflow-hidden"
        style={{ width: THUMB_WIDTH, height: THUMB_HEIGHT }}
        inert
      >
        <div
          style={{
            width: VIRTUAL_WIDTH,
            height: VIRTUAL_HEIGHT,
            transform: `scale(${SCALE})`,
            transformOrigin: 'top left',
            pointerEvents: 'none',
          }}
        >
          {activeTab ? (
            <TerminalGrid
              node={activeTab.layout}
              activePaneId=""
              onFocus={noop}
              onSplitVert={noop}
              onSplitHoriz={noop}
              onClose={noop}
              cwd={workspace.path}
              onSizesChange={noop}
            />
          ) : (
            <div className="flex h-full w-full items-center justify-center">
              <p className="text-sm text-muted-foreground italic">Empty workspace</p>
            </div>
          )}
        </div>
      </div>
    </div>,
    document.body,
  )
}
