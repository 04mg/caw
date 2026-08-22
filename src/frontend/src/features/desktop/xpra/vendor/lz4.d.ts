declare const lz4: {
  decode(data: Uint8Array): Uint8Array
  compress(src: Uint8Array, maxSize?: number): Uint8Array
  compressBound(n: number): number
  decompress(src: Uint8Array, maxSize?: number): Uint8Array
}

export { lz4 }