// A draw packet, decoded from rencode. Indices match the xpra wire layout:
//   [0] "draw", [1] wid, [2] x, [3] y, [4] width, [5] height, [6] coding,
//   [7] data, [8] packet_sequence, [9] rowstride, [10] options
export type DrawPacket = unknown[]

export function decode_rgb(packet: DrawPacket): Uint8Array
export function rgb24_to_rgb32(
  data: Uint8Array | ArrayBuffer,
  width: number,
  height: number,
  rowstride: number,
): Uint8Array