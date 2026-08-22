// Audio playback for the bundled xpra client. Negotiates a codec with the
// server (preferring opus in a webm container via MediaSource Extensions,
// then mp3, then raw wav via the Web Audio API) and feeds the `sound-data`
// packet stream into a player.
//
// We don't depend on the upstream xpra-html5 MediaSourceUtil / aurora
// libraries: the codec set is tiny (Caw's use case is desktop-app audio) and
// modern browsers expose everything we need natively.

// xpra codec name -> MediaSource MIME string. Matches the subset of the
// upstream MediaSourceConstants.CODEC_STRING table we advertise to the
// server.
const MSE_CODEC_STRING: Record<string, string> = {
  'opus+mka': 'audio/webm; codecs="opus"',
  'mp3': 'audio/mpeg',
  'mp3+id3v2': 'audio/mpeg',
  'wav': 'audio/wav',
  'wave': 'audio/wav',
}

// Preference order: opus-in-webm is the best modern default, mp3 for older
// servers, wav as a lossless fallback the server can always produce.
const MSE_PREFERENCE = ['opus+mka', 'mp3+id3v2', 'mp3', 'wav', 'wave']

function mediaSourceAvailable(): boolean {
  return typeof MediaSource !== 'undefined' && MediaSource.isTypeSupported !== undefined
}

// pickMseCodec returns the best xpra codec the browser can decode via
// MediaSource, or null if none are available.
export function pickMseCodec(): string | null {
  if (!mediaSourceAvailable()) return null
  for (const codec of MSE_PREFERENCE) {
    const mime = MSE_CODEC_STRING[codec]
    if (mime && MediaSource.isTypeSupported(mime)) return codec
  }
  return null
}

// decoders advertised to the server in the hello `audio.decoders` list.
export function audioDecoders(): string[] {
  const codecs: string[] = []
  const mse = pickMseCodec()
  if (mse) codecs.push(mse)
  // wav is always decodable via the Web Audio API even without MSE.
  if (!codecs.includes('wav')) codecs.push('wav')
  return codecs
}

// bufferSource copies a rencode-decoded Uint8Array (typed by tsc as
// Uint8Array<ArrayBufferLike>) into a plain ArrayBuffer-backed view so it
// satisfies BufferSource (which excludes SharedArrayBuffer).
function bufferSource(buf: Uint8Array): ArrayBuffer {
  const ab = new ArrayBuffer(buf.byteLength)
  new Uint8Array(ab).set(buf)
  return ab
}

export class XpraAudio {
  private codec: string | null = null
  private framework: 'mediasource' | 'webaudio' | null = null
  private audio: HTMLAudioElement | null = null
  private mediaSource: MediaSource | null = null
  private sourceBuffer: SourceBuffer | null = null
  private objectUrl: string | null = null
  private queue: Uint8Array[] = []
  private appending = false
  private audioCtx: AudioContext | null = null
  private nextStartTime = 0
  private started = false
  // Avoids creating a new MediaSource / requesting the stream again after a
  // transient flush; the server pushes a fresh "start-of-stream" when audio
  // restarts.
  onStateChange: ((state: string, details?: string) => void) | null = null

  // negotiate picks the codec+framework and returns the xpra codec name to
  // request from the server (via a sound-control "start" packet).
  negotiate(): string | null {
    const mse = pickMseCodec()
    if (mse) {
      this.codec = mse
      this.framework = 'mediasource'
      return mse
    }
    // wav via Web Audio is always available.
    this.codec = 'wav'
    this.framework = 'webaudio'
    return 'wav'
  }

  get activeCodec(): string | null {
    return this.codec
  }

  start(): void {
    if (this.started) return
    this.started = true
    if (this.framework === 'mediasource') this.startMediaSource()
    else if (this.framework === 'webaudio') this.startWebAudio()
  }

