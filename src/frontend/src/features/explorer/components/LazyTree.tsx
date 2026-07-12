import { LazyNode } from './LazyNode'

interface LazyTreeProps {
  rootPath: string
  selected: string | null
  onSelect: (path: string) => void
  focusPath: string | null
  scrollContainerRef: React.RefObject<HTMLDivElement | null>
}

export function LazyTree({ rootPath, selected, onSelect, focusPath, scrollContainerRef }: LazyTreeProps) {
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
        scrollContainerRef={scrollContainerRef}
      />
    </div>
  )
}