// Window model + draw pipeline for the bundled xpra client. Each remote
// window owns a <canvas>. The session's primary window is rendered
// fullscreen inside the pane (no chrome); transient windows (dialogs,
// popups, menus) are drawn as floating overlays with macOS-style chrome
// that matches what DesktopPanel previously injected into the iframe.

import { decode_rgb } from './vendor/RgbHelpers.js'
import { PKT } from './packets'

export interface WindowMetadata {
  title?: string
  'size-constraints'?: Record<string, unknown>
  'transient-for'?: number
  modal?: boolean
  'window-type'?: string[]
  fullscreen?: boolean
  maximized?: boolean
  'iconic'?: boolean
}

interface XpraWindow {
  wid: number
  x: number
  y: number
  w: number
  h: number
  metadata: WindowMetadata
  canvas: HTMLCanvasElement
  ctx: CanvasRenderingContext2D
  // chrome overlay element (only for transient windows)
  wrapper: HTMLDivElement | null
  titleBar: HTMLDivElement | null
}

// The container where window DOM lives (the pane root, passed in).
export class XpraWindows {
  private container: HTMLElement
  private windows = new Map<number, XpraWindow>()
  private send: (packet: unknown[]) => void
  // screen geometry (the pane rect); used to configure the main window and
  // to position transient windows relative to it.
  screenW = 0
  screenH = 0
  focusedWid = 0

  constructor(container: HTMLElement, send: (packet: unknown[]) => void) {
    this.container = container
    this.send = send
  }

  setScreen(w: number, h: number): void {
    this.screenW = w
    this.screenH = h
    for (const [, win] of this.windows) {
      if (win.metadata['transient-for'] == null) this.reconfigure(win)
    }
  }

  focus(wid: number): void {
    if (this.focusedWid === wid) return
    this.focusedWid = wid
    this.send([PKT.focus, wid, []])
  }

  newWindow(packet: unknown[]): void {
    const wid = Number(packet[1])
    const x = Number(packet[2] || 0)
    const y = Number(packet[3] || 0)
    const w = Math.max(1, Number(packet[4] || this.screenW))
    const h = Math.max(1, Number(packet[5] || this.screenH))
    const metadata = (packet[6] as WindowMetadata) || {}
    this.createWindow(wid, x, y, w, h, metadata, false)
  }

  newOverrideRedirect(packet: unknown[]): void {
    const wid = Number(packet[1])
    const x = Number(packet[2] || 0)
    const y = Number(packet[3] || 0)
    const w = Math.max(1, Number(packet[4] || 1))
    const h = Math.max(1, Number(packet[5] || 1))
    const metadata = (packet[6] as WindowMetadata) || {}
    this.createWindow(wid, x, y, w, h, metadata, true)
  }

  private createWindow(wid: number, x: number, y: number, w: number, h: number, metadata: WindowMetadata, overrideRedirect: boolean): void {
    if (this.windows.has(wid)) return
    const canvas = document.createElement('canvas')
    canvas.width = w
    canvas.height = h
    canvas.className = 'caw-xpra-canvas'
    canvas.style.cssText = 'display:block;width:100%;height:100%;'

    const transient = metadata['transient-for'] != null || overrideRedirect || (metadata['window-type']?.some((t) => t === 'DIALOG' || t === 'MENU' || t === 'POPUP_MENU' || t === 'TOOLTIP' || t === 'DROPDOWN_MENU' || t === 'COMBO') ?? false)

    let wrapper: HTMLDivElement | null = null
    let titleBar: HTMLDivElement | null = null
    const ctx = canvas.getContext('2d', { alpha: true })!

    if (transient) {
      // macOS-style chrome overlay.
      wrapper = document.createElement('div')
      wrapper.className = 'caw-xpra-window'
      wrapper.style.cssText = [
        'position:absolute',
        `left:${x}px`,
        `top:${y}px`,
        `width:${w}px`,
        `height:${h + 30}px`,
        'border-radius:10px',
        'overflow:hidden',
        'border:1px solid rgba(255,255,255,0.14)',
        'box-shadow:0 12px 32px rgba(0,0,0,0.55)',
        'background:#1c1c1e',
        'z-index:10',
      ].join(';')
      titleBar = document.createElement('div')
      titleBar.className = 'caw-xpra-titlebar'
      titleBar.style.cssText = [
        'position:relative',
        'height:30px',
        'background:linear-gradient(#38383a,#2a2a2c)',
        'border-bottom:1px solid rgba(255,255,255,0.08)',
        'display:flex',
        'align-items:center',
        'padding:0 12px',
      ].join(';')
      const title = document.createElement('div')
      title.style.cssText = 'position:absolute;left:64px;right:64px;top:0;height:30px;line-height:30px;text-align:center;font:500 13px -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;color:rgba(255,255,255,0.85);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;pointer-events:none;'
      title.textContent = metadata.title ?? ''
      titleBar.appendChild(title)
      const buttons = document.createElement('div')
      buttons.style.cssText = 'position:absolute;right:12px;top:0;height:30px;display:flex;align-items:center;gap:8px;'
      const close = this.chromeButton('#ff5f57')
      const minimize = this.chromeButton('#febc2e')
      const maximize = this.chromeButton('#28c840')
      minimize.style.pointerEvents = 'none'
      minimize.style.cursor = 'default'
      close.addEventListener('click', (e) => { e.stopPropagation(); this.send([PKT.close_window, wid]) })
      maximize.addEventListener('click', (e) => { e.stopPropagation(); this.toggleMaximize(wid) })
      buttons.appendChild(close)
      buttons.appendChild(minimize)
      buttons.appendChild(maximize)
      titleBar.appendChild(buttons)
      const body = document.createElement('div')
      body.style.cssText = 'position:absolute;top:30px;left:0;right:0;bottom:0;'
      body.appendChild(canvas)
      canvas.style.cssText = 'display:block;width:100%;height:100%;'
      wrapper.appendChild(titleBar)
      wrapper.appendChild(body)
      // Clicking the overlay focuses the window.
      wrapper.addEventListener('pointerdown', () => this.focus(wid))
      this.container.appendChild(wrapper)
    } else {
      // Main window: fills the pane, no chrome, lowest layer.
      canvas.style.position = 'absolute'
      canvas.style.left = '0'
      canvas.style.top = '0'
      canvas.style.width = '100%'
      canvas.style.height = '100%'
      canvas.style.zIndex = '1'
      this.container.appendChild(canvas)
    }

    const win: XpraWindow = { wid, x, y, w, h, metadata, canvas, ctx, wrapper, titleBar }
    this.windows.set(wid, win)
    // Focus + configure to the pane immediately.
    this.focus(wid)
    this.reconfigure(win)
  }

