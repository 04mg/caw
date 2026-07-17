import { wsMux } from '@/features/shared/services/wsMultiplexer'

export interface GitStatusEvent {
  type: 'git-status'
  path: string
  statuses: Record<string, string>
  ignored: Record<string, boolean>
}

type GitStatusListener = (event: GitStatusEvent) => void

const listeners = new Set<GitStatusListener>()
const subscribedPaths = new Map<string, number>()
let unsubMux: (() => void) | null = null

function ensureMux() {
  if (unsubMux) return
  unsubMux = wsMux.subscribe('git', (data) => {
    try {
      const event = data as GitStatusEvent
      if (event && event.type === 'git-status' && event.path) {
        for (const l of listeners) l(event)
      }
    } catch { /* ignore */ }
  })
}

function sendSub(path: string) {
  wsMux.send('git', { type: 'subscribe', path })
}

function sendUnsub(path: string) {
  wsMux.send('git', { type: 'unsubscribe', path })
}

function addPath(path: string) {
  const count = subscribedPaths.get(path) ?? 0
  if (count === 0) {
    sendSub(path)
  }
  subscribedPaths.set(path, count + 1)
}

function removePath(path: string) {
  const count = subscribedPaths.get(path) ?? 0
  if (count <= 1) {
    subscribedPaths.delete(path)
    sendUnsub(path)
  } else {
    subscribedPaths.set(path, count - 1)
  }
}

export function subscribeToGitStatus(path: string, cb: GitStatusListener): () => void {
  listeners.add(cb)
  addPath(path)
  ensureMux()

  return () => {
    listeners.delete(cb)
    removePath(path)
    if (listeners.size === 0 && unsubMux) {
      unsubMux()
      unsubMux = null
    }
  }
}