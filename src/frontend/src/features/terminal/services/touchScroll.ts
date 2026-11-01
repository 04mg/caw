import type { IDisposable, Terminal } from '@xterm/xterm'

// Touch scrolling for xterm.js terminals on phones/tablets.
//
// xterm.js has no usable touch scrolling of its own, and the previous
// approach (re-dispatching synthetic `wheel` events) was broken in two ways:
//  - xterm.js v6's VS Code ScrollableElement derives the delta from the
//    legacy `wheelDeltaY` property, which synthetic WheelEvents report as
//    ±1 instead of ±120, so each synthetic "line" scrolled ~1px (and in
//    Chromium in the wrong direction): the scrollback barely moved.
//  - Deltas were quantised to whole lines at 1 line per 50px of drag with a
//    frame-rate dependent 0.85 friction, so content lagged far behind the
//    finger, advanced line-by-line and stopped almost dead on release.
//
// This module drives the viewport directly instead:
//  - Normal buffer (shell output, agents printing to scrollback): the
//    content follows the finger 1:1. xterm.js can only render whole rows,
//    so the scroll position is kept as a float; the integer part goes to
//    term.scrollToLine() and the fractional part is applied as a CSS
//    translate on .xterm-screen, giving pixel-smooth motion. On release a
//    fling decelerates exponentially (iOS-like) and is aimed so it comes to
//    rest exactly on a row boundary, so the terminal is always row-aligned
//    (no transform) when idle.
//  - Alternate buffer / mouse-wheel protocol (TUIs such as Claude Code,
//    less, vim): scrolling belongs to the app, so finger travel is sent as
//    wheel "notches" through xterm's own wheel handling, with the same
//    momentum physics. Apps with mouse reporting conventionally scroll ~3
//    lines per notch, so one notch is sent per 3 rows of travel; without
//    mouse reporting xterm sends one arrow key (= 1 line) per notch, so one
//    notch per row. Either way the content roughly follows the finger.

export interface TouchScrollSettings {
  /** Content movement per finger movement. 1 = content sticks to the finger. */
  speed: number
  /** Fraction of fling velocity retained per 60Hz frame (frame-rate independent). */
  friction: number
  /** Minimum release velocity (px/ms) that starts a fling. */
  minFlingVelocity: number
}

export const TOUCH_SCROLL_DEFAULTS: TouchScrollSettings = {
  speed: 1,
  friction: 0.96,
  minFlingVelocity: 0.05,
}

export const TOUCH_SCROLL_KEYS = {
  speed: 'caw:terminalTouchScrollSpeed',
  friction: 'caw:terminalScrollFriction',
  minFlingVelocity: 'caw:terminalScrollVelocityThreshold',
} as const

function readNumber(key: string, fallback: number, min: number, max: number): number {
  try {
    const raw = localStorage.getItem(key)
    if (raw === null) return fallback
    const n = parseFloat(raw)
    return Number.isFinite(n) ? Math.min(max, Math.max(min, n)) : fallback
  } catch {
    return fallback
  }
}

export function readTouchScrollSettings(): TouchScrollSettings {
  return {
    speed: readNumber(TOUCH_SCROLL_KEYS.speed, TOUCH_SCROLL_DEFAULTS.speed, 0.1, 5),
    friction: readNumber(TOUCH_SCROLL_KEYS.friction, TOUCH_SCROLL_DEFAULTS.friction, 0.8, 0.99),
    minFlingVelocity: readNumber(TOUCH_SCROLL_KEYS.minFlingVelocity, TOUCH_SCROLL_DEFAULTS.minFlingVelocity, 0, 1),
  }
}

