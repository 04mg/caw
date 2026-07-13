export type FileCategory = 'text' | 'image' | 'binary-likely'

const IMAGE_EXTENSIONS = new Set([
  'png', 'jpg', 'jpeg', 'gif', 'webp', 'svg', 'bmp', 'ico', 'avif', 'tiff',
])

const KNOWN_BINARY_EXTENSIONS = new Set([
  'exe', 'dll', 'so', 'dylib', 'bin', 'o', 'a', 'lib',
  'zip', 'tar', 'gz', 'bz2', 'xz', '7z', 'rar',
  'pdf', 'doc', 'docx', 'xls', 'xlsx', 'ppt', 'pptx',
  'mp3', 'mp4', 'wav', 'ogg', 'flac', 'mkv', 'avi', 'mov',
  'wasm', 'pyc', 'class',
])

export function getFileCategory(path: string): FileCategory {
  const ext = path.split('.').pop()?.toLowerCase() ?? ''
  if (IMAGE_EXTENSIONS.has(ext)) return 'image'
  if (KNOWN_BINARY_EXTENSIONS.has(ext)) return 'binary-likely'
  return 'text'
}

/**
 * Runtime heuristic: scan first 8KB for null bytes or high non-printable ratio.
 * Used as a fallback when extension is not conclusive.
 */
export function isBinaryContent(content: string): boolean {
  const sample = content.slice(0, 8192)
  let nonPrintable = 0
  for (let i = 0; i < sample.length; i++) {
    const c = sample.charCodeAt(i)
    if (c === 0) return true // null byte is definitive
    if (c < 32 && c !== 9 && c !== 10 && c !== 13) nonPrintable++
  }
  if (sample.length === 0) return false
  return nonPrintable / sample.length > 0.1
}