  private chromeButton(color: string): HTMLSpanElement {
    const b = document.createElement('span')
    b.style.cssText = `width:12px;height:12px;border-radius:50%;cursor:pointer;border:0.5px solid rgba(0,0,0,0.25);background:${color};box-sizing:border-box;`
    return b
  }

  private toggleMaximize(wid: number): void {
    const win = this.windows.get(wid)
    if (!win || !win.wrapper) return
    const maximized = win.wrapper.dataset.max === '1'
    if (maximized) {
      win.wrapper.dataset.max = '0'
      win.wrapper.style.left = `${win.x}px`
      win.wrapper.style.top = `${win.y}px`
      win.wrapper.style.width = `${win.w}px`
      win.wrapper.style.height = `${win.h + 30}px`
    } else {
      win.wrapper.dataset.max = '1'
      win.wrapper.style.left = '0'
      win.wrapper.style.top = '0'
      win.wrapper.style.width = `${this.screenW}px`
      win.wrapper.style.height = `${this.screenH}px`
    }
  }

  private reconfigure(win: XpraWindow): void {
    if (win.wrapper) {
      // transient: keep its requested geometry but clamp inside the pane.
      const x = Math.min(Math.max(win.x, 0), Math.max(0, this.screenW - win.w))
      const y = Math.min(Math.max(win.y, 0), Math.max(0, this.screenH - win.h))
      win.x = x
      win.y = y
      win.wrapper.style.left = `${x}px`
      win.wrapper.style.top = `${y}px`
    } else {
      // main window: configure to fill the pane.
      win.x = 0
      win.y = 0
      win.w = this.screenW
      win.h = this.screenH
      if (win.canvas.width !== win.w) win.canvas.width = win.w
      if (win.canvas.height !== win.h) win.canvas.height = win.h
    }
    // Tell the server the new geometry so the app repaints at the right size.
    this.send([PKT.configure_window, win.wid, win.x, win.y, win.w, win.h, {}])
  }

  windowMetadata(packet: unknown[]): void {
    const wid = Number(packet[1])
    const win = this.windows.get(wid)
    if (!win) return
    const meta = (packet[2] as WindowMetadata) || {}
    win.metadata = { ...win.metadata, ...meta }
    if (win.titleBar && typeof meta.title === 'string') {
      const title = win.titleBar.querySelector('div') as HTMLDivElement | null
      if (title) title.textContent = meta.title
    }
  }

  windowResized(packet: unknown[]): void {
    // [type, wid, x, y, w, h]
    const wid = Number(packet[1])
    const win = this.windows.get(wid)
    if (!win) return
    win.w = Math.max(1, Number(packet[4] || win.w))
    win.h = Math.max(1, Number(packet[5] || win.h))
    if (win.wrapper) {
      win.wrapper.style.width = `${win.w}px`
      win.wrapper.style.height = `${win.h + 30}px`
    } else if (win.canvas.width !== win.w) {
      win.canvas.width = win.w
      win.canvas.height = win.h
    }
  }

