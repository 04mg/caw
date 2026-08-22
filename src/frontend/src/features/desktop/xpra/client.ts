// The bundled Xpra HTML5 client. Owns the WebSocket connection (via the
// vendored Protocol), performs the hello handshake, dispatches packets to
// the window/draw, input, clipboard and audio subsystems, and handles ping,
// reconnect and connection lifecycle. This replaces the iframe that used to
// load xpra's built-in HTML5 client — the canvas now renders directly inside
// the React pane.

import { XpraProtocol } from './vendor/Protocol.js'
import { XpraWindows } from './windows'
import { XpraInput } from './input'
import type { InputSink } from './input'
import { XpraClipboard } from './clipboard'
import { XpraAudio, audioDecoders } from './audio'
import { PKT } from './packets'
import type { DesktopStreamPrefs } from '@/features/prefs/stores/prefsStore'

export type ConnectionState = 'idle' | 'connecting' | 'connected' | 'reconnecting' | 'closed' | 'error'

export interface XpraClientCallbacks {
  onStateChange: (state: ConnectionState, details?: string) => void
  // Fired once the first "new-window" (or new-override-redirect) arrives so
  // the UI can hide the loading spinner.
  onFirstWindow: () => void
  // Fired when the server sends "disconnect" or the WS closes for good.
  onClose: (reason?: string) => void
}

const DEFAULT_STREAM: DesktopStreamPrefs = { encoding: 'auto', quality: 70, speed: 50 }

export class XpraClient {
  private protocol = new XpraProtocol()
  private windows: XpraWindows
  private input: XpraInput
  private clipboard: XpraClipboard
  private audio: XpraAudio
  private wsUrl: string
  private stream: DesktopStreamPrefs
  cb: XpraClientCallbacks
  private sink: InputSink
  private connected = false
  private helloReceived = false
  private reconnectAttempts = 0
  private reconnectTimer: number | null = null
  private firstWindowSeen = false
  private width = 1
  private height = 1
  // Whether audio has been requested by the user (mute toggle).
  private audioMuted = false

  constructor(container: HTMLElement, wsUrl: string, stream: DesktopStreamPrefs, cb: XpraClientCallbacks) {
    this.wsUrl = wsUrl
    this.stream = { ...DEFAULT_STREAM, ...stream }
    this.cb = cb
    this.windows = new XpraWindows(container, (p) => this.send(p))
    this.clipboard = new XpraClipboard()
    this.clipboard.send = (p) => this.send(p)
    this.sink = this.makeInputSink()
    this.input = new XpraInput(this.sink, { interceptedCombos: [] })
    this.audio = new XpraAudio()
    this.audio.onStateChange = (s, d) => { void s; void d }
    this.protocol.set_packet_handler((packet) => this.onPacket(packet))
  }

  // makeInputSink builds a stable object whose captureKeyboard/altgr flags
  // the client toggles as the connection comes up / drops and AltGr is
  // pressed (a `get` accessor would bind `this` to the sink object).
  private makeInputSink(): InputSink {
    return {
      send: (p) => this.send(p),
      focusedWid: () => this.windows.focusedWid,
      captureKeyboard: false,
      altgr: false,
    }
  }

  setInterceptedCombos(combos: string[]): void {
    this.input.setConfig({ interceptedCombos: combos })
  }

  setCallbacks(cb: XpraClientCallbacks): void {
    this.cb = cb
  }

  setScreenSize(w: number, h: number): void {
    if (this.width === w && this.height === h) return
    this.width = w
    this.height = h
    this.windows.setScreen(w, h)
    if (this.connected && this.helloReceived) this.sendConfigureDisplay()
  }

  setAudioMuted(muted: boolean): void {
    this.audioMuted = muted
    if (muted) {
      this.audio.close()
      this.send([PKT.sound_control, 'stop'])
    } else if (this.connected && this.helloReceived) {
      this.startAudio()
    }
  }

  isAudioMuted(): boolean {
    return this.audioMuted
  }

  // ----- lifecycle -----

  connect(): void {
    this.setState('connecting')
    this.protocol.open(this.wsUrl)
  }

  close(): void {
    if (this.reconnectTimer) { clearTimeout(this.reconnectTimer); this.reconnectTimer = null }
    this.connected = false
    this.sink.captureKeyboard = false
    this.audio.close()
    try { this.send([PKT.disconnect]) } catch { /* ignore */ }
    this.protocol.close()
    this.windows.destroyAll()
    this.setState('closed')
  }

