// desktopRegistry manages xpra desktop sessions on the client side. It is
// the desktop equivalent of terminalRegistry.ts: ensureDesktop(id) POSTs
// /api/desktop to spawn the xpra server for the leaf, and destroyDesktop
// DELETEs it. The actual rendering is the bundled HTML5 client (see
// DesktopPanel.tsx and ../xpra), whose canvas lives in a keep-alive
// registry (../xpra/clientRegistry) so hiding a pane never reloads the
// stream.

import { destroyClient } from '../xpra/clientRegistry'

export interface DesktopSession {
  leafId: string
  backendId: string
  exited: boolean
}

const registry = new Map<string, DesktopSession>()
const subscribers = new Set<() => void>()
let onDesktopExit: ((leafId: string) => void) | null = null

export function setOnDesktopExit(cb: ((leafId: string) => void) | null) {
  onDesktopExit = cb
}

function notify() {
  for (const s of subscribers) s()
}

export function subscribeDesktop(cb: () => void): () => void {
  subscribers.add(cb)
  return () => { subscribers.delete(cb) }
}

// ensureDesktop spawns the xpra session for a leaf if one doesn't already
// exist. Mirrors terminalRegistry.ensureBackend. Returns the backend
// session id (which equals the leaf id).
export async function ensureDesktop(leafId: string, cwd: string, cmd?: string[], env?: [string, string][]): Promise<string> {
  const existing = registry.get(leafId)
  if (existing && !existing.exited) return existing.backendId

  const body: Record<string, unknown> = { id: leafId, cwd: cwd || '', cmd }
  if (env && env.length > 0) body.env = env
  const res = await fetch('/api/desktop', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  const { id } = (await res.json())?.data ?? {}
  const backendId = id || leafId
  registry.set(leafId, { leafId, backendId, exited: false })
  notify()
  return backendId
}

export function destroyDesktop(leafId: string): void {
  const inst = registry.get(leafId)
  if (!inst) return
  registry.delete(leafId)
  destroyClient(leafId)
  notify()
  fetch(`/api/desktop/${encodeURIComponent(leafId)}`, {
    method: 'DELETE',
  }).catch(() => {})
}

// healthCheck polls the desktop session's health endpoint. Returns false if
// the session is gone (the xpra server died). Used by DesktopPanel to show
// a "session ended" state instead of a blank iframe.
export async function desktopHealthCheck(leafId: string): Promise<boolean> {
  try {
    const res = await fetch(`/api/desktop/${encodeURIComponent(leafId)}`)
    if (!res.ok) return false
    const data = (await res.json())?.data
    return !!data?.exists && !!data?.healthy
  } catch {
    return false
  }
}

export function markDesktopExited(leafId: string): void {
  const inst = registry.get(leafId)
  if (inst && !inst.exited) {
    inst.exited = true
    onDesktopExit?.(leafId)
    notify()
  }
}

export function getDesktopSession(leafId: string): DesktopSession | undefined {
  return registry.get(leafId)
}

// isDesktopExited reports whether the desktop session for this leaf has
// already exited (the xpra server / app closed). Used by handleClosePane to
// close the pane immediately when the app is gone.
export function isDesktopExited(leafId: string): boolean {
  const inst = registry.get(leafId)
  return !!inst?.exited
}