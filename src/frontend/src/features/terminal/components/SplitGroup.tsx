import { type ReactNode, useMemo } from 'react'
import { SplitLayout } from '@/features/shared/components/SplitLayout'

interface SplitGroupProps {
  splitId: string
  orientation: 'horizontal' | 'vertical'
  onSizesChange: (splitId: string, sizes: number[]) => void
  children: ReactNode
  sizes?: number[]
}

export function SplitGroup({
  splitId,
  orientation,
  onSizesChange,
  children,
  sizes,
}: SplitGroupProps): ReactNode {
  const childArray = Array.isArray(children) ? children : [children]
  const count = childArray.length

  const resolvedSizes = useMemo(() => {
    if (sizes && sizes.length === count) return sizes
    return new Array(count).fill(100 / Math.max(count, 1))
  }, [sizes, count])

  return (
    <SplitLayout
      key={splitId}
      orientation={orientation}
      sizes={resolvedSizes}
      onSizesChange={(next) => onSizesChange(splitId, next)}
      className="h-full w-full"
      separatorClassName={
        orientation === 'horizontal'
          ? 'w-px bg-border hover:bg-ring transition-colors cursor-col-resize'
          : 'h-px bg-border hover:bg-ring transition-colors cursor-row-resize'
      }
    >
      {childArray}
    </SplitLayout>
  )
}