/**
 * Notification sound utilities.
 *
 * Audio files are served from /public and referenced by their root-relative paths.
 * We reuse a single HTMLAudioElement per sound type to avoid creating new DOM
 * nodes on every notification.
 */

const audioCache: Record<string, HTMLAudioElement> = {}

function getAudio(src: string): HTMLAudioElement {
  if (!audioCache[src]) {
    audioCache[src] = new Audio(src)
    audioCache[src].volume = 0.6
  }
  return audioCache[src]
}

/**
 * Play a notification sound non-blockingly.
 * Errors are silently swallowed — sound is best-effort.
 */
function playSound(src: string): void {
  try {
    const audio = getAudio(src)
    audio.currentTime = 0
    audio.play().catch(() => {
      // Browser autoplay policy may block playback — ignore.
    })
  } catch {
    // Ignore any synchronous errors (e.g. SSR or unsupported browser).
  }
}

export const Sounds = {
  waitingInput: () => playSound('/waiting_input.mp3'),
  finished: () => playSound('/finished.mp3'),
} as const
