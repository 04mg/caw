// Type declarations for the vendored xpra-html5 protocol modules. These
// are plain JS modules (see ./vendor/*.js) ported from
// https://github.com/Xpra-org/xpra-html5 and licensed under MPL 2.0.

export function rencode(obj: unknown): Uint8Array
export function rdecode(buf: Uint8Array): unknown
export function rencode_selftest(): boolean