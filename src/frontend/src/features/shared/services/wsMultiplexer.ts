type ChannelHandler = (data: unknown) => void

interface Envelope {
  channel: string
  data?: unknown
}

class WsMultiplexer {
  private ws: WebSocket | null = null
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private readonly channels = new Map<string, Set<ChannelHandler>>()
  private connecting = false

  private connect() {
    if (this.ws && (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)) return
    if (this.connecting) return
    this.connecting = true
    const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
    this.ws = new WebSocket(`${protocol}//${location.host}/ws`)

    this.ws.onopen = () => {
      this.connecting = false
      for (const channel of this.channels.keys()) {
        this.sendSubscribe(channel)
      }
    }

    this.ws.onmessage = (e) => {
      try {
        const env = JSON.parse(e.data) as Envelope
        if (!env || !env.channel) return
        const handlers = this.channels.get(env.channel)
        if (handlers) {
          for (const h of handlers) h(env.data)
        }
      } catch {
        /* ignore malformed */
      }
    }

    this.ws.onclose = () => {
      this.ws = null
      this.connecting = false
      if (this.reconnectTimer) clearTimeout(this.reconnectTimer)
      if (this.channels.size > 0) {
        this.reconnectTimer = setTimeout(() => this.connect(), 1000)
      }
    }

    this.ws.onerror = () => {
      // onclose will fire next; let it handle reconnect.
    }
  }

  private sendSubscribe(channel: string) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify({ channel, type: 'subscribe' }))
    }
  }

  private sendUnsubscribe(channel: string) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify({ channel, type: 'unsubscribe' }))
    }
  }

  subscribe(channel: string, handler: ChannelHandler): () => void {
    let set = this.channels.get(channel)
    if (!set) {
      set = new Set()
      this.channels.set(channel, set)
      this.sendSubscribe(channel)
    }
    set.add(handler)
    this.connect()
    return () => {
      set!.delete(handler)
      if (set!.size === 0) {
        this.channels.delete(channel)
        this.sendUnsubscribe(channel)
        if (this.channels.size === 0 && this.reconnectTimer) {
          clearTimeout(this.reconnectTimer)
          this.reconnectTimer = null
        }
      }
    }
  }

  send(channel: string, payload: unknown) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify({ channel, data: payload }))
    }
  }
}

export const wsMux = new WsMultiplexer()