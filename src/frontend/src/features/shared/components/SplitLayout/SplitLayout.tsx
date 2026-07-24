import {
  type CSSProperties,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { clampSizes } from './clampSizes'
import { resolveSizes, type SizeInput } from './pxToPct'
import { useSplitResize } from './useSplitResize'

export interface SplitLayoutProps {
  orientation: 'horizontal' | 'vertical'
  /** Panel sizes, one entry per direct child. May be percentages (number or
   * "20%") or pixels ("44px"). They need not sum to 100 — the layout is
   * re-clamped on mount and on container resize. */
  sizes: SizeInput[]
  /** Per-panel minimum size. Same unit semantics as `sizes`. Default 0. */
  minSizes?: SizeInput[]
  /** Per-panel maximum size. Same unit semantics as `sizes`. Default 100. */
  maxSizes?: SizeInput[]
  /** Called with the new sizes (in pure percentages, summing to 100) whenever
   * the user drags a separator or the container resizes enough to invalidate
   * the previous clamping. */
  onSizesChange?: (sizes: number[]) => void
  className?: string
  /** Class applied to each separator handle. */
  separatorClassName?: string
  /** Per-separator visibility. A `false` entry hides the separator *before* the
   * panel at that index (i.e. separator i sits between panel i and i+1). Hidden
   * separators render nothing and are not draggable. */
  separatorHidden?: boolean[]
  /** Whether the separators are interactive. Set to false to render a static
   * layout (e.g. when all panels are collapsed/pinned). */
  disabled?: boolean
  style?: CSSProperties
  children: ReactNode[]
}

/**
 * Declarative resizable split layout.
 *
 * Replaces `react-resizable-panels`' `<Group>`/`<Panel>`/`<Separator>` with a
 * single controlled component. The parent owns the `sizes` array and is
 * notified via `onSizesChange` when the user drags a separator. No imperative
 * refs are needed: collapse/expand is driven by changing `sizes` + `minSizes`/
 * `maxSizes`, which removes the entire class of ResizeObserver arity-mismatch
 * bugs that plagued the previous library (issue #691).
 */
export function SplitLayout({
  orientation,
  sizes,
  minSizes = [],
  maxSizes = [],
  onSizesChange,
  className,
  separatorClassName,
  separatorHidden,
  disabled = false,
  style,
  children,
}: SplitLayoutProps) {
  const containerRef = useRef<HTMLDivElement | null>(null)
  const separatorRefs = useRef<Array<HTMLDivElement | null>>([])

  // Measured container size (px) along the split axis. Used to convert px-based
  // constraints to percentages and to re-clamp on resize.
  const [containerPx, setContainerPx] = useState(0)

  // Resolved constraints in pure percentages, recomputed whenever inputs or the
  // container size change.
  const resolvedMin = useMemo(
    () => resolveSizes(pad(minSizes, children.length, 0), containerPx),
    [minSizes, children.length, containerPx],
  )
  const resolvedMax = useMemo(
    () => resolveSizes(pad(maxSizes, children.length, 100), containerPx),
    [maxSizes, children.length, containerPx],
  )
  const resolvedSizes = useMemo(() => {
    const raw = resolveSizes(sizes, containerPx)
    return clampSizes(raw, resolvedMin, resolvedMax)
  }, [sizes, containerPx, resolvedMin, resolvedMax])

  // Keep the parent informed when the effective (clamped) sizes change due to
  // a container resize or constraint change — but ONLY when the clamped values
  // actually differ from what we last emitted. This breaks the feedback loop
  // that would otherwise occur when the parent stores sizes in state: a state
  // update produces a new `sizes` array reference, which recomputes
  // `resolvedSizes` (also a new reference), which would re-fire this effect and
  // re-emit, causing React error #185 (maximum update depth exceeded).
  //
  // Drag interactions emit their own onSizesChange from useSplitResize, so this
  // effect only handles the container-resize / constraint-change re-clamp path.
  const lastEmittedRef = useRef<number[] | null>(null)
  useLayoutEffect(() => {
    // Skip until we have a real container measurement; emitting with
    // containerPx=0 would produce garbage percentages.
    if (containerPx <= 0) return

    const last = lastEmittedRef.current
    if (last && last.length === resolvedSizes.length) {
      let same = true
      for (let i = 0; i < last.length; i++) {
        if (Math.abs(last[i] - resolvedSizes[i]) > 0.01) { same = false; break }
      }
      if (same) return
    }
    lastEmittedRef.current = resolvedSizes
    onSizesChange?.(resolvedSizes)
  }, [resolvedSizes, containerPx, onSizesChange])

  // Measure the container and track resizes.
  useEffect(() => {
    const el = containerRef.current
    if (!el) return
    const measure = () => {
      const rect = el.getBoundingClientRect()
      setContainerPx(orientation === 'horizontal' ? rect.width : rect.height)
    }
    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => ro.disconnect()
  }, [orientation])

  const { onSeparatorPointerDown } = useSplitResize({
    orientation,
    sizes: resolvedSizes,
    minSizes: resolvedMin,
    maxSizes: resolvedMax,
    onSizesChange: onSizesChange ?? noop,
  })

  const isHorizontal = orientation === 'horizontal'

  const handleSeparatorPointerDown = useCallback(
    (index: number) => (e: ReactPointerEvent<HTMLDivElement>) => {
      if (disabled) return
      const container = containerRef.current
      const sep = separatorRefs.current[index]
      if (!container || !sep) return
      onSeparatorPointerDown(index, container, sep)(e)
    },
    [disabled, onSeparatorPointerDown],
  )

  // Render: a flex container; each panel gets flex-basis %, separators are
  // inserted between adjacent panels.
  const childArray = Array.isArray(children) ? children : [children]
  const panelNodes = childArray.slice(0, resolvedSizes.length || undefined)
  const count = panelNodes.length

  const elements: ReactNode[] = []
  for (let i = 0; i < count; i++) {
    if (i > 0) {
      const sepIndex = i - 1
      const hidden = separatorHidden?.[sepIndex] === true
      if (!hidden) {
        // The separator is a wide invisible hit area (so the user doesn't have
        // to point exactly at the 1px line) wrapping a thin visual bar. The
        // visual bar uses the caller's className for color/hover styling; the
        // hit area is transparent and carries the pointer handlers.
        const hitClass = isHorizontal ? 'w-2.5' : 'h-2.5'
        const barClass = isHorizontal
          ? 'w-px h-full'
          : 'h-px w-full'
        elements.push(
          <div
            key={`sep-${sepIndex}`}
            ref={(el) => { separatorRefs.current[sepIndex] = el }}
            onPointerDown={handleSeparatorPointerDown(sepIndex)}
            className={`relative flex items-center justify-center shrink-0 grow-0 ${hitClass}`}
            style={{
              touchAction: 'none',
              cursor: isHorizontal ? 'col-resize' : 'row-resize',
              ...(isHorizontal ? { height: '100%' } : { width: '100%' }),
            }}
          >
            <div className={separatorClassName ? `${separatorClassName} ${barClass}` : barClass} />
          </div>,
        )
      }
    }
    elements.push(
      <div
        key={`panel-${i}`}
        style={{
          flexGrow: 0,
          flexShrink: 0,
          flexBasis: `${resolvedSizes[i]}%`,
          minWidth: 0,
          minHeight: 0,
          overflow: 'hidden',
          ...(isHorizontal ? { height: '100%' } : { width: '100%' }),
        }}
      >
        {panelNodes[i]}
      </div>,
    )
  }

  return (
    <div
      ref={containerRef}
      className={className}
      style={{
        display: 'flex',
        flexDirection: isHorizontal ? 'row' : 'column',
        ...style,
      }}
    >
      {elements}
    </div>
  )
}

function pad(arr: SizeInput[], n: number, fallback: SizeInput): SizeInput[] {
  if (arr.length >= n) return arr.slice(0, n)
  return [...arr, ...new Array(n - arr.length).fill(fallback)]
}

function noop() {}