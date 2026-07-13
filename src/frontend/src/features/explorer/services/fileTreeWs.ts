import { wsMux } from '@/features/shared/services/wsMultiplexer'

export interface FileTreeEvent {
  type: 'file-created' | 'file-modified' | 'file-deleted' | 'dir-changed'
  path: string
  isDir?: boolean
}

type FileTreeListener = (event: FileTreeEvent) => void

const listeners = new Set<FileTreeListener>()
let subscribedPath: string | null = null
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

export function subscribeToFileTree(path: string, cb: FileTreeListener): () => void {
  listeners.add(cb)

  if (subscribedPath !== path) {
    if (subscribedPath) sendUnsub(subscribedPath)
    subscribedPath = path
    sendSub(path)
  }

  ensureMux()

  return () => {
    listeners.delete(cb)
    if (listeners.size === 0) {
      if (subscribedPath) sendUnsub(subscribedPath)
      subscribedPath = null
      if (unsubMux) {
        unsubMux()
        unsubMux = null
      }
    }
  }
}

export function hasFileTreeListeners(): boolean {
  return listeners.size > 0
}
