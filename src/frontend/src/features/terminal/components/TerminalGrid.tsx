import { type ReactNode, Fragment } from 'react'
import { Panel, Separator } from 'react-resizable-panels'

import { X, Columns2, Rows2 } from 'lucide-react'
import { type LayoutNode } from '@/features/shared/utils/layout'
import { TerminalPanel } from './TerminalPanel'
import { EditorPanel } from '@/features/editor/components/EditorPanel'
import { destroyTerminal } from '@/features/terminal/services/terminalRegistry'
import { SplitGroup } from './SplitGroup'



interface TerminalGridProps {
  node: LayoutNode
  activePaneId: string
  onFocus: (id: string) => void
  onSplitVert: (id: string) => void
  onSplitHoriz: (id: string) => void
  onClose: (id: string) => void
  leafCount: number
  cwd: string
  onSizesChange: (splitId: string, sizes: number[]) => void
  gitStatuses?: Record<string, string>
  onOpenDiff?: (filePath?: string) => void
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
  leafCount,
  cwd,
  onSizesChange,
  gitStatuses,
  onOpenDiff,
}: TerminalGridProps): ReactNode {
  if (node.type === 'empty') {
    return null
  }

  if (node.type === 'leaf') {
    const isActive = activePaneId === node.id
    const isEditor = !!node.filePath || node.isDiff
    return (
      <div
        className={`relative h-full overflow-hidden ${isActive ? 'ring-1 ring-inset ring-border' : ''}`}
        onClick={() => onFocus(node.id)}
        onPointerDown={() => onFocus(node.id)}
      >
        {isEditor ? (
          <EditorPanel filePath={node.filePath} isDiff={node.isDiff} cwd={node.cwd || cwd} gitStatuses={gitStatuses} onOpenDiff={onOpenDiff} />
        ) : (
          <TerminalPanel terminalId={node.id} cwd={node.cwd || cwd} cmd={node.cmd} isActive={isActive} />
        )}

        {!isEditor && (
          <div className="absolute top-1 right-1 z-20 flex gap-0.5 opacity-0 hover:opacity-100 transition-opacity">
            <button
              onClick={(e) => {
                e.stopPropagation()
                if (!isEditor) {
                  destroyTerminal(node.id)
                }
                onClose(node.id)
              }}
              className="h-5 w-5 rounded bg-background/80 text-muted-foreground hover:text-foreground flex items-center justify-center"
              title="Close pane"
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
    >
      {node.children.map((child, i) => (
        <Fragment key={childKey(child)}>
          {i > 0 && (
            <Separator
              className={
                node.orientation === 'horizontal'
                  ? 'w-px bg-border hover:bg-ring transition-colors cursor-col-resize'
                  : 'h-px bg-border hover:bg-ring transition-colors cursor-row-resize'
              }
            />
          )}
          <Panel id={childKey(child)} defaultSize={`${sizes[i]}%`}>
            <TerminalGrid
              node={child}
              activePaneId={activePaneId}
              onFocus={onFocus}
              onSplitVert={onSplitVert}
              onSplitHoriz={onSplitHoriz}
              onClose={onClose}
              leafCount={leafCount}
              cwd={cwd}
              onSizesChange={onSizesChange}
              gitStatuses={gitStatuses}
              onOpenDiff={onOpenDiff}
            />
          </Panel>
        </Fragment>
      ))}
    </SplitGroup>
  )
}

