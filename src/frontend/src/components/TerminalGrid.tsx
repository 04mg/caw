import { type ReactNode, Fragment, useCallback, useRef } from 'react'
import { Group, Panel, Separator } from 'react-resizable-panels'
import { X, Columns2, Rows2 } from 'lucide-react'
import { type LayoutNode } from '@/lib/layout'
import { TerminalPanel } from '@/components/TerminalPanel'
import { EditorPanel } from '@/components/EditorPanel'
import { destroyTerminal } from '@/lib/terminalRegistry'

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
          <TerminalPanel terminalId={node.id} cwd={node.cwd || cwd} cmd={node.cmd} />
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
                  ? 'w-[3px] bg-border hover:bg-ring transition-colors cursor-col-resize'
                  : 'h-[3px] bg-border hover:bg-ring transition-colors cursor-row-resize'
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

interface SplitGroupProps {
  splitId: string
  orientation: 'horizontal' | 'vertical'
  onSizesChange: (splitId: string, sizes: number[]) => void
  children: ReactNode
}

function SplitGroup({
  splitId,
  orientation,
  onSizesChange,
  children,
}: SplitGroupProps): ReactNode {
  const childIdsRef = useRef<string[]>([])
  childIdsRef.current = []

  const collectIds = (kids: ReactNode) => {
    const arr = Array.isArray(kids) ? kids : [kids]
    for (const k of arr) {
      if (k && typeof k === 'object' && 'props' in (k as any)) {
        const p = (k as any).props
        const id = p?.id
        if (typeof id === 'string') childIdsRef.current.push(id)
        if (p?.children) collectIds(p.children)
      }
    }
  }
  collectIds(children)

  const handleLayoutChanged = useCallback(
    (layout: Record<string, number>) => {
      const ids = childIdsRef.current
      if (ids.length === 0) return
      const ordered = ids.map((id) => layout[id]).filter((v) => typeof v === 'number')
      if (ordered.length === ids.length) {
        const total = ordered.reduce((a, b) => a + b, 0) || 1
        const normalized = ordered.map((v) => (v / total) * 100)
        onSizesChange(splitId, normalized)
      }
    },
    [splitId, onSizesChange],
  )

  return (
    <Group
      key={splitId}
      orientation={orientation}
      className="h-full w-full"
      onLayoutChanged={handleLayoutChanged}
    >
      {children}
    </Group>
  )
}