const FRAME_MS = 1000 / 60
/** Finger travel before a touch becomes a scroll (keeps taps as taps). */
const SLOP_PX = 6
/** Window of recent touch samples used to estimate release velocity. */
const VELOCITY_WINDOW_MS = 100
/** A finger that rested this long before lifting doesn't fling. */
const HOLD_BEFORE_RELEASE_MS = 80
const MAX_FLING_VELOCITY = 8 // px/ms
/** Below this speed (px/ms) a fling hands over to a short settle onto its target row. */
const FLING_STOP_VELOCITY = 0.03
/** A touch only counts as "stopping a fling" (and is not a tap) above this speed (px/ms). */
const FLING_CATCH_VELOCITY = 0.05
/** Wheel notches sent to a TUI per frame at most (avoid flooding the PTY). */
const MAX_NOTCHES_PER_FRAME = 4

type Phase = 'idle' | 'pending' | 'drag' | 'fling' | 'settle'
type Mode = 'buffer' | 'wheel'

export interface TouchScrollDeps {
  getTerm: () => Terminal | undefined
  /** False while touch scrolling must stay native (e.g. soft keyboard open). */
  isEnabled: () => boolean
  /** A touch scroll gesture (drag or fling) started. */
  onScrollStart: () => void
  /** The gesture and its momentum finished; `atBottom` = viewport at the end of the output. */
  onScrollEnd: (atBottom: boolean) => void
}

function cellHeight(term: Terminal): number {
  const h = (term as any)._core?._renderService?.dimensions?.css?.cell?.height
  if (typeof h === 'number' && h > 0) return h
  const screen = term.element?.querySelector('.xterm-screen') as HTMLElement | null
  if (screen && term.rows > 0 && screen.clientHeight > 0) return screen.clientHeight / term.rows
  return 16
}

function appReceivesWheelReports(term: Terminal): boolean {
  const m = term.modes.mouseTrackingMode
  return m === 'vt200' || m === 'drag' || m === 'any'
}

function wheelOwnedByApp(term: Terminal): boolean {
  return term.buffer.active.type === 'alternate' || appReceivesWheelReports(term)
}

/** Rows of finger travel per wheel notch sent to an app (see header). */
const ROWS_PER_MOUSE_NOTCH = 3
const ROWS_PER_ARROW_NOTCH = 1

export function isTerminalAtBottom(term: Terminal): boolean {
  const buf = term.buffer.active
  return buf.viewportY >= buf.baseY
}

