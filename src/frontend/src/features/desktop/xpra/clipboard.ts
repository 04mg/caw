// Clipboard synchronisation for the bundled xpra client. Implements the
// xpra clipboard protocol (tokens + contents requests) over both
// directions for text, using the async Clipboard API. Image clipboard is
// best-effort via ClipboardItem when the browser exposes navigator.clipboard.write.

const UTF8_STRING = 'UTF8_STRING'
const TEXT_PLAIN = 'text/plain'

const TEXT_TARGETS = [TEXT_PLAIN, UTF8_STRING, 'TEXT', 'STRING']
const CLIPBOARD_IMAGES = typeof navigator !== 'undefined' && !!navigator.clipboard && 'write' in navigator.clipboard

export interface ClipboardCaps {
  enabled: boolean
  want_targets: boolean
  greedy: boolean
  selections: string[]
  'preferred-targets': string[]
}

export class XpraClipboard {
  // Direction control. The server may disable the clipboard; we honor that.
  enabled = true
  direction: 'both' | 'to-server' | 'to-client' | 'disabled' = 'both'

  // Last selection contents the server pushed (used to answer requests and
  // to echo back when the browser clipboard can't be read).
  private serverBuffers: Record<string, [target: string, dtype: string, dformat: number, enc: string, data: Uint8Array | string]> = {}
  private preferredFormat = TEXT_PLAIN

  // send is the outbound packet sink (the client wires this to protocol.send).
  send: ((packet: unknown[]) => void) | null = null

  caps(): ClipboardCaps {
    const hasAsync = !!navigator.clipboard && !!navigator.clipboard.readText && !!navigator.clipboard.writeText
    const selections = hasAsync ? ['CLIPBOARD'] : ['CLIPBOARD', 'PRIMARY']
    const targets = [this.preferredFormat]
    for (const t of TEXT_TARGETS) if (t !== this.preferredFormat) targets.push(t)
    if (CLIPBOARD_IMAGES) targets.push('image/png')
    return {
      enabled: this.enabled,
      want_targets: true,
      greedy: true,
      selections,
      'preferred-targets': targets,
    }
  }

  setEnabled(enabled: boolean, _reason?: string): void {
    this.enabled = enabled
  }

  // handleToken processes an inbound clipboard-token packet:
  //   [type, selection, targets, target, dtype, dformat, wire_encoding, data, claim, greedy, synchronous]
  handleToken(packet: unknown[]): void {
    if (!this.enabled || this.direction === 'to-server') return
    const selection = String(packet[1])
    let target = ''
    let dtype = ''
    let dformat = 0
    let wireEncoding = ''
    let wireData: Uint8Array | string = ''
    if (packet.length >= 8) {
      target = String(packet[3] ?? '')
      dtype = String(packet[4] ?? '')
      dformat = Number(packet[5] ?? 0) || 0
      wireEncoding = String(packet[6] ?? '')
      wireData = (packet[7] as Uint8Array | string) ?? ''
      this.serverBuffers[selection] = [target, dtype, dformat, wireEncoding, wireData]
    }
    const isValidTarget = TEXT_TARGETS.includes(target) || (CLIPBOARD_IMAGES && target === 'image/png')
    if (!isValidTarget) return
    const isText = dtype.toLowerCase().includes('text') || dtype.toLowerCase().includes('string')
    if (isText && navigator.clipboard?.writeText) {
      const text = typeof wireData === 'string' ? wireData : uint8ToString(wireData)
      navigator.clipboard.writeText(text).catch(() => {})
    } else if (CLIPBOARD_IMAGES && dtype === 'image/png' && dformat === 8 && wireEncoding === 'bytes' && wireData instanceof Uint8Array) {
      try {
        const copy = new Uint8Array(wireData.byteLength)
        copy.set(wireData)
        const blob = new Blob([copy], { type: 'image/png' })
        const item = new ClipboardItem({ 'image/png': blob })
        navigator.clipboard.write([item]).catch(() => {})
      } catch { /* ignore */ }
    }
  }

