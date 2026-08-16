import { useCallback, useRef } from 'react'
import { applyDrag } from './clampSizes'
import { resolveSizes, type SizeInput } from './pxToPct'

interface DragState {
  pointerId: number
  index: number
  containerPx: number
  axis: 'x' | 'y'
  startClient: number
  // Snapshot of the sizes/constraints (resolved to % of the container at the
  // moment of pointerdown). The cumulative drag delta is applied to this
  // baseline, NOT to the live `sizes` prop, so that state round-trips between
  // pointermove frames don't compound the delta.
  startSizesPct: number[]
  startMinPct: number[]
  startMaxPct: number[]
}

interface UseSplitResizeArgs {
  orientation: 'horizontal' | 'vertical'
  reverse?: boolean
  sizes: SizeInput[]
  minSizes: SizeInput[]
  maxSizes: SizeInput[]
  onSizesChange: (sizes: number[]) => void
}

// Hook that exposes a `onSeparatorPointerDown(i)` factory. The returned handler
// captures the pointer, tracks movement, and emits clamped sizes via
// onSizesChange. Movement is throttled to animation frames for smoothness.
//
// The sizes/constraints are snapshotted at pointerdown (resolved to % against
// the container measured at that instant) so the cumulative delta is applied to
// a stable baseline. This avoids compounding the delta when the parent stores
// sizes in state and feeds the new array back into the hook between frames.
export function useSplitResize({
  orientation,
  reverse = false,
  sizes,
  minSizes,
  maxSizes,
  onSizesChange,
}: UseSplitResizeArgs) {
  const dragRef = useRef<DragState | null>(null)
  const rafRef = useRef<number | null>(null)
  const pendingPctRef = useRef<number[] | null>(null)

  const clearRaf = useCallback(() => {
    if (rafRef.current != null) {
      cancelAnimationFrame(rafRef.current)
      rafRef.current = null
    }
  }, [])

  const flush = useCallback(() => {
    rafRef.current = null
    const pct = pendingPctRef.current
    if (pct) {
      pendingPctRef.current = null
      onSizesChange(pct)
    }
  }, [onSizesChange])

  const computeNext = useCallback(
    (drag: DragState, clientPos: number): number[] => {
      const deltaPx = drag.axis === 'x' ? clientPos - drag.startClient : clientPos - drag.startClient
      const direction = drag.axis === 'x' && reverse ? -1 : 1
      const deltaPct = drag.containerPx > 0 ? (deltaPx / drag.containerPx) * 100 * direction : 0
      return applyDrag(drag.startSizesPct, drag.startMinPct, drag.startMaxPct, drag.index, deltaPct)
    },
    [reverse],
  )

  const onPointerMove = useCallback(
    (e: PointerEvent) => {
      const drag = dragRef.current
      if (!drag) return
      const next = computeNext(drag, drag.axis === 'x' ? e.clientX : e.clientY)
      pendingPctRef.current = next
      if (rafRef.current == null) {
        rafRef.current = requestAnimationFrame(flush)
      }
    },
    [computeNext, flush],
  )

  const onPointerUp = useCallback(
    (e: PointerEvent) => {
      const drag = dragRef.current
      if (!drag) return
      const el = e.target as HTMLElement | null
      el?.releasePointerCapture?.(drag.pointerId)
      window.removeEventListener('pointermove', onPointerMove)
      window.removeEventListener('pointerup', onPointerUp)
      window.removeEventListener('pointercancel', onPointerUp)
      dragRef.current = null
      clearRaf()
      // Emit final sizes synchronously so persistence sees the last position.
      onSizesChange(computeNext(drag, drag.axis === 'x' ? e.clientX : e.clientY))
    },
    [onPointerMove, clearRaf, onSizesChange, computeNext],
  )

  const onSeparatorPointerDown = useCallback(
    (index: number, containerEl: HTMLElement, separatorEl: HTMLElement) => (e: React.PointerEvent) => {
      // Only react to primary button drags.
      if (e.button !== 0) return
      e.preventDefault()
      e.stopPropagation()

      const rect = containerEl.getBoundingClientRect()
      const containerPx =
        orientation === 'horizontal' ? rect.width : rect.height

      const axis = orientation === 'horizontal' ? 'x' : 'y'
      const startClient = orientation === 'horizontal' ? e.clientX : e.clientY

      dragRef.current = {
        pointerId: e.pointerId,
        index,
        containerPx,
        axis,
        startClient,
        startSizesPct: resolveSizes(sizes, containerPx),
        startMinPct: resolveSizes(minSizes, containerPx),
        startMaxPct: resolveSizes(maxSizes, containerPx),
      }

      separatorEl.setPointerCapture(e.pointerId)
      window.addEventListener('pointermove', onPointerMove)
      window.addEventListener('pointerup', onPointerUp)
      window.addEventListener('pointercancel', onPointerUp)
    },
    [orientation, reverse, sizes, minSizes, maxSizes, onPointerMove, onPointerUp],
  )

  return { onSeparatorPointerDown, dragRef }
}