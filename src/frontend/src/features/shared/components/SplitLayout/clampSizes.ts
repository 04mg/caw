// Clamp each panel's percentage to its min/max, then redistribute any remainder
// (positive or negative) across the panels that still have headroom. The result
// always sums to 100 as long as the constraints are satisfiable.
//
// minSizes/maxSizes are given in the same unit as sizes (percentages), already
// converted from pixels by the caller when needed. A panel whose min === max is
// considered "fixed" and is never touched by the redistribution loop.

export function clampSizes(
  sizes: number[],
  minSizes: number[],
  maxSizes: number[],
): number[] {
  const n = sizes.length
  if (n === 0) return []

  const mins = minSizes.length === n ? minSizes : new Array(n).fill(0)
  const maxs = maxSizes.length === n ? maxSizes : new Array(n).fill(100)

  // First pass: hard-clamp each panel to its own [min, max].
  const clamped = sizes.map((s, i) => {
    const min = mins[i] ?? 0
    const max = maxs[i] ?? 100
    return Math.max(min, Math.min(max, s))
  })

  // Redistribute the remainder so the total is exactly 100. We iterate a few
  // times because adjusting one panel can push another past its bounds. In
  // practice two passes are enough; we cap at n+2 iterations to avoid infinite
  // loops on unsatisfiable constraint sets.
  for (let iter = 0; iter < n + 2; iter++) {
    const total = clamped.reduce((a, b) => a + b, 0)
    const remainder = 100 - total
    if (Math.abs(remainder) < 1e-6) break

    // Collect panels that can absorb (or give back) space.
    const movable: number[] = []
    for (let i = 0; i < n; i++) {
      if (mins[i] === maxs[i]) continue // fixed panel
      if (remainder > 0 && clamped[i] < maxs[i]) movable.push(i)
      else if (remainder < 0 && clamped[i] > mins[i]) movable.push(i)
    }
    if (movable.length === 0) break

    const share = remainder / movable.length
    let leftover = 0
    for (const i of movable) {
      let next = clamped[i] + share
      if (next > maxs[i]) {
        leftover += next - maxs[i]
        next = maxs[i]
      } else if (next < mins[i]) {
        leftover += next - mins[i]
        next = mins[i]
      }
      clamped[i] = next
    }
    if (Math.abs(leftover) < 1e-6) break
  }

  // Final guard: if constraints are unsatisfiable, scale proportionally to 100.
  const total = clamped.reduce((a, b) => a + b, 0)
  if (total > 0 && Math.abs(total - 100) > 1e-6) {
    const scale = 100 / total
    for (let i = 0; i < n; i++) clamped[i] *= scale
  }

  return clamped
}

// Apply a drag delta (in percentage points) to the pair (i, i+1), conserving
// their sum and respecting min/max. Overflow is NOT propagated to other panels
// — this matches the VS Code / tmux behaviour where dragging one separator
// only affects its two neighbouring panels.
export function applyDrag(
  sizes: number[],
  minSizes: number[],
  maxSizes: number[],
  index: number,
  deltaPct: number,
): number[] {
  const n = sizes.length
  if (index < 0 || index >= n - 1) return sizes

  const mins = minSizes.length === n ? minSizes : new Array(n).fill(0)
  const maxs = maxSizes.length === n ? maxSizes : new Array(n).fill(100)

  const a = index
  const b = index + 1
  let va = sizes[a] + deltaPct
  let vb = sizes[b] - deltaPct

  // Clamp the pair, preserving their combined size.
  const pairSum = sizes[a] + sizes[b]
  const minA = mins[a]
  const maxA = maxs[a]
  const minB = mins[b]
  const maxB = maxs[b]

  if (va < minA) {
    vb -= minA - va
    va = minA
  } else if (va > maxA) {
    vb -= maxA - va
    va = maxA
  }
  if (vb < minB) {
    va -= minB - vb
    vb = minB
  } else if (vb > maxB) {
    va -= maxB - vb
    vb = maxB
  }

  // If after clamping the pair no longer sums to its original total, the
  // remainder is lost (panel hit a wall). That is the expected behaviour.
  void pairSum

  const next = sizes.slice()
  next[a] = va
  next[b] = vb
  return next
}