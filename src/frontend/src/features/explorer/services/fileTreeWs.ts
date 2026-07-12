export interface FileTreeEvent {
  type: 'file-created' | 'file-modified' | 'file-deleted' | 'dir-changed'
  path: string
  isDir?: boolean
}

type FileTreeListener = (event: FileTreeEvent) => void

let ws: WebSocket | null = null
const listeners = new Set<FileTreeListener>()
let subscribedPath: string | null = null
let reconnectTimer: ReturnType<typeof setTimeout> | null = null

function connect() {
  if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) return
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
  ws = new WebSocket(`${protocol}//${location.host}/ws/workspaces/files`)

  ws.onopen = () => {
    if (subscribedPath) {
      ws!.send(JSON.stringify({ type: 'subscribe', path: subscribedPath }))
    }
  }

  ws.onmessage = (e) => {
    try {
      const event = JSON.parse(e.data) as FileTreeEvent
      if (event && event.type) {
        for (const l of listeners) l(event)
      }
    } catch { /* ignore */ }
  }

  ws.onclose = () => {
    ws = null
    if (reconnectTimer) clearTimeout(reconnectTimer)
    reconnectTimer = setTimeout(() => connect(), 2000)
  }
}

function disconnect() {
  if (reconnectTimer) {
    clearTimeout(reconnectTimer)
    reconnectTimer = null
  }
  if (ws) {
    ws.onclose = null
    ws.close()
    ws = null
  }
}

export function subscribeToFileTree(path: string, cb: FileTreeListener): () => void {
  listeners.add(cb)

  if (subscribedPath !== path) {
    if (ws && ws.readyState === WebSocket.OPEN && subscribedPath) {
      ws.send(JSON.stringify({ type: 'unsubscribe', path: subscribedPath }))
    }
    subscribedPath = path
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type: 'subscribe', path }))
    }
  }

  connect()

  return () => {
    listeners.delete(cb)
    if (listeners.size === 0) {
      if (ws && ws.readyState === WebSocket.OPEN && subscribedPath) {
        ws.send(JSON.stringify({ type: 'unsubscribe', path: subscribedPath }))
      }
      subscribedPath = null
      disconnect()
    }
  }
}

export function hasFileTreeListeners(): boolean {
  return listeners.size > 0
}
