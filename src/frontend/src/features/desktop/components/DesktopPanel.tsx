import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { Loader2, Maximize2 } from 'lucide-react'
import { ensureDesktop, desktopHealthCheck, markDesktopExited } from '../services/desktopRegistry'
import { parkIframe, takeIframe } from '../services/iframeLot'
import { getPrefs, subscribePrefs } from '@/features/prefs/stores/prefsStore'

interface DesktopPanelProps {
  leafId: string
  cwd: string
  cmd?: string[]
  env?: [string, string][]
  isActive: boolean
  // Called when the desktop app / stream is gone so the pane closes itself.
  onClose: (leafId: string) => void
}

type KeyboardApi = Navigator & {
  keyboard?: {
    lock?: (keys?: string[]) => Promise<void>
    unlock?: () => void
  }
}

// DesktopPanel renders an xpra-forwarded graphical application inside a
// pane. It is the desktop equivalent of TerminalPanel: ensureDesktop
// spawns the xpra server for the leaf, then an <iframe> loads the xpra
// HTML5 client (served by Caw at /desktop/{id}/) which connects back over
// the proxied WebSocket at /ws/desktop/{id}. The iframe is sandboxed to
// allow scripts, forms, popups and same-origin (required so the xpra
// client's WebSocket to the Caw proxy counts as same-origin).
//
// The iframe element is created imperatively and parked in iframeLot when
// the pane unmounts (tab/workspace switch), so coming back is instant —
// moving an iframe within the same document does not reload it. When the
// app closes or the stream dies, the pane closes itself via onClose.
export function DesktopPanel({ leafId, cwd, cmd, env, isActive, onClose }: DesktopPanelProps): ReactNode {
  const [ready, setReady] = useState(false)
  const [failed, setFailed] = useState(false)
  const [stream, setStream] = useState(() => getPrefs().desktopStream)
  const hostRef = useRef<HTMLDivElement>(null)
  const wrapperRef = useRef<HTMLDivElement>(null)
  const iframeRef = useRef<HTMLIFrameElement | null>(null)
  const onCloseRef = useRef(onClose)
  onCloseRef.current = onClose

  useEffect(() => subscribePrefs(() => setStream(getPrefs().desktopStream)), [])

  useEffect(() => {
    let cancelled = false
    let spawned = false
    setReady(false)
    setFailed(false)

    const boot = async () => {
      try {
        await ensureDesktop(leafId, cwd, cmd, env)
        if (!cancelled) {
          spawned = true
          setReady(true)
        }
      } catch (err) {
        console.error('desktop session init failed:', err)
        if (!cancelled) setFailed(true)
      }
    }
    void boot()

    return () => {
      cancelled = true
      // Park the live iframe instead of destroying it so remounting this
      // pane (tab/workspace switch) resumes instantly. Only park sessions
      // that were actually running; a failed spawn has nothing to keep.
      const iframe = iframeRef.current
      iframeRef.current = null
      if (iframe && spawned) parkIframe(leafId, iframe)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [leafId, cwd, cmd, env])

  // Periodically health-check the session so we can close the pane when
  // the xpra server dies (app closed, crash). The xpra HTML5 client has
  // its own reconnect logic for transient drops, so we poll on a slow
  // interval (3s) to avoid fighting it.
  useEffect(() => {
    if (!ready) return
    let active = true
    const poll = async () => {
      if (!active) return
      const alive = await desktopHealthCheck(leafId)
      if (!active) return
      if (!alive) {
        markDesktopExited(leafId)
        onCloseRef.current(leafId)
      }
    }
    const t = setInterval(poll, 3000)
    return () => { active = false; clearInterval(t) }
  }, [leafId, ready])

  // Build the xpra HTML5 client URL. The client is served by Caw at
  // /desktop/{id}/ (proxied to the xpra WS server's built-in HTTP). URL
  // params auto-connect the client to the proxied WS path /ws/desktop/{id}
  // without showing the connect dialog. exit_with_children=1 keeps the
  // session alive only while the app is running. The floating_menu/autohide
  // params strip the xpra chrome; encoding/quality/speed come from the
  // user's streaming preferences.
  const clientUrl = ready
    ? `/desktop/${encodeURIComponent(leafId)}/?action=connect&exit_with_children=1&submit=Connect&floating_menu=false&autohide=true&printing=false&file_transfer=false&sound=false&encoding=${encodeURIComponent(stream.encoding)}&quality=${stream.quality}&speed=${stream.speed}&path=${encodeURIComponent(`/ws/desktop/${encodeURIComponent(leafId)}`)}`
    : ''

  // Inject CSS + JS into the iframe document (same-origin via the Caw
  // proxy) to strip the remaining xpra chrome and start the app maximized:
  // - hide the floating menu, window borders/shadows and title bars drawn by
  //   the HTML5 client, and page margins so the app fills the pane edge-to-edge;
  // - hide the #progress overlay (the "Opening WebSocket connection…" text);
  // - maximize each window via the client's set_maximized() — the same code
  //   path as the native maximize button, which resizes the display to match
  //   the pane correctly (unlike --desktop-fullscreen, which only scales).
  //
  // The .windowhead uses height:0 rather than display:none because xpra's
  // JS reads the header's CSS height to compute its top offset — display:none
  // still yields 30px and leaves a dead, unclickable strip at the top.
  const injectChrome = useCallback((iframe: HTMLIFrameElement) => {
    const doc = iframe.contentDocument
    if (!doc) return
    if (!doc.getElementById('caw-chrome-style')) {
      const style = doc.createElement('style')
      style.id = 'caw-chrome-style'
      style.textContent = `
        #float_menu, #float_menu_button, #float_tray { display: none !important; }
        .window.border { border: none !important; box-shadow: none !important; border-radius: 0 !important; }
        .windowhead { display: none !important; height: 0 !important; }
        #progress { display: none !important; }
        html, body { margin: 0 !important; padding: 0 !important; overflow: hidden !important; background: transparent !important; }
      `
      doc.head.appendChild(style)
    }
    if (!doc.getElementById('caw-maximize-script')) {
      const script = doc.createElement('script')
      script.id = 'caw-maximize-script'
      script.textContent = `
        (function () {
          var maximized = {};
          var attempts = 0;
          var timer = setInterval(function () {
            attempts += 1;
            var client = window.client;
            if (!client || !client.id_to_window) {
              if (attempts > 100) clearInterval(timer);
              return;
            }
            var any = false;
            for (var wid in client.id_to_window) {
              any = true;
              if (!maximized[wid]) {
                maximized[wid] = true;
                try { client.id_to_window[wid].set_maximized(true); } catch (e) {}
              }
            }
            if (any && attempts > 100) clearInterval(timer);
          }, 200);
        })();
      `
      doc.head.appendChild(script)
    }
  }, [])

  // Mount/unmount the iframe element into the host div. Reuses a parked
  // iframe when one exists for this leaf (instant resume); otherwise
  // creates a fresh one that navigates to clientUrl once ready.
  useEffect(() => {
    const host = hostRef.current
    if (!host || !ready) return

    const parked = takeIframe(leafId)
    const reused = !!parked
    const iframe = parked ?? document.createElement('iframe')

    if (!reused) {
      iframe.title = 'Desktop'
      iframe.className = 'h-full w-full border-0'
      iframe.setAttribute('sandbox', 'allow-same-origin allow-scripts allow-forms allow-popups allow-popups-to-escape-sandbox allow-downloads')
      iframe.allow = 'clipboard-read; clipboard-write; fullscreen'
    }
    const onLoadHandler = () => injectChrome(iframe)
    iframe.addEventListener('load', onLoadHandler)
    iframeRef.current = iframe

    // Navigate only when needed: re-setting src on a reused iframe would
    // reload it and defeat the parking lot.
    if (iframe.dataset.cawSrc !== clientUrl) {
      iframe.dataset.cawSrc = clientUrl
      iframe.src = clientUrl
    }

    host.appendChild(iframe)
    injectChrome(iframe)

    return () => {
      iframe.removeEventListener('load', onLoadHandler)
      if (iframeRef.current === iframe) iframeRef.current = null
      if (iframe.parentElement === host) host.removeChild(iframe)
    }
  }, [leafId, ready, clientUrl, injectChrome])

  // Keyboard capture: while the pane is fullscreen, lock the keyboard so
  // browser-reserved shortcuts (Ctrl+W, Ctrl+T, …) reach the app instead
  // of the browser. Requires the Keyboard Lock API (Chromium) and only
  // takes effect in fullscreen; other browsers silently ignore it.
  const enterCapture = useCallback(async () => {
    const wrapper = wrapperRef.current
    if (!wrapper) return
    try {
      if (!document.fullscreenElement) await wrapper.requestFullscreen()
      await (navigator as KeyboardApi).keyboard?.lock?.()
    } catch {
      // Fullscreen denied or unsupported — ignore.
    }
  }, [])

  useEffect(() => {
    const onFsChange = () => {
      const wrapper = wrapperRef.current
      if (document.fullscreenElement && document.fullscreenElement === wrapper) {
        void (navigator as KeyboardApi).keyboard?.lock?.().catch(() => {})
      } else {
        ;(navigator as KeyboardApi).keyboard?.unlock?.()
      }
    }
    document.addEventListener('fullscreenchange', onFsChange)
    return () => {
      document.removeEventListener('fullscreenchange', onFsChange)
      if (document.fullscreenElement === wrapperRef.current) void document.exitFullscreen().catch(() => {})
      ;(navigator as KeyboardApi).keyboard?.unlock?.()
    }
  }, [])

  if (failed) {
    return (
      <div className="flex h-full w-full items-center justify-center bg-background text-muted-foreground p-4 text-center">
        <div>
          <p className="text-sm">Failed to start desktop session</p>
          <button
            className="mt-2 text-xs underline hover:text-foreground"
            onClick={() => { setFailed(false); setReady(false); ensureDesktop(leafId, cwd, cmd, env).then(() => setReady(true)).catch(() => setFailed(true)) }}
          >
            Restart
          </button>
        </div>
      </div>
    )
  }

  return (
    <div
      ref={wrapperRef}
      className="relative h-full w-full overflow-hidden bg-black"
      data-active={isActive ? 'true' : 'false'}
      data-pane-id={leafId}
    >
      {!ready && (
        <div className="absolute inset-0 flex items-center justify-center bg-black">
          <Loader2 className="h-6 w-6 animate-spin text-muted-foreground/60" />
        </div>
      )}
      <div ref={hostRef} className="h-full w-full" />
      {ready && (
        <button
          onClick={(e) => { e.stopPropagation(); void enterCapture() }}
          className="absolute top-1 right-1 z-20 h-5 w-5 rounded bg-background/80 text-muted-foreground hover:text-foreground flex items-center justify-center opacity-0 hover:opacity-100 focus:opacity-100 transition-opacity"
          title="Fullscreen (captures all keyboard input)"
        >
          <Maximize2 className="h-3 w-3" />
        </button>
      )}
    </div>
  )
}
