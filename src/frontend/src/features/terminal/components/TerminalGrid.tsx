import { type ReactNode } from 'react'

import { X, Columns2, Rows2 } from 'lucide-react'
import { type LayoutNode } from '@/features/shared/utils/layout'
import { TerminalPanel } from './TerminalPanel'
import { EditorPanel } from '@/features/editor/components/EditorPanel'
import { DesktopPanel } from '@/features/desktop/components/DesktopPanel'
import { destroyTerminal } from '@/features/terminal/services/terminalRegistry'
import { destroyDesktop } from '@/features/desktop/services/desktopRegistry'
import { SplitGroup } from './SplitGroup'



interface TerminalGridProps {
  node: LayoutNode
  activePaneId: string
  onFocus: (paneId: string) => void
  onSplitVert: (paneId: string) => void
  onSplitHoriz: (paneId: string) => void
  onClose: (paneId: string) => void
  cwd: string
  onSizesChange: (splitId: string, sizes: number[]) => void
  gitStatuses?: Record<string, string>
  onOpenDiff?: (filePath?: string) => void
  onOpenFile?: (filePath: string, line?: number, column?: number) => void
}

function childKey(child: LayoutNode): string {
  return child.type === 'leaf' ? child.id : child.type === 'split' ? child.id : 'empty'
}

export function TerminalGrid({
  node,
  activePaneId,
  onFocus,
  onSplitVert,
  onSplitHoriz,
  onClose,
  cwd,
  onSizesChange,
  gitStatuses,
  onOpenDiff,
  onOpenFile,
}: TerminalGridProps): ReactNode {
  if (node.type === 'empty') {
    return null
  }

  if (node.type === 'leaf') {
    const isActive = activePaneId === node.id
    // Resolve the view type. The layout's normalizeLayout fills `view`
    // (terminal/editor/desktop); fall back to the legacy isEditor/filePath
    // heuristic for leaves constructed before the field existed.
    const view = node.view ?? (!!node.filePath || node.isDiff ? 'editor' : 'terminal')
    const isEditor = view === 'editor'
    const isDesktop = view === 'desktop'
    return (
      <div
        className="relative h-full overflow-hidden"
        onClick={() => onFocus(node.id)}
        onPointerDown={() => onFocus(node.id)}
        data-pane-id={node.id}
        data-active={isActive ? 'true' : 'false'}
      >
        {isEditor ? (
          <EditorPanel filePath={node.filePath} isDiff={node.isDiff} cwd={node.cwd || cwd} gitStatuses={gitStatuses} onOpenDiff={onOpenDiff} onOpenFile={onOpenFile} />
        ) : isDesktop ? (
          <DesktopPanel leafId={node.id} cwd={node.cwd || cwd} cmd={node.cmd} env={node.env} isActive={isActive} />
        ) : (
          <TerminalPanel terminalId={node.id} cwd={node.cwd || cwd} cmd={node.cmd} env={node.env} isActive={isActive} />
        )}

        {!isEditor && !isDesktop && (
          <div className="absolute top-1 right-1 z-20 flex gap-0.5 opacity-0 hover:opacity-100 transition-opacity">
            <button
              onClick={(e) => {
                e.stopPropagation()
                if (!isEditor) {
                  if (isDesktop) destroyDesktop(node.id)
                  else destroyTerminal(node.id)
                }
                onClose(node.id)
              }}
              className="h-5 w-5 rounded bg-background/80 text-muted-foreground hover:text-foreground flex items-center justify-center"
              title="Close pane"
              data-testid={`terminal-close-${node.id}`}
            >
              <X className="h-3 w-3" />
            </button>
            <button
              onClick={(e) => { e.stopPropagation(); onSplitHoriz(node.id) }}
              className="h-5 w-5 rounded bg-background/80 text-muted-foreground hover:text-foreground flex items-center justify-center"
              title="Split horizontally"
            >
              <Columns2 className="h-3.5 w-3.5" />
            </button>
            <button
              onClick={(e) => { e.stopPropagation(); onSplitVert(node.id) }}
              className="h-5 w-5 rounded bg-background/80 text-muted-foreground hover:text-foreground flex items-center justify-center"
              title="Split vertically"
            >
              <Rows2 className="h-3.5 w-3.5" />
            </button>
          </div>
        )}
      </div>
    )
  }

  const sizes =
    node.sizes && node.sizes.length === node.children.length
      ? node.sizes
      : node.children.map(() => 100 / node.children.length)

  return (
    <SplitGroup
      splitId={node.id}
      orientation={node.orientation}
      onSizesChange={onSizesChange}
      sizes={sizes}
    >
      {node.children.map((child) => (
        <TerminalGrid
          key={childKey(child)}
          node={child}
          activePaneId={activePaneId}
          onFocus={onFocus}
          onSplitVert={onSplitVert}
          onSplitHoriz={onSplitHoriz}
          onClose={onClose}
          cwd={cwd}
          onSizesChange={onSizesChange}
          gitStatuses={gitStatuses}
          onOpenDiff={onOpenDiff}
          onOpenFile={onOpenFile}
        />
      ))}
    </SplitGroup>
  )
}
