import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Loader2 } from 'lucide-react'
import { ensureDesktop, desktopHealthCheck, markDesktopExited } from '../services/desktopRegistry'

interface DesktopPanelProps {
  leafId: string
  cwd: string
  cmd?: string[]
  env?: [string, string][]
  isActive: boolean
}

// DesktopPanel renders an xpra-forwarded graphical application inside a
// pane. It is the desktop equivalent of TerminalPanel: ensureDesktop
// spawns the xpra server for the leaf, then an <iframe> loads the xpra
// HTML5 client (served by Caw at /desktop/{id}/) which connects back over
// the proxied WebSocket at /ws/desktop/{id}. The iframe is sandboxed to
// allow scripts, forms, popups and same-origin (required so the xpra
// client's WebSocket to the Caw proxy counts as same-origin).
export function DesktopPanel({ leafId, cwd, cmd, env, isActive }: DesktopPanelProps): ReactNode {
  const [ready, setReady] = useState(false)
  const [failed, setFailed] = useState(false)
  const iframeRef = useRef<HTMLIFrameElement>(null)

  useEffect(() => {
    let cancelled = false
    setReady(false)
    setFailed(false)
    ensureDesktop(leafId, cwd, cmd, env)
      .then(() => {
        if (!cancelled) setReady(true)
      })
      .catch((err) => {
        console.error('desktop session init failed:', err)
        if (!cancelled) setFailed(true)
      })
    return () => { cancelled = true }
  }, [leafId, cwd, cmd, env])

  // Periodically health-check the session so we can show a "session ended"
  // banner instead of a blank iframe when the xpra server dies (app
  // closed, crash). The xpra HTML5 client has its own reconnect logic for
  // transient drops, so we poll on a slow interval (5s) to avoid fighting
  // it.
  useEffect(() => {
    if (!ready) return
    let active = true
    const poll = async () => {
      if (!active) return
      const alive = await desktopHealthCheck(leafId)
      if (!active) return
      if (!alive) {
        markDesktopExited(leafId)
        setFailed(true)
      }
    }
    const t = setInterval(poll, 5000)
    return () => { active = false; clearInterval(t) }
  }, [leafId, ready])

  // Build the xpra HTML5 client URL. The client is served by Caw at
  // /desktop/{id}/ (proxied to the xpra WS server's built-in HTTP). URL
  // params auto-connect the client to the proxied WS path /ws/desktop/{id}
  // without showing the connect dialog. exit_with_children=1 keeps the
  // session alive only while the app is running. The floating_menu/autohide
  // params strip the xpra chrome so only the app UI is visible.
  const clientUrl = ready
    ? `/desktop/${encodeURIComponent(leafId)}/?action=connect&exit_with_children=1&submit=Connect&floating_menu=false&autohide=true&printing=false&file_transfer=false&sound=false&path=${encodeURIComponent(`/ws/desktop/${encodeURIComponent(leafId)}`)}`
    : ''

  // Inject CSS + JS into the iframe document (same-origin via the Caw
  // proxy) to strip the remaining xpra chrome and start the app maximized:
  // - hide the floating menu, window borders/shadows and title bars drawn by
  //   the HTML5 client, and page margins so the app fills the pane edge-to-edge;
  // - hide the #progress overlay (the "Opening WebSocket connection…" text);
  // - maximize each window via the client's set_maximized() — the same code
  //   path as the native maximize button, which resizes the display to match
  //   the pane correctly (unlike --desktop-fullscreen, which only scales).
  const injectChrome = () => {
    const doc = iframeRef.current?.contentDocument
    if (!doc) return
    const style = doc.createElement('style')
    style.textContent = `
      #float_menu, #float_menu_button, #float_tray { display: none !important; }
      .window.border { border: none !important; box-shadow: none !important; border-radius: 0 !important; }
      .windowhead { display: none !important; height: 0 !important; }
      #progress { display: none !important; }
      html, body { margin: 0 !important; padding: 0 !important; overflow: hidden !important; background: transparent !important; }
    `
    doc.head.appendChild(style)
    const script = doc.createElement('script')
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

  if (failed) {
    return (
      <div className="flex h-full w-full items-center justify-center bg-background text-muted-foreground p-4 text-center">
        <div>
          <p className="text-sm">Desktop session ended</p>
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
      className="relative h-full w-full overflow-hidden bg-black"
      data-active={isActive ? 'true' : 'false'}
      data-pane-id={leafId}
    >
      {!ready && (
        <div className="absolute inset-0 flex items-center justify-center bg-black">
          <Loader2 className="h-6 w-6 animate-spin text-muted-foreground/60" />
        </div>
      )}
      {ready && (
        <iframe
          ref={iframeRef}
          src={clientUrl}
          className="h-full w-full border-0"
          title="Desktop"
          sandbox="allow-same-origin allow-scripts allow-forms allow-popups allow-popups-to-escape-sandbox allow-downloads"
          allow="clipboard-read; clipboard-write; fullscreen"
          onLoad={injectChrome}
        />
      )}
    </div>
  )
}