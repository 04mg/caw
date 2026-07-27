/**
 * Pure type-ahead matching over a flat list of named items (e.g. rendered
 * tree node buttons). Selection-only: does not expand or filter the list.
 */

export interface NamedItem {
  name: string
  path: string
}

/**
 * Type-ahead match: returns the index to select for a given query.
 *
 * Semantics (Finder / VS Code style):
 * - Multi-char query extension: if the current item still matches, stay
 *   (so `p` → `pr` → `pro` narrows on the same item without jumping).
 * - Single-char query: always scan forward (cycle Documents → Downloads → ...).
 * - Otherwise scan forward for the next prefix match (wrap).
 * - Fall back to substring matches the same way.
 * Returns -1 if nothing matches.
 */
export function findTypeaheadIndex(
  items: NamedItem[],
  query: string,
  currentIndex: number,
): number {
  const q = query.toLowerCase()
  if (!q || items.length === 0) return -1

  const cur = currentIndex >= 0 ? currentIndex : -1

  // 1) Multi-char extension: stay on current if it still prefix-matches.
  if (q.length > 1 && cur >= 0 && items[cur].name.toLowerCase().startsWith(q)) {
    return cur
  }

  // 2) Scan forward from current for the next prefix match (wrap).
  for (let offset = 1; offset <= items.length; offset++) {
    const i = (cur + offset) % items.length
    if (items[i].name.toLowerCase().startsWith(q)) return i
  }

  // 3) Multi-char extension: stay on current if it still substring-matches.
  if (q.length > 1 && cur >= 0 && items[cur].name.toLowerCase().includes(q)) {
    return cur
  }

  // 4) Fall back to substring matches scanning forward.
  for (let offset = 1; offset <= items.length; offset++) {
    const i = (cur + offset) % items.length
    if (items[i].name.toLowerCase().includes(q)) return i
  }

  return -1
}

/**
 * Next/previous index for arrow navigation (wraps).
 */
export function stepIndex(currentIndex: number, length: number, delta: 1 | -1): number {
  if (length <= 0) return -1
  if (currentIndex < 0) return delta === 1 ? 0 : length - 1
  return (currentIndex + delta + length) % length
}

/**
 * Append a character to the type-ahead buffer.
 * Pure: returns the new buffer string (caller owns timers).
 */
export function appendTypeahead(buffer: string, ch: string): string {
  if (ch.length !== 1) return buffer
  return (buffer + ch).toLowerCase()
}