  // handleRequest processes an inbound clipboard-request packet:
  //   [type, request_id, selection, target?]
  handleRequest(packet: unknown[]): void {
    if (!this.enabled) return
    const requestId = String(packet[1])
    const selection = String(packet[2])
    if (selection !== 'CLIPBOARD') {
      this.sendNone(requestId, selection)
      return
    }
    const target = packet.length >= 4 ? String(packet[3]) : ''
    if (navigator.clipboard?.read) {
      navigator.clipboard.read().then(
        (items) => {
          for (const item of items) {
            if (item.types.includes(TEXT_PLAIN)) {
              item.getType(TEXT_PLAIN).then((blob) => blob.text()).then(
                (text) => this.sendString(requestId, selection, text, UTF8_STRING),
                () => this.resendServerBuffer(requestId, selection),
              )
              return
            }
            if (target === 'image/png' && item.types.includes('image/png')) {
              item.getType('image/png').then((blob) => blob.arrayBuffer()).then(
                (ab) => this.sendContents(requestId, selection, 'image/png', 8, 'bytes', new Uint8Array(ab)),
                () => this.resendServerBuffer(requestId, selection),
              )
              return
            }
          }
          this.resendServerBuffer(requestId, selection)
        },
        () => this.resendServerBuffer(requestId, selection),
      )
    } else if (navigator.clipboard?.readText) {
      navigator.clipboard.readText().then(
        (text) => this.sendString(requestId, selection, text, UTF8_STRING),
        () => this.resendServerBuffer(requestId, selection),
      )
    } else {
      this.resendServerBuffer(requestId, selection)
    }
  }

  // sendToken is called when the browser clipboard changes locally (e.g. the
  // user copied text inside the desktop app — we never see that, so this is
  // only useful when a Caw-side copy should be pushed to the remote). Caw
  // keeps it simple: we don't actively poll, but expose this for completeness.
  sendToken(): void {
    const send = this.send
    if (!this.enabled || this.direction === 'to-client' || !send) return
    if (!navigator.clipboard?.readText) return
    navigator.clipboard.readText().then((text) => {
      const data = new TextEncoder().encode(text)
      send(['clipboard-token', 'CLIPBOARD', [TEXT_PLAIN, UTF8_STRING], UTF8_STRING, UTF8_STRING, 8, 'bytes', data, true, true, true])
    }).catch(() => {})
  }

  private resendServerBuffer(requestId: string, selection: string): void {
    const buf = this.serverBuffers['CLIPBOARD']
    if (!buf) {
      this.sendNone(requestId, selection)
      return
    }
    const [target, dtype, dformat, enc, data] = buf
    void target
    this.sendContents(requestId, selection, dtype, dformat, enc, data)
  }

  private sendString(requestId: string, selection: string, text: string, datatype: string): void {
    if (!text) {
      this.sendNone(requestId, selection)
      return
    }
    if (!this.send) return
    const data = new TextEncoder().encode(text)
    this.send(['clipboard-contents', requestId, selection, datatype, 8, 'bytes', data])
  }

  private sendContents(requestId: string, selection: string, dtype: string, dformat: number, encoding: string, data: Uint8Array | string): void {
    if (!this.send) return
    this.send(['clipboard-contents', requestId, selection, dtype, dformat || 8, encoding || 'bytes', data])
  }

  private sendNone(requestId: string, selection: string): void {
    if (!this.send) return
    this.send(['clipboard-contents-none', requestId, selection])
  }
}

function uint8ToString(u8: Uint8Array): string {
  const CHUNK = 0x8000
  const parts: string[] = []
  for (let i = 0; i < u8.length; i += CHUNK) {
    parts.push(String.fromCharCode.apply(null, u8.subarray(i, i + CHUNK) as unknown as number[]))
  }
  return parts.join('')
}