  close(): void {
    this.started = false
    this.queue = []
    this.appending = false
    if (this.sourceBuffer && this.mediaSource && this.mediaSource.readyState === 'open') {
      try { this.mediaSource.removeSourceBuffer(this.sourceBuffer) } catch { /* ignore */ }
    }
    this.sourceBuffer = null
    if (this.mediaSource) {
      try { this.mediaSource.endOfStream() } catch { /* ignore */ }
      this.mediaSource = null
    }
    if (this.audio) {
      this.audio.pause()
      this.audio.src = ''
      this.audio.load()
      this.audio = null
    }
    if (this.objectUrl) {
      URL.revokeObjectURL(this.objectUrl)
      this.objectUrl = null
    }
    if (this.audioCtx) {
      try { void this.audioCtx.close() } catch { /* ignore */ }
      this.audioCtx = null
    }
    this.nextStartTime = 0
    this.onStateChange?.('closed')
  }

  // feed handles a sound-data packet's payload [codec, buf, options, metadata].
  feed(codec: string, buf: Uint8Array | null, options: Record<string, unknown>, _metadata: unknown): void {
    if (this.codec && codec !== this.codec) {
      this.onStateChange?.('error', `expected ${this.codec}, got ${codec}`)
      this.close()
      return
    }
    if (options['start-of-stream']) this.start()
    if (buf && buf.length > 0) {
      if (this.framework === 'mediasource') this.feedMediaSource(buf)
      else if (this.framework === 'webaudio') this.feedWebAudio(buf)
    }
    if (options['end-of-stream']) this.close()
  }

  private startMediaSource(): void {
    const codec = this.codec
    const mime = codec ? MSE_CODEC_STRING[codec] : null
    if (!codec || !mime) {
      this.onStateChange?.('error', 'no mse codec')
      return
    }
    const ms = new MediaSource()
    this.mediaSource = ms
    this.audio = document.createElement('audio')
    this.audio.setAttribute('autoplay', 'true')
    this.objectUrl = URL.createObjectURL(ms)
    this.audio.src = this.objectUrl
    this.audio.addEventListener('error', () => this.onStateChange?.('error', 'audio element error'))
    ms.addEventListener('error', () => this.onStateChange?.('error', 'media source error'))
    ms.addEventListener('sourceopen', () => {
      if (!this.mediaSource || this.sourceBuffer) return
      try {
        this.sourceBuffer = this.mediaSource.addSourceBuffer(mime)
      } catch (e) {
        this.onStateChange?.('error', `addSourceBuffer failed: ${e}`)
        return
      }
      this.onStateChange?.('ready')
      this.flushQueue()
    })
    // Append to the DOM (muted-style) so the browser actually plays.
    this.audio.style.display = 'none'
    document.body.appendChild(this.audio)
    void this.audio.play().catch((e) => this.onStateChange?.('error', `play() failed: ${e}`))
  }

  private feedMediaSource(buf: Uint8Array): void {
    this.queue.push(buf)
    this.flushQueue()
  }

  private flushQueue(): void {
    if (!this.sourceBuffer || this.appending) return
    if (this.sourceBuffer.updating || this.queue.length === 0) return
    const chunk = this.queue.shift()!
    this.appending = true
    try {
      this.sourceBuffer.appendBuffer(bufferSource(chunk))
    } catch (e) {
      this.appending = false
      this.onStateChange?.('error', `appendBuffer failed: ${e}`)
      return
    }
    this.sourceBuffer.addEventListener('updateend', () => {
      this.appending = false
      this.flushQueue()
    }, { once: true })
  }

  private startWebAudio(): void {
    const Ctor = window.AudioContext || (window as unknown as { webkitAudioContext: typeof AudioContext }).webkitAudioContext
    if (!Ctor) {
      this.onStateChange?.('error', 'no AudioContext')
      return
    }
    this.audioCtx = new Ctor()
    void this.audioCtx.resume().catch(() => {})
    this.onStateChange?.('ready')
  }

  private feedWebAudio(buf: Uint8Array): void {
    const ctx = this.audioCtx
    if (!ctx) return
    // wav is little-endian PCM in a RIFF container; decodeAudioData handles
    // 8/16/24/32-bit and float. The buffer is copied since decodeAudioData
    // detaches it.
    const data = bufferSource(buf)
    ctx.decodeAudioData(
      data,
      (decoded) => {
        const src = ctx.createBufferSource()
        src.buffer = decoded
        src.connect(ctx.destination)
        const startAt = Math.max(ctx.currentTime, this.nextStartTime)
        src.start(startAt)
        this.nextStartTime = startAt + decoded.duration
      },
      () => this.onStateChange?.('error', 'decodeAudioData failed'),
    )
  }
}