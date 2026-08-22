# Vendored Xpra HTML5 client modules

The files in this directory (`rencode.js`, `lz4.js`, `Protocol.js`,
`RgbHelpers.js`, `Keycodes.js`) are derived from the
[Xpra HTML5 client](https://github.com/Xpra-org/xpra-html5), copyright its
respective authors (see the headers in each file), and are licensed under the
**Mozilla Public License 2.0** (MPL-2.0).

Source commit at the time of vendoring: `master` branch of
`https://github.com/Xpra-org/xpra-html5`.

## Modifications

The files have been adapted for bundling inside Caw:

- Converted from plain `<script>`-loaded files to ES modules (added
  `import`/`export` statements).
- `Protocol.js`: removed the web-worker host class and worker bootstrap, and
  removed the AES encryption paths (Caw never enables xpra encryption).
- `RgbHelpers.js`: imports `lz4` instead of relying on a global.

Per the MPL-2.0, the modified files remain available under MPL-2.0; see
[`LICENSE`](./LICENSE) for the full text.

The higher-level client in this directory's parent (`../`) — the handshake,
draw pipeline, window model, input, clipboard and audio — is original Caw
code (not derived from xpra-html5's `Client.js`/`Window.js`).