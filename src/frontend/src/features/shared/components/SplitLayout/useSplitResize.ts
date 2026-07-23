import { useCallback, useRef } from 'react'
import { applyDrag, clampSizes } from './clampSizes'
import { resolveSizes, type SizeInput } from './pxToPct'

interface DragState {
  pointerId: number
  index: number
  containerPx: number
  axis: 'x' | 'y'
  startClient: number
}

interface UseSplitResizeArgs {
  orientation: 'horizontal' | 'vertical'
  sizes: SizeInput[]
  minSizes: SizeInput[]
  maxSizes: SizeInput[]
  onSizesChange: (sizes: number[]) => void
}

// Hook that exposes a `onSeparatorPointerDown(i)` factory. The returned handler
// captures the pointer, tracks movement, and emits clamped sizes via
// onSizesChange. Movement is throttled to animation frames for smoothness.
//
// The hook measures the container lazily: it reads the parent element's
// bounding rect at pointerdown and again on container resize (the caller is
// responsible for re-emitting sizes when the container resizes; this hook only
// handles the drag interaction itself).
export function useSplitResize({
  orientation,
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

  const onPointerMove = useCallback(
    (e: PointerEvent) => {
      const drag = dragRef.current
      if (!drag) return
      const deltaPx =
        drag.axis === 'x' ? e.clientX - drag.startClient : e.clientY - drag.startClient
      const deltaPct = drag.containerPx > 0 ? (deltaPx / drag.containerPx) * 100 : 0

      const currentPct = resolveSizes(sizes, drag.containerPx)
      const next = applyDrag(currentPct, resolveSizes(minSizes, drag.containerPx), resolveSizes(maxSizes, drag.containerPx), drag.index, deltaPct)
      pendingPctRef.current = next
      if (rafRef.current == null) {
        rafRef.current = requestAnimationFrame(flush)
      }
    },
    [sizes, minSizes, maxSizes, flush],
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
      const deltaPx =
        drag.axis === 'x' ? e.clientX - drag.startClient : e.clientY - drag.startClient
      const deltaPct = drag.containerPx > 0 ? (deltaPx / drag.containerPx) * 100 : 0
      const currentPct = resolveSizes(sizes, drag.containerPx)
      const next = applyDrag(currentPct, resolveSizes(minSizes, drag.containerPx), resolveSizes(maxSizes, drag.containerPx), drag.index, deltaPct)
      onSizesChange(next)
    },
    [sizes, minSizes, maxSizes, onSizesChange, onPointerMove, clearRaf],
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
      }

      separatorEl.setPointerCapture(e.pointerId)
      window.addEventListener('pointermove', onPointerMove)
      window.addEventListener('pointerup', onPointerUp)
      window.addEventListener('pointercancel', onPointerUp)
    },
    [orientation, onPointerMove, onPointerUp],
  )

  return { onSeparatorPointerDown, dragRef }
}

// Re-export for callers that need to clamp a proposed layout (e.g. when the
// container resizes and the persisted px-based constraints must be re-clamped).
export { clampSizes }