  moveResize(packet: unknown[]): void {
    // [type, wid, x, y, w, h]
    this.windowResized(packet)
  }

  lostWindow(packet: unknown[]): void {
    const wid = Number(packet[1])
    this.destroyWindow(wid)
  }

  unmapWindow(packet: unknown[]): void {
    const wid = Number(packet[1])
    const win = this.windows.get(wid)
    if (!win) return
    if (win.wrapper) win.wrapper.style.display = 'none'
    else win.canvas.style.display = 'none'
  }

  mapWindow(packet: unknown[]): void {
    const wid = Number(packet[1])
    const win = this.windows.get(wid)
    if (!win) return
    if (win.wrapper) win.wrapper.style.display = ''
    else win.canvas.style.display = ''
    // re-focus on re-map
    this.focus(wid)
  }

  destroyWindow(wid: number): void {
    const win = this.windows.get(wid)
    if (!win) return
    this.windows.delete(wid)
    if (win.wrapper) win.wrapper.remove()
    else win.canvas.remove()
    if (this.focusedWid === wid) this.focusedWid = 0
  }

  destroyAll(): void {
    for (const wid of [...this.windows.keys()]) this.destroyWindow(wid)
  }

  windowIcon(packet: unknown[]): void {
    // [type, wid, width, height, encoding, img_data]
    const wid = Number(packet[1])
    const win = this.windows.get(wid)
    if (!win || !win.titleBar) return
    const encoding = String(packet[4] || '')
    const imgData = packet[5] as Uint8Array | string
    if (encoding !== 'png' || !imgData) return
    // We don't render window icons in the chrome (parity with the previous
    // injected style which hid .windowicon). Left as a no-op.
  }

  cursor(_packet: unknown[]): void {
    // [type, wid?, ...] — newer xpra sends [cursor, x, y, w, h, xhot, yhot, serial, encoding, img_data]
    // We support named cursors via a CSS cursor swap on the container only if
    // the packet carries a simple cursor name; custom pixel cursors are left
    // to the default arrow.
  }

  // draw handles a "draw" packet: [type, wid, x, y, width, height, coding, data, seq, rowstride, options]
  draw(packet: unknown[]): void {
    const wid = Number(packet[1])
    const win = this.windows.get(wid)
    if (!win) return
    const x = Number(packet[2])
    const y = Number(packet[3])
    const width = Number(packet[4])
    const height = Number(packet[5])
    let coding = String(packet[6])
    const imgData = packet[7] as Uint8Array
    const options = (packet[10] as Record<string, unknown>) || {}

    try {
      if (coding === 'void') return
      if (coding === 'rgb32' || coding === 'rgb24') {
        const rgb = decode_rgb(packet as unknown[])
        const img = win.ctx.createImageData(width, height)
        img.data.set(rgb)
        win.ctx.putImageData(img, x, y, 0, 0, width, height)
        return
      }
      if (coding === 'jpeg' || coding.startsWith('png') || coding === 'webp') {
        const base = coding.split('/')[0]
        const mime = base === 'pngP' || base === 'pngL' ? 'image/png' : `image/${base}`
        const blob = new Blob([blobPart(imgData)], { type: mime })
        createImageBitmap(blob).then((bitmap) => {
          win.ctx.clearRect(x, y, width, height)
          win.ctx.drawImage(bitmap, x, y, width, height)
          bitmap.close()
        }).catch((e) => console.error('xpra draw decode failed:', coding, e))
        return
      }
      if (coding === 'scroll') {
        const scrolls = (options['scroll'] || imgData) as number[][]
        for (const s of scrolls) {
          const [sx, sy, sw, sh, dx, dy] = s
          win.ctx.drawImage(win.canvas, sx, sy, sw, sh, sx + dx, sy + dy, sw, sh)
        }
        return
      }
      // Unsupported encoding (e.g. h264): request a buffer refresh so the
      // server resends with a codec we can handle.
      console.warn('xpra: unsupported draw encoding', coding)
    } catch (e) {
      console.error('xpra draw error:', e)
    }
  }

  // The container element for window DOM. Used by the React layer to attach.
  getContainer(): HTMLElement {
    return this.container
  }

  // Returns the window element to fullscreen (the pane itself handles FS).
  hasWindows(): boolean {
    return this.windows.size > 0
  }
}

// blobPart normalises a rencode-decoded data buffer (which tsc types as
// Uint8Array<ArrayBufferLike>) into a BlobPart by copying into a plain
// ArrayBuffer-backed Uint8Array.
function blobPart(data: Uint8Array | string): BlobPart {
  if (typeof data === 'string') return data
  const copy = new Uint8Array(data.byteLength)
  copy.set(data)
  return copy
}