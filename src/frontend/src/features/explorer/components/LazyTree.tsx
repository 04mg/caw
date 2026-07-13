import { LazyNode } from './LazyNode'

interface LazyTreeProps {
  rootPath: string
  selected: string | null
  onSelect: (path: string) => void
  focusPath: string | null
}

export function LazyTree({ rootPath, selected, onSelect, focusPath }: LazyTreeProps) {
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
      />
    </div>
  )
}