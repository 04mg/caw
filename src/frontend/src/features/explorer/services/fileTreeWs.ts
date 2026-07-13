import { wsMux } from '@/features/shared/services/wsMultiplexer'

export interface FileTreeEvent {
  type: 'file-created' | 'file-modified' | 'file-deleted' | 'dir-changed'
  path: string
  isDir?: boolean
}

type FileTreeListener = (event: FileTreeEvent) => void

const listeners = new Set<FileTreeListener>()
const subscribedPaths = new Map<string, number>()
let unsubMux: (() => void) | null = null

function ensureMux() {
  if (unsubMux) return
  unsubMux = wsMux.subscribe('files', (data) => {
    try {
      const event = data as FileTreeEvent
      if (event && event.type) {
        for (const l of listeners) l(event)
      }
    } catch { /* ignore */ }
  })
}

function sendSub(path: string) {
  wsMux.send('files', { type: 'subscribe', path })
}

function sendUnsub(path: string) {
  wsMux.send('files', { type: 'unsubscribe', path })
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

export function subscribeToFileTree(path: string, cb: FileTreeListener): () => void {
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

export function hasFileTreeListeners(): boolean {
  return listeners.size > 0
}