  // Attach DOM listeners for keyboard/pointer onto the given element.
  attachInput(target: HTMLElement): () => void {
    const kd = (e: KeyboardEvent) => {
      const allow = this.input.onKeyDown(e)
      if (!allow) e.preventDefault()
    }
    const ku = (e: KeyboardEvent) => {
      const allow = this.input.onKeyUp(e)
      if (!allow) e.preventDefault()
    }
    target.addEventListener('keydown', kd, true)
    target.addEventListener('keyup', ku, true)
    // pointer events are handled by the windows module (per-window chrome).
    return () => {
      target.removeEventListener('keydown', kd, true)
      target.removeEventListener('keyup', ku, true)
    }
  }

  // ----- protocol -----

  private send(packet: unknown[]): void {
    if (!this.connected && (packet[0] as string) !== PKT.disconnect) return
    this.protocol.send(packet)
  }

  private setState(state: ConnectionState, details?: string): void {
    this.cb.onStateChange(state, details)
  }

  private onPacket(packet: unknown[]): void {
    const type = String(packet[0])
    switch (type) {
      case 'open':
        this.handleOpen()
        break
      case 'hello':
        this.handleHello(packet)
        break
      case 'close':
        this.handleClose(String(packet[1] ?? ''))
        break
      case 'error':
        this.handleError(String(packet[1] ?? 'error'), Number(packet[2] ?? 0))
        break
      case PKT.ping:
        this.send([PKT.ping_echo, packet[1], 0, 0, 0, 0])
        break
      case PKT.new_window:
      case PKT.new_override_redirect:
        if (!this.firstWindowSeen) {
          this.firstWindowSeen = true
          this.cb.onFirstWindow()
        }
        if (type === PKT.new_window) this.windows.newWindow(packet)
        else this.windows.newOverrideRedirect(packet)
        break
      case PKT.window_metadata:
        this.windows.windowMetadata(packet)
        break
      case PKT.window_resized:
        this.windows.windowResized(packet)
        break
      case PKT.window_move_resize:
        this.windows.moveResize(packet)
        break
      case PKT.map_window:
        this.windows.mapWindow(packet)
        break
      case PKT.unmap_window:
        this.windows.unmapWindow(packet)
        break
      case PKT.lost_window:
        this.windows.lostWindow(packet)
        break
      case PKT.draw:
        this.windows.draw(packet)
        break
      case PKT.window_icon:
        this.windows.windowIcon(packet)
        break
      case PKT.cursor:
        this.windows.cursor(packet)
        break
      case PKT.set_clipboard_enabled:
        this.clipboard.setEnabled(Boolean(packet[1]), String(packet[2] ?? ''))
        break
      case PKT.clipboard_token:
        this.clipboard.handleToken(packet)
        break
      case PKT.clipboard_request:
        this.clipboard.handleRequest(packet)
        break
      case PKT.audio_signal:
        // server indicates audio is available — start it if not muted.
        if (!this.audioMuted) this.startAudio()
        break
      case PKT.sound_data:
        this.audio.feed(String(packet[1]), packet[2] as Uint8Array | null, (packet[3] as Record<string, unknown>) || {}, packet[4])
        break
      case PKT.bell:
      case PKT.encodings:
      case PKT.damage_sequence:
      case PKT.setting_change:
      case PKT.connection_data:
      case 'challenge':
      case PKT.keyboard_config:
      case PKT.keymap_changed:
      case PKT.startup_complete:
        // no-op for Caw's desktop-app use case
        break
      default:
        // unknown packet — ignore
        break
    }
  }

  private handleOpen(): void {
    this.connected = true
    this.sink.captureKeyboard = true
    this.reconnectAttempts = 0
    this.sendHello()
  }

  private sendHello(): void {
    const encodings = ['rgb32', 'rgb24', 'png', 'pngL', 'pngP', 'jpeg', 'webp', 'void', 'scroll']
    const caps: Record<string, unknown> = {
      'version': '15.0',
      'client_type': 'HTML5',
      'session-type': 'caw',
      'argv': [typeof location !== 'undefined' ? location.href : 'caw'],
      'share': false,
      'steal': false,
      'mouse.show': true,
      'vrefresh': 60,
      'file-chunks': 0,
      'setting-change': true,
      // The vendored rencode.js implements rencode+; the server must decode
      // our packets with it (proto_flags=0x10) or it rejects the hello.
      'rencodeplus': true,
      'brotli': false,
      'lz4': true,
      'compression_level': 1,
      'network': { 'pings': 5 },
      'auto_refresh_delay': 500,
      'metadata.supported': ['title', 'size-constraints', 'transient-for', 'modal', 'window-type', 'fullscreen', 'maximized', 'iconic', 'icon-name'],
      'encodings': {
        '': encodings,
        'core': encodings,
        'rgb_formats': ['RGBA', 'RGB'],
        'window-icon': ['png'],
        'cursor': ['png'],
        'packet': true,
      },
      'encoding': this.encodingCaps(),
      'audio': { 'receive': true, 'send': false, 'decoders': audioDecoders(), 'signal': true },
      'clipboard': this.clipboard.caps(),
      'pointer': { 'double_click': {} },
      'file': { 'enabled': false, 'printing': false, 'open-url': false, 'size-limit': 0 },
      'wants': ['audio', 'packet-types'],
      'windows': true,
      'window.pre-m': true,
      'keyboard': true,
      'screen_sizes': this.screenSizes(),
      'dpi': { 'x': 96, 'y': 96 },
      'notifications': { 'enabled': false },
      'cursors': true,
      'bell': false,
      'system_tray': false,
      'named_cursors': false,
    }
    this.send([PKT.hello, caps])
  }

