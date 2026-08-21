import { useEffect, useMemo, useState } from 'react'
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

interface CaptureSource {
  rect: DOMRect
  canvases: HTMLCanvasElement[]
}

/**
 * Composite the live main-area content into a single static image:
 * terminal panes contribute their painted xterm canvases; desktop sessions
 * contribute the xpra client's canvases read same-origin out of the surface
 * layer's iframes. Anything inside a hover-preview portal is excluded so we
 * never capture our own output. Returns a PNG data URL, or null when
 * nothing is rendered. A single broken canvas must never lose the whole
 * capture — every draw is isolated.
 */
function captureActiveWorkspaceSnapshot(): string | null {
  try {
    const sources = collectCaptureSources()
    if (!sources.length) return null

    let left = Infinity
    let top = Infinity
    let right = -Infinity
    let bottom = -Infinity
    for (const s of sources) {
      left = Math.min(left, s.rect.left)
      top = Math.min(top, s.rect.top)
      right = Math.max(right, s.rect.right)
      bottom = Math.max(bottom, s.rect.bottom)
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

    const bg = getComputedStyle(document.body).backgroundColor
    ctx.fillStyle = bg && bg !== 'rgba(0, 0, 0, 0)' ? bg : '#000000'
    ctx.fillRect(0, 0, width, height)

    for (const source of sources) {
      for (const canvas of source.canvases) {
        try {
          // Zero-intrinsic-size canvases (xterm/xpra scratch buffers with
          // layout size but no pixels) make drawImage throw.
          if (canvas.width === 0 || canvas.height === 0) continue
          const rect = canvas.getBoundingClientRect()
          if (rect.width === 0 || rect.height === 0) continue
          ctx.drawImage(canvas, rect.left - left, rect.top - top, rect.width, rect.height)
        } catch {
          // Skip canvases that cannot be drawn (cross-origin or detached).
        }
      }
    }
    return out.toDataURL('image/png')
  } catch (err) {
    console.error('workspace snapshot failed:', err)
    return null
  }
}

// Gather everything worth capturing. Terminal/editor panes are found via
// their data-pane-id wrappers; desktop surfaces via the body-level layer,
// whose visible wrappers are pinned exactly over their pane rects (so
// coordinates inside an iframe viewport match the top-level viewport).
function collectCaptureSources(): CaptureSource[] {
  const sources: CaptureSource[] = []
  const previewRoots = Array.from(document.querySelectorAll<HTMLElement>('[data-testid="workspace-preview"]'))

  for (const pane of document.querySelectorAll<HTMLElement>('div[data-pane-id]')) {
    if (previewRoots.some((root) => root.contains(pane))) continue
    const canvases = Array.from(pane.querySelectorAll<HTMLCanvasElement>('.xterm-screen canvas'))
    if (!canvases.length) continue
    const rect = pane.getBoundingClientRect()
    if (rect.width < 2 || rect.height < 2) continue
    sources.push({ rect, canvases })
  }

  for (const wrapper of document.querySelectorAll<HTMLElement>('#caw-desktop-surface-layer [data-leaf-id]')) {
    if (wrapper.style.visibility !== 'visible') continue
    const doc = wrapper.querySelector('iframe')?.contentDocument
    if (!doc) continue
    const canvases = Array.from(doc.querySelectorAll<HTMLCanvasElement>('canvas')).filter(
      (c) => c.width > 0 && c.height > 0,
    )
    if (!canvases.length) continue
    const rect = wrapper.getBoundingClientRect()
    if (rect.width < 2 || rect.height < 2) continue
    sources.push({ rect, canvases })
  }

  return sources
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
              /* object-contain letterboxes the snapshot instead of cropping
                 it — object-cover made workspaces look zoomed-in whenever
                 their aspect ratio didn't match the thumbnail. */
              <img src={snapshot} alt="" className="h-full w-full bg-black object-contain" draggable={false} />
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
