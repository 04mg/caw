import { useMemo } from 'react'
import { createPortal } from 'react-dom'

import { TerminalGrid } from '@/features/terminal/components/TerminalGrid'
import { WorkspaceEmptyState } from '@/features/shared/components/WorkspaceEmptyState'
import { countLeaves } from '@/features/shared/utils/layout'
import { ensureTabGroups, findGroupById, collectGroups } from '../utils/tabGroups'
import { type Workspace } from '../types'

// The preview renders the workspace's real UI (terminals / editor panes) at
// full desktop size inside a fixed virtual viewport, then scales it down with
// a CSS transform. Rendering at full size means xterm.js grids and Monaco
// lay out exactly as they would when the workspace is actually opened, and
// the PTY resize lands on a normal desktop grid instead of thumbnail cells.
//
// The currently-open workspace gets no thumbnail at all: its terminals are
// already visible in the main area, so a preview would only duplicate them.
const VIRTUAL_WIDTH = 1024
const VIRTUAL_HEIGHT = 640
const THUMB_WIDTH = 320
const SCALE = THUMB_WIDTH / VIRTUAL_WIDTH
const THUMB_HEIGHT = Math.round(VIRTUAL_HEIGHT * SCALE)
const FOOTER_HEIGHT = 36
const PREVIEW_HEIGHT = THUMB_HEIGHT + FOOTER_HEIGHT
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
  emoji: string
  title: string
  anchor: PreviewAnchor
}

function noop() {}

export function WorkspacePreview({ workspace, emoji, title, anchor }: WorkspacePreviewProps) {
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

  // The workspace has nothing to show when its active tab is missing or
  // holds no panes — render the shared empty state instead of a blank tile.
  const isEmpty = !activeTab || countLeaves(activeTab.layout) === 0

  const left = anchor.side === 'right'
    ? anchor.edge + GAP
    : anchor.edge - GAP - THUMB_WIDTH
  const maxTop = Math.max(VIEWPORT_MARGIN, window.innerHeight - PREVIEW_HEIGHT - VIEWPORT_MARGIN)
  const top = Math.min(Math.max(VIEWPORT_MARGIN, anchor.top), maxTop)

  return createPortal(
    <div
      className="fixed z-50"
      style={{ left, top }}
      data-testid="workspace-preview"
    >
      <div
        className="rounded-lg border border-border bg-background shadow-xl overflow-hidden"
        style={{ width: THUMB_WIDTH }}
        inert
      >
        <div className="overflow-hidden relative" style={{ width: THUMB_WIDTH, height: THUMB_HEIGHT }}>
          <div
            style={{
              width: VIRTUAL_WIDTH,
              height: VIRTUAL_HEIGHT,
              transform: `scale(${SCALE})`,
              transformOrigin: 'top left',
              pointerEvents: 'none',
            }}
          >
            {isEmpty ? (
              <WorkspaceEmptyState />
            ) : (
              <TerminalGrid
                node={activeTab!.layout}
                activePaneId=""
                onFocus={noop}
                onSplitVert={noop}
                onSplitHoriz={noop}
                onClose={noop}
                cwd={workspace.path}
                onSizesChange={noop}
                preview
              />
            )}
          </div>
        </div>
        {/* Browser-style caption strip: emoji + workspace name. */}
        <div
          className="flex items-center gap-1.5 px-2.5 py-2 border-t border-border bg-secondary/40 select-none"
          style={{ height: FOOTER_HEIGHT }}
        >
          <span className="text-xs leading-none shrink-0">{emoji}</span>
          <span className="text-xs text-muted-foreground truncate">{title}</span>
        </div>
      </div>
    </div>,
    document.body,
  )
}