  private encodingCaps(): Record<string, unknown> {
    const opts: Record<string, unknown> = {
      'quality': this.stream.quality,
      'speed': this.stream.speed,
    }
    if (this.stream.encoding && this.stream.encoding !== 'auto') {
      opts['encoding'] = this.stream.encoding
    } else {
      opts['encoding'] = 'auto'
    }
    return opts
  }

  private screenSizes(): unknown[] {
    const dpi = 96
    const wmm = Math.round((this.width * 25.4) / dpi)
    const hmm = Math.round((this.height * 25.4) / dpi)
    const monitor = ['Canvas', 0, 0, this.width, this.height, wmm, hmm]
    const screen = ['HTML', this.width, this.height, wmm, hmm, [monitor], 0, 0, this.width, this.height]
    return [screen]
  }

  private sendConfigureDisplay(): void {
    this.send([PKT.configure_display, {
      'desktop-size': [this.width, this.height],
      'monitors': this.monitors(),
      'dpi': { 'x': 96, 'y': 96 },
      'vrefresh': 60,
    }])
  }

  private monitors(): Map<number, unknown> {
    const dpi = 96
    const wmm = Math.round((this.width * 25.4) / dpi)
    const hmm = Math.round((this.height * 25.4) / dpi)
    const m = new Map<number, unknown>()
    m.set(0, {
      'geometry': [0, 0, this.width, this.height],
      'primary': true,
      'refresh-rate': 60,
      'width-mm': wmm,
      'height-mm': hmm,
      'workarea': [0, 0, this.width, this.height],
      'name': 'Canvas',
    })
    return m
  }

  private handleHello(packet: unknown[]): void {
    this.helloReceived = true
    const caps = (packet[1] as Record<string, unknown>) || {}
    // If the server names packet types (newer protocol), we could use them,
    // but our fixed PKT table matches.
    void caps
    this.windows.setScreen(this.width, this.height)
    this.setState('connected')
  }

  private startAudio(): void {
    if (this.audio.activeCodec) return
    const codec = this.audio.negotiate()
    if (!codec) return
    this.send([PKT.sound_control, 'start', codec])
  }

  private handleError(details: string, code: number): void {
    void code
    this.connected = false
    this.sink.captureKeyboard = false
    this.setState('error', details)
    this.scheduleReconnect()
  }

  private handleClose(reason: string): void {
    if (!this.connected) {
      // The protocol fires a synthetic "close" on protocol_error; treat as
      // fatal only if we never connected.
      this.connected = false
      this.sink.captureKeyboard = false
      this.setState('error', reason)
      this.scheduleReconnect()
      return
    }
    this.connected = false
    this.sink.captureKeyboard = false
    this.helloReceived = false
    this.firstWindowSeen = false
    this.audio.close()
    this.windows.destroyAll()
    this.setState('closed', reason)
    this.cb.onClose(reason)
  }

  private scheduleReconnect(): void {
    if (this.reconnectTimer) return
    this.reconnectAttempts += 1
    if (this.reconnectAttempts > 10) {
      this.setState('closed', 'reconnect attempts exhausted')
      this.cb.onClose('reconnect failed')
      return
    }
    this.setState('reconnecting')
    const delay = Math.min(2000 * this.reconnectAttempts, 10000)
    this.reconnectTimer = window.setTimeout(() => {
      this.reconnectTimer = null
      this.helloReceived = false
      this.firstWindowSeen = false
      this.protocol.open(this.wsUrl)
    }, delay)
  }

  // Expose for fullscreen capture (Keyboard Lock API needs the element).
  getContainer(): HTMLElement {
    return this.windows.getContainer()
  }
}