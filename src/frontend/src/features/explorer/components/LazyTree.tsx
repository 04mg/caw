import { LazyNode } from './LazyNode'

interface LazyTreeProps {
  rootPath: string
  selected: string | null
  onSelect: (path: string) => void
  focusPath: string | null
  onShowContextMenu?: (path: string, name: string, x: number, y: number) => void
  createTargetPath?: string | null
  onCreateFolder?: (parentPath: string, name: string) => void
  onCreateCancel?: () => void
}

export function LazyTree({
  rootPath,
  selected,
  onSelect,
  focusPath,
  onShowContextMenu,
  createTargetPath,
  onCreateFolder,
  onCreateCancel,
}: LazyTreeProps) {
  return (
    <div className="py-1">
      <LazyNode
        name={rootPath}
        path={rootPath}
        depth={0}
        startExpanded
        selected={selected}
        onSelect={onSelect}
        focusPath={focusPath}
        onShowContextMenu={onShowContextMenu}
        createTargetPath={createTargetPath}
        onCreateFolder={onCreateFolder}
        onCreateCancel={onCreateCancel}
      />
    </div>
  )
}
