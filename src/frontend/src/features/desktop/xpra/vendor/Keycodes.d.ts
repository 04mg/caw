// Keymap tables + modifier helpers, vendored from xpra-html5 (MPL 2.0).

export const KEY_TO_NAME: Record<string, string>
export const DEAD_KEYS: Record<string, string>
export const NUMPAD_TO_NAME: Record<string, string>
export const KEYSYM_TO_UNICODE: Record<string, string>
export const CHAR_TO_NAME: Record<string, string>
export const KEYSYM_TO_LAYOUT: Record<string, string>
export const CHARCODE_TO_NAME: Record<number, string>
export const CHARCODE_TO_NAME_SHIFTED: Record<number, string>

export function get_event_modifiers(event: KeyboardEvent | MouseEvent): string[]
export function translate_modifiers(modifiers: string[], swap_keys: boolean): string[]
export function patch_altgr(modifiers: string[]): string[]
export function parse_modifiers(modifier_keycodes: Record<string, unknown>): void
export function parse_server_modifiers(modifiers: unknown): string[]
export function parse_modifier_key(modifier: string, key: unknown): string[]