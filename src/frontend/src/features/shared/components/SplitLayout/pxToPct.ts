// Convert a size constraint that may be expressed in pixels (string ending with
// "px") or percentage (number, or string ending with "%") into a percentage of
// the given container size (in pixels). A bare number is treated as a
// percentage already and returned unchanged.
//
// Examples (containerPx = 1000):
//   pxToPct(20, 1000)        -> 20   (already a percentage)
//   pxToPct("20%", 1000)     -> 20
//   pxToPct("44px", 1000)    -> 4.4
//   pxToPct("44px", 0)       -> 0   (guard against zero container)

export type SizeInput = number | string

export function pxToPct(value: SizeInput, containerPx: number): number {
  if (typeof value === 'number') return value
  const s = value.trim()
  if (s.endsWith('px')) {
    const px = parseFloat(s)
    if (!Number.isFinite(px)) return 0
    return containerPx > 0 ? (px / containerPx) * 100 : 0
  }
  if (s.endsWith('%')) {
    const pct = parseFloat(s)
    return Number.isFinite(pct) ? pct : 0
  }
  const n = parseFloat(s)
  return Number.isFinite(n) ? n : 0
}

// Resolve an array of size constraints (mixed px/%) into pure percentages,
// using the measured container size.
export function resolveSizes(values: SizeInput[], containerPx: number): number[] {
  return values.map((v) => pxToPct(v, containerPx))
}