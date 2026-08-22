// Keyboard and pointer input for the bundled xpra client. Translates DOM
// KeyboardEvent / MouseEvent / WheelEvent into xpra packets using the
// vendored Keycodes.js keymap tables.

import {
  KEY_TO_NAME,
  NUMPAD_TO_NAME,
  CHAR_TO_NAME,
  KEYSYM_TO_LAYOUT,
  CHARCODE_TO_NAME,
  CHARCODE_TO_NAME_SHIFTED,
  get_event_modifiers,
  translate_modifiers,
  patch_altgr,
} from './vendor/Keycodes.js'
import { PKT } from './packets'

export interface InputConfig {
  // Hotkey combos Caw intercepts at the app level; matching keydowns are NOT
  // forwarded to the remote so pane switching keeps working while the desktop
  // pane holds focus.
  interceptedCombos: string[]
}

export interface InputSink {
  send: (packet: unknown[]) => void
  focusedWid: () => number
  captureKeyboard: boolean
  altgr: boolean
}

// lastKeycodePressed mirrors upstream's dead-key tracking across keydown/up.
let lastKeycodePressed = 0
let numLock = false

export class XpraInput {
  private sink: InputSink
  private config: InputConfig

  constructor(sink: InputSink, config: InputConfig) {
    this.sink = sink
    this.config = config
  }

  setConfig(config: InputConfig) {
    this.config = config
  }

  // Returns true if the browser's default action should be allowed (so Caw's
  // global hotkey handler sees the event), false if we preventDefault'd it.
  onKeyDown(event: KeyboardEvent): boolean {
    return this.processKey(true, event)
  }

  onKeyUp(event: KeyboardEvent): boolean {
    return this.processKey(false, event)
  }

  private processKey(pressed: boolean, event: KeyboardEvent): boolean {
    if (!this.sink.captureKeyboard) return true

    // Intercept Caw hotkeys before forwarding: compose the combo string the
    // same way Caw's global handler does, and if it matches, leave the event
    // for the app.
    if (this.matchesInterceptedCombo(event)) return true

    let keyname = event.code || ''
    const keycode = event.which || event.keyCode
    if (keycode === 229) return false
    let keystring = event.key || String.fromCharCode(keycode)
    const dead = keystring.toLowerCase() === 'dead'

    // sync numlock
    if (keycode === 144 && pressed) numLock = !numLock

    if (dead && lastKeycodePressed !== keycode && !pressed) {
      // dead key release without a prior press: send a press+release pair
      this.emit(pressed, event, keyname, keystring, keycode)
      this.emit(false, event, keyname, keystring, keycode)
      lastKeycodePressed = 0
      return false
    }
    lastKeycodePressed = pressed ? keycode : 0

    if (dead && keyname in KEY_TO_NAME) {
      keyname = KEY_TO_NAME[keyname]
    } else if (keyname in KEY_TO_NAME) {
      keyname = KEY_TO_NAME[keyname]
    } else if (keyname === '' && keystring in KEY_TO_NAME) {
      keyname = KEY_TO_NAME[keystring]
    } else if (keyname !== keystring && keystring in NUMPAD_TO_NAME) {
      keyname = NUMPAD_TO_NAME[keystring]
      numLock = '0123456789.'.includes(keyname)
    } else if (keystring in CHAR_TO_NAME) {
      keyname = CHAR_TO_NAME[keystring]
      if (keyname.includes('_')) {
        const lang = keyname.split('_')[0]
        void KEYSYM_TO_LAYOUT[lang] // layout switch hook (kept for parity)
      }
    } else if (keycode in CHARCODE_TO_NAME) {
      keyname = CHARCODE_TO_NAME[keycode]
      if (event.getModifierState && event.getModifierState('Shift') && keycode in CHARCODE_TO_NAME_SHIFTED) {
        keyname = CHARCODE_TO_NAME_SHIFTED[keycode]
      }
    }

    // Right-side variants: _L codes used on the right become _R.
    const DOM_KEY_LOCATION_RIGHT = 2
    if (/_L$/.test(keyname) && event.location === DOM_KEY_LOCATION_RIGHT) {
      keyname = keyname.replace('_L', '_R')
    }

    // AltGr tracking: macOS reports Alt_L as the AltGraph modifier.
    if (keystring === 'AltGraph' || (keyname === 'Alt_R' && isWin()) || (keyname === 'Alt_L' && isMac())) {
      this.sink.altgr = pressed
      keyname = 'ISO_Level3_Shift'
      keystring = 'AltGraph'
    }

    const modifiers = this.eventModifiers(event)
    const keyval = keycode
    const group = 0
    const shift = modifiers.includes('shift')
    const capslock = modifiers.includes('capslock')
    if ((capslock && shift) || (!capslock && !shift)) keystring = keystring.toLowerCase()

    this.emitPacket(pressed, event, keyname, keystring, keyval, group, modifiers)
    // F11 should keep its default (fullscreen toggle) handler.
    if (keyname === 'F11') return true
    return false
  }

