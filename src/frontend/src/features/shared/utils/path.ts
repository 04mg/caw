export function normalizePath(p: string): string {
  if (!p) return ''
  let s = p.replace(/\\/g, '/')
  if (/^[A-Z]:/i.test(s)) {
    s = s.charAt(0).toLowerCase() + s.slice(1)
  }
  if (s.length > 1 && s.endsWith('/')) {
    s = s.slice(0, -1)
  }
  return s
}

export function pathsEqual(a: string, b: string): boolean {
  return normalizePath(a) === normalizePath(b)
}