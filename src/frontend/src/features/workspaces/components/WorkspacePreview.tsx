import { useEffect, useMemo, useState } from 'react'
import { createPortal } from 'react-dom'

import { TerminalGrid } from '@/features/terminal/components/TerminalGrid'
import { WorkspaceEmptyState } from '@/features/shared/components/WorkspaceEmptyState'
import { countLeaves } from '@/features/shared/utils/layout'
import { getSurface } from '@/features/desktop/services/desktopSurface'
import { ensureTabGroups, findGroupById, collectGroups } from '../utils/tabGroups'
import { type Workspace } from '../types'

// The preview renders the workspace's real UI (terminals / editor panes) at
// full desktop size inside a fixed virtual viewport, then scales it down with
// a CSS transform. Rendering at full size means xterm.js grids and Monaco
// lay out exactly as they would when the workspace is actually opened, and
// the PTY resize lands on a normal desktop grid instead of thumbnail cells.
//
// The currently-open workspace is the exception: its terminals are already
// mounted in the main area, so attaching a second live client to the same
// PTYs would cause conflicts. Instead we composite a static image snapshot
// from the canvases xterm.js has already painted.
const VIRTUAL_WIDTH = 1024
const VIRTUAL_HEIGHT = 640
const THUMB_WIDTH = 320
const SCALE = THUMB_WIDTH / VIRTUAL_WIDTH
const THUMB_HEIGHT = Math.round(VIRTUAL_HEIGHT * SCALE)
const FOOTER_HEIGHT = 26
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
  /** True when this preview targets the workspace already open in the main
   * area; renders an image snapshot instead of a second live terminal grid. */
  isActive: boolean
  emoji: string
  title: string
  anchor: PreviewAnchor
}

/**
 * Composite every visible live terminal canvas in the main content area into
 * a single static image. Panes are located via their `data-pane-id` wrappers;
 * anything inside a hover-preview portal is excluded so we never capture our
 * own output. Desktop panes contribute their xpra client canvases, read
 * same-origin out of the session surface's iframe. Returns a PNG data URL,
 * or null when nothing is rendered.
 */
function captureActiveWorkspaceSnapshot(): string | null {
  const previewRoots = Array.from(document.querySelectorAll<HTMLElement>('[data-testid="workspace-preview"]'))
  const panes = Array.from(document.querySelectorAll<HTMLElement>('div[data-pane-id]'))
  // Deduplicate nested wrappers (TerminalGrid leaf + DesktopPanel root share
  // the same data-pane-id) so panes aren't visited twice.
  const unique = panes.filter((p) => !panes.some((other) => other !== p && other.contains(p)))
  const visible: DOMRect[] = []
  for (const pane of unique) {
    if (previewRoots.some((root) => root.contains(pane))) continue
    const rect = pane.getBoundingClientRect()
    if (rect.width < 2 || rect.height < 2) continue
    if (!hasRenderableContent(pane)) continue
    visible.push(rect)
  }
  if (!visible.length) return null

  let left = Infinity
  let top = Infinity
  let right = -Infinity
  let bottom = -Infinity
  for (const rect of visible) {
    left = Math.min(left, rect.left)
    top = Math.min(top, rect.top)
    right = Math.max(right, rect.right)
    bottom = Math.max(bottom, rect.bottom)
  }
  const width = right - left
  const height = bottom - top

  const out = document.createElement('canvas')
  const dpr = window.devicePixelRatio || 1
  out.width = Math.max(1, Math.round(width * dpr))
  out.height = Math.max(1, Math.round(height * dpr))
  const ctx = out.getContext('2d')
  if (!ctx) return null
  ctx.scale(dpr, dpr)

  const bg = getComputedStyle(unique[0]).backgroundColor
  ctx.fillStyle = bg && bg !== 'rgba(0, 0, 0, 0)' ? bg : '#000000'
  ctx.fillRect(0, 0, width, height)

  for (const pane of unique) {
    if (previewRoots.some((root) => root.contains(pane))) continue
    for (const canvas of collectPaneCanvases(pane)) {
      const rect = canvas.getBoundingClientRect()
      if (rect.width === 0 || rect.height === 0) continue
      ctx.drawImage(canvas, rect.left - left, rect.top - top, rect.width, rect.height)
    }
  }
  return out.toDataURL('image/png')
}

// A pane is worth capturing when it has a painted xterm canvas or a live
// desktop session surface whose iframe document contains canvases.
function hasRenderableContent(pane: HTMLElement): boolean {
  if (pane.querySelector('.xterm-screen canvas')) return true
  const doc = getSurface(pane.dataset.paneId ?? '')?.iframe.contentDocument
  return !!doc && !!doc.querySelector('canvas')
}

// Collect the canvases to draw for a pane: its own xterm canvases plus, for
// desktop panes, the xpra client's canvases inside the same-origin iframe.
// The iframe is pinned exactly over the pane rect, so coordinates inside its
// viewport match the top-level viewport.
function collectPaneCanvases(pane: HTMLElement): HTMLCanvasElement[] {
  const canvases = Array.from(pane.querySelectorAll('canvas'))
  const doc = getSurface(pane.dataset.paneId ?? '')?.iframe.contentDocument
  if (doc) canvases.push(...Array.from(doc.querySelectorAll('canvas')))
  return canvases
}

function noop() {}

export function WorkspacePreview({ workspace, isActive, emoji, title, anchor }: WorkspacePreviewProps) {
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

  // Snapshot the live terminals once when the preview pops in. A frame delay
  // lets the browser finish painting before we read back the canvases.
  const [snapshot, setSnapshot] = useState<string | null>(null)
  useEffect(() => {
    if (!isActive) return
    const raf = requestAnimationFrame(() => {
      setSnapshot(captureActiveWorkspaceSnapshot())
    })
    return () => cancelAnimationFrame(raf)
  }, [isActive])

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
          {isActive ? (
            isEmpty ? (
              <WorkspaceEmptyState />
            ) : snapshot ? (
              <img src={snapshot} alt="" className="h-full w-full object-cover" draggable={false} />
            ) : (
              <div className="flex h-full w-full items-center justify-center">
                <p className="text-sm text-muted-foreground italic">No preview</p>
              </div>
            )
          ) : (
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
                  preview
                />
              ) : (
                <WorkspaceEmptyState />
              )}
            </div>
          )}
        </div>
        {/* Browser-style caption strip: emoji + workspace name. */}
        <div
          className="flex items-center gap-1.5 px-2 border-t border-border bg-secondary/40 select-none"
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
