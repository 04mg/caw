import { LazyNode } from './LazyNode'

interface LazyTreeProps {
  rootPath: string
  selected: string | null
  onSelect: (path: string) => void
  focusPath: string | null
  refreshCounter?: number
  onShowContextMenu?: (path: string, name: string, x: number, y: number) => void
  createTargetPath?: string | null
  onCreateFolder?: (parentPath: string, name: string) => void
  onCreateCancel?: () => void
  editingPath?: string | null
  onRenameFolder?: (path: string, name: string) => void
  onRenameCancel?: () => void
  onHoverPath?: (path: string) => void
}

export function LazyTree({
  rootPath,
  selected,
  onSelect,
  focusPath,
  refreshCounter,
  onShowContextMenu,
  createTargetPath,
  onCreateFolder,
  onCreateCancel,
  editingPath,
  onRenameFolder,
  onRenameCancel,
  onHoverPath,
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
        refreshCounter={refreshCounter}
        onShowContextMenu={onShowContextMenu}
        createTargetPath={createTargetPath}
        onCreateFolder={onCreateFolder}
        onCreateCancel={onCreateCancel}
        editingPath={editingPath}
        onRenameFolder={onRenameFolder}
        onRenameCancel={onRenameCancel}
        onHoverPath={onHoverPath}
      />
    </div>
  )
}