export function attachTouchScroll(el: HTMLElement, deps: TouchScrollDeps): () => void {
  let settings = readTouchScrollSettings()
  let phase: Phase = 'idle'
  let mode: Mode = 'buffer'
  let term: Terminal | undefined
  let screen: HTMLElement | null = null
  let renderSub: IDisposable | null = null

  // Gesture state
  let startX = 0
  let startY = 0
  let lastX = 0
  let lastY = 0
  let samples: { t: number; y: number }[] = []
  let swallowTap = false
  let carriedVelocity = 0
  let gestureStartT = 0

  // Scroll state. `pos` is the fractional top row (buffer mode); `wheelAccum`
  // collects partial notches (wheel mode).
  let pos = 0
  let appliedRow = 0
  let wheelAccum = 0
  let rowsPerNotch = ROWS_PER_ARROW_NOTCH
  let appliedOffset = 0
  let pendingOffset: number | null = null

  // Animation state. Velocity is in px/ms, positive = content moves toward
  // newer output (finger moving up).
  let velocity = 0
  let flingTau = 0
  let settleTarget: number | null = null
  let rafId = 0
  let lastFrameT = 0

  const setTransform = (px: number) => {
    if (!screen) return
    appliedOffset = px
    screen.style.transform = px ? `translate3d(0, ${-px}px, 0)` : ''
  }

  const clearOffset = () => {
    pendingOffset = null
    if (appliedOffset) setTransform(0)
  }

  const bindTerm = (): boolean => {
    term = deps.getTerm()
    if (!term || !term.element) return false
    const nextScreen = term.element.querySelector('.xterm-screen') as HTMLElement | null
    if (nextScreen !== screen) {
      if (screen && appliedOffset) screen.style.transform = ''
      screen = nextScreen
      appliedOffset = 0
    }
    renderSub?.dispose()
    // Row changes are rendered by xterm on its next animation frame. Apply
    // the matching sub-row offset only once those rows are in the DOM, so
    // rows and offset always change in the same painted frame (no 1-row
    // jitter at each row boundary).
    renderSub = term.onRender(() => {
      if (pendingOffset !== null) {
        setTransform(pendingOffset)
        pendingOffset = null
      }
    })
    return true
  }

  const unbindTerm = () => {
    renderSub?.dispose()
    renderSub = null
  }

  /** Move the buffer viewport by `deltaPx` (positive = toward newer output). Returns true when clamped at an edge. */
  const moveBuffer = (deltaPx: number): boolean => {
    if (!term) return true
    const buf = term.buffer.active
    // Follow viewport changes made by xterm itself (new output trimming the
    // scrollback, input snapping to the bottom, a resize reflow...).
    if (buf.viewportY !== appliedRow) {
      pos += buf.viewportY - appliedRow
      appliedRow = buf.viewportY
    }
    const ch = cellHeight(term)
    pos += deltaPx / ch
    const max = buf.baseY
    let clamped = false
    if (pos <= 0) {
      pos = 0
      clamped = true
    } else if (pos >= max) {
      pos = max
      clamped = true
    }
    let row = Math.floor(pos)
    // Treat float noise just below a row boundary as the boundary itself.
    if (pos - row > 0.999) row += 1
    const offset = Math.max(0, (pos - row) * ch)
    if (row !== buf.viewportY) {
      term.scrollToLine(row)
      appliedRow = term.buffer.active.viewportY
      pendingOffset = offset
    } else if (pendingOffset !== null) {
      pendingOffset = offset
    } else {
      setTransform(offset)
    }
    return clamped
  }

  const sendNotches = (deltaPx: number, limit: boolean) => {
    if (!term) return
    const ch = cellHeight(term)
    wheelAccum += deltaPx / (ch * rowsPerNotch)
    let n = Math.trunc(wheelAccum)
    if (n === 0) return
    wheelAccum -= n
    if (limit && Math.abs(n) > MAX_NOTCHES_PER_FRAME) n = Math.sign(n) * MAX_NOTCHES_PER_FRAME
    const target = screen ?? term.element
    if (!target) return
    const rect = target.getBoundingClientRect()
    const clientX = Math.min(rect.right - 1, Math.max(rect.left + 1, lastX))
    const clientY = Math.min(rect.bottom - 1, Math.max(rect.top + 1, lastY))
    // One event per notch: xterm.js turns each wheel event into exactly one
    // mouse report / arrow key regardless of its delta.
    const dir = n > 0 ? 1 : -1
    for (let i = 0; i < Math.abs(n); i++) {
      target.dispatchEvent(new WheelEvent('wheel', {
        deltaY: dir,
        deltaMode: WheelEvent.DOM_DELTA_LINE,
        bubbles: true,
        cancelable: true,
        clientX,
        clientY,
      }))
    }
  }

  const move = (deltaPx: number, fromAnimation: boolean): boolean => {
    if (mode === 'wheel') {
      sendNotches(deltaPx, fromAnimation)
      return false
    }
    return moveBuffer(deltaPx)
  }

  const stopRaf = () => {
    if (rafId) cancelAnimationFrame(rafId)
    rafId = 0
  }

  const finish = () => {
    stopRaf()
    phase = 'idle'
    velocity = 0
    if (mode === 'buffer' && term) {
      // Land exactly on a row; xterm renders whole rows only.
      if (appliedOffset || pendingOffset !== null) {
        const buf = term.buffer.active
        const row = Math.min(buf.baseY, Math.max(0, Math.round(pos)))
        if (row !== buf.viewportY) term.scrollToLine(row)
        appliedRow = term.buffer.active.viewportY
        pos = appliedRow
      }
      clearOffset()
    }
    wheelAccum = 0
    unbindTerm()
    deps.onScrollEnd(term ? isTerminalAtBottom(term) : true)
  }

  const frame = (t: number) => {
    rafId = 0
    if (!term) return finish()
    // Real elapsed time (physics stay correct on slow devices); capped so a
    // backgrounded tab doesn't jump on its first frame back.
    const dt = Math.min(250, Math.max(0, t - lastFrameT))
    lastFrameT = t
    if (phase === 'fling') {
      // Exact integration of v(t) = v0 * e^(-t/tau) over this frame.
      const decay = Math.exp(-dt / flingTau)
      const dist = velocity * flingTau * (1 - decay)
      velocity *= decay
      const clamped = move(dist, true)
      if (clamped || (mode === 'wheel' && Math.abs(velocity) < FLING_STOP_VELOCITY)) return finish()
      if (Math.abs(velocity) < FLING_STOP_VELOCITY) {
        // Ease the last few px onto the row the fling was aimed at instead
        // of crawling there for seconds.
        settleTarget = Math.round(pos + (velocity * flingTau) / cellHeight(term))
        phase = 'settle'
      }
    } else if (phase === 'settle') {
      const max = term.buffer.active.baseY
      const target = Math.min(max, Math.max(0, settleTarget ?? Math.round(pos)))
      const diff = target - pos
      if (Math.abs(diff) * cellHeight(term) < 0.5) return finish()
      const k = 1 - Math.pow(0.6, dt / FRAME_MS)
      moveBuffer(diff * k * cellHeight(term))
    } else {
      return
    }
    rafId = requestAnimationFrame(frame)
  }

  const startAnimation = (next: 'fling' | 'settle') => {
    phase = next
    settleTarget = null
    lastFrameT = performance.now()
    stopRaf()
    rafId = requestAnimationFrame(frame)
  }

  const releaseVelocity = (t: number): number => {
    const recent = samples.filter((s) => t - s.t <= VELOCITY_WINDOW_MS)
    if (recent.length < 2) return 0
    const last = recent[recent.length - 1]
    if (t - last.t > HOLD_BEFORE_RELEASE_MS) return 0
    const first = recent[0]
    const span = last.t - first.t
    if (span <= 0) return 0
    const v = ((first.y - last.y) / span) * settings.speed
    return Math.max(-MAX_FLING_VELOCITY, Math.min(MAX_FLING_VELOCITY, v))
  }

  const onTouchStart = (e: TouchEvent) => {
    if (e.touches.length !== 1 || !deps.isEnabled()) {
      if (phase === 'pending' || phase === 'drag') {
        // Second finger (pinch etc.): abandon the scroll.
        if (mode === 'buffer') startAnimation('settle')
        else finish()
      }
      return
    }
    // Touching a visibly moving terminal stops it, like native scrolling,
    // and that touch must not also act as a tap (focus / keyboard / click).
    // A fling that is only easing its last pixels is just snapped into place.
    const animating = phase === 'fling' && Math.abs(velocity) >= FLING_CATCH_VELOCITY
    if (!animating && (phase === 'fling' || phase === 'settle')) finish()
    swallowTap = animating
    carriedVelocity = animating ? velocity : 0
    stopRaf()
    if (!animating) {
      if (!bindTerm()) return
      settings = readTouchScrollSettings()
      mode = wheelOwnedByApp(term!) ? 'wheel' : 'buffer'
      rowsPerNotch = appReceivesWheelReports(term!) ? ROWS_PER_MOUSE_NOTCH : ROWS_PER_ARROW_NOTCH
      pos = term!.buffer.active.viewportY
      appliedRow = pos
      wheelAccum = 0
    }
    const t = e.touches[0]
    startX = lastX = t.clientX
    startY = lastY = t.clientY
    gestureStartT = e.timeStamp
    samples = [{ t: e.timeStamp, y: t.clientY }]
    velocity = 0
    phase = 'pending'
  }

  const onTouchMove = (e: TouchEvent) => {
    if (phase !== 'pending' && phase !== 'drag') return
    if (e.touches.length !== 1) return
    const t = e.touches[0]
    if (phase === 'pending') {
      const dx = t.clientX - startX
      const dy = t.clientY - startY
      if (Math.abs(dy) < SLOP_PX && Math.abs(dx) < SLOP_PX) {
        e.preventDefault()
        return
      }
      if (Math.abs(dx) > Math.abs(dy) && !swallowTap && appliedOffset === 0) {
        // Horizontal gesture: not ours.
        phase = 'idle'
        unbindTerm()
        return
      }
      phase = 'drag'
      // Start tracking from here so the content doesn't jump by the slop.
      lastY = t.clientY
      deps.onScrollStart()
    }
    // We are the only scroll authority for the terminal: keep the browser
    // from panning/zooming the page and xterm from seeing the move.
    e.preventDefault()
    e.stopPropagation()
    const dy = (lastY - t.clientY) * settings.speed
    lastX = t.clientX
    lastY = t.clientY
    samples.push({ t: e.timeStamp, y: t.clientY })
    if (samples.length > 32) samples.splice(0, samples.length - 32)
    if (dy) move(dy, false)
  }

  const onTouchEnd = (e: TouchEvent) => {
    if (phase === 'pending') {
      if (swallowTap) {
        e.preventDefault()
        swallowTap = false
        if (mode === 'buffer' && (appliedOffset || pendingOffset !== null)) startAnimation('settle')
        else finish()
      } else {
        phase = 'idle'
        unbindTerm()
      }
      return
    }
    if (phase !== 'drag') return
    if (e.touches.length > 0) return
    swallowTap = false
    // A cancelled touch (system gesture took over) doesn't fling.
    let v = e.type === 'touchcancel' ? 0 : releaseVelocity(e.timeStamp)
    // Repeated flicks in the same direction accumulate speed, as on iOS.
    if (carriedVelocity && Math.sign(carriedVelocity) === Math.sign(v) && e.timeStamp - gestureStartT < 400) {
      v = Math.max(-MAX_FLING_VELOCITY, Math.min(MAX_FLING_VELOCITY, v + carriedVelocity))
    }
    carriedVelocity = 0
    if (Math.abs(v) < settings.minFlingVelocity || !term) {
      if (mode === 'buffer') startAnimation('settle')
      else finish()
      return
    }
    flingTau = -FRAME_MS / Math.log(settings.friction)
    if (mode === 'buffer') {
      // Aim the fling so it comes to rest on a row boundary.
      const ch = cellHeight(term)
      const restRow = Math.round(pos + (v * flingTau) / ch)
      const aimed = ((restRow - pos) * ch) / flingTau
      if (Math.sign(aimed) === Math.sign(v)) v = aimed
    }
    velocity = v
    startAnimation('fling')
  }

  // A real mouse wheel / trackpad scroll takes over from any touch momentum.
  const onWheel = (e: WheelEvent) => {
    if (!e.isTrusted || phase === 'idle') return
    stopRaf()
    if (mode === 'buffer' && term) {
      pos = term.buffer.active.viewportY
      appliedRow = pos
    }
    clearOffset()
    finish()
  }

  el.addEventListener('touchstart', onTouchStart, { passive: true, capture: true })
  el.addEventListener('touchmove', onTouchMove, { passive: false, capture: true })
  el.addEventListener('touchend', onTouchEnd, { passive: false, capture: true })
  el.addEventListener('touchcancel', onTouchEnd, { passive: false, capture: true })
  el.addEventListener('wheel', onWheel, { passive: true, capture: true })

  return () => {
    stopRaf()
    unbindTerm()
    if (screen && appliedOffset) screen.style.transform = ''
    el.removeEventListener('touchstart', onTouchStart, { capture: true } as EventListenerOptions)
    el.removeEventListener('touchmove', onTouchMove, { capture: true } as EventListenerOptions)
    el.removeEventListener('touchend', onTouchEnd, { capture: true } as EventListenerOptions)
    el.removeEventListener('touchcancel', onTouchEnd, { capture: true } as EventListenerOptions)
    el.removeEventListener('wheel', onWheel, { capture: true } as EventListenerOptions)
  }
}