  private emit(pressed: boolean, event: KeyboardEvent, keyname: string, keystring: string, keycode: number): void {
    const modifiers = this.eventModifiers(event)
    this.emitPacket(pressed, event, keyname, keystring, keycode, 0, modifiers)
  }

  private emitPacket(pressed: boolean, _event: KeyboardEvent, keyname: string, keystring: string, keyval: number, group: number, modifiers: string[]): void {
    const wid = this.sink.focusedWid()
    this.sink.send([PKT.key_action, wid, keyname, pressed, modifiers, keyval, keystring, keyval, group])
  }

  private eventModifiers(event: KeyboardEvent | MouseEvent): string[] {
    const mods = get_event_modifiers(event)
    return this.sink.altgr ? translate_modifiers(patch_altgr(mods), false) : translate_modifiers(mods, false)
  }

  private matchesInterceptedCombo(event: KeyboardEvent): boolean {
    const combos = this.config.interceptedCombos
    if (combos.length === 0) return false
    const parts: string[] = []
    if (event.altKey) parts.push('Alt')
    if (event.ctrlKey) parts.push('Ctrl')
    if (event.metaKey) parts.push('Meta')
    if (event.shiftKey) parts.push('Shift')
    parts.push(event.key.length === 1 ? event.key.toUpperCase() : event.key)
    const combo = parts.join('+')
    return combos.includes(combo)
  }

  // ----- pointer -----

  onMouseDown(event: MouseEvent, wid: number): void {
    this.sendButton(wid, event, true)
  }

  onMouseUp(event: MouseEvent, wid: number): void {
    this.sendButton(wid, event, false)
  }

  onMouseMove(event: MouseEvent, wid: number): void {
    const { x, y } = this.pointerPos(event)
    this.sink.send([PKT.pointer_position, wid, [x, y], this.eventModifiers(event), []])
  }

  onWheel(event: WheelEvent, wid: number): void {
    // xpra expects button-action packets with button 4 (up) / 5 (down) /
    // 6 (left) / 7 (right). Send enough clicks to cover the delta.
    const modifiers = this.eventModifiers(event)
    const { x, y } = this.pointerPos(event)
    let button = 0
    let steps = 0
    if (event.deltaY !== 0) {
      button = event.deltaY > 0 ? 5 : 4
      steps = Math.max(1, Math.abs(Math.round(event.deltaY / 40)))
    } else if (event.deltaX !== 0) {
      button = event.deltaX > 0 ? 7 : 6
      steps = Math.max(1, Math.abs(Math.round(event.deltaX / 40)))
    }
    for (let i = 0; i < steps; i++) {
      this.sink.send([PKT.button_action, wid, button, true, [x, y], modifiers, []])
      this.sink.send([PKT.button_action, wid, button, false, [x, y], modifiers, []])
    }
  }

  private sendButton(wid: number, event: MouseEvent, pressed: boolean): void {
    // DOM buttons are 0-based; xpra buttons are 1-based (1=left,2=middle,3=right).
    const button = (event.which ?? event.button + 1) as number
    const { x, y } = this.pointerPos(event)
    this.sink.send([PKT.button_action, wid, button, pressed, [x, y], this.eventModifiers(event), []])
  }

  private pointerPos(event: MouseEvent | WheelEvent): { x: number; y: number } {
    return { x: Math.round(event.clientX), y: Math.round(event.clientY) }
  }
}

function isMac(): boolean {
  return typeof navigator !== 'undefined' && /Mac|iPhone|iPad|iPod/i.test(navigator.platform || navigator.userAgent)
}

function isWin(): boolean {
  return typeof navigator !== 'undefined' && /Win/i.test(navigator.platform || navigator.userAgent)
}