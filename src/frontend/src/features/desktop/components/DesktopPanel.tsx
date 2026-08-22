import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { Loader2, Maximize2, Volume2, VolumeX } from 'lucide-react'
import { desktopHealthCheck, ensureDesktop, getDesktopSession, markDesktopExited } from '../services/desktopRegistry'
import { acquireClient, attachClient, destroyClient, detachClient, getClient } from '../xpra/clientRegistry'
import type { XpraClient } from '../xpra/client'
import { getPrefs, subscribePrefs, getDesktopApps, getHotkey, DEFAULT_HOTKEYS, type DesktopStreamPrefs } from '@/features/prefs/stores/prefsStore'
import { DesktopAppIcon } from './DesktopAppIcon'

interface DesktopPanelProps {
  leafId: string
  cwd: string
  cmd?: string[]
  env?: [string, string][]
  isActive: boolean
  // Preview mode (workspace hover thumbnails): render a static placeholder
  // instead of spawning/attaching a real xpra session — hovering a sidebar
  // row must never launch processes or steal keyboard focus.
  preview?: boolean
  // Reports clicks on the desktop surface so the pane can become the
  // active pane.
  onFocusPane?: (leafId: string) => void
  // Called when the desktop app / stream is gone so the pane closes itself.
  onClose: (leafId: string) => void
}

type KeyboardApi = Navigator & {
  keyboard?: {
    lock?: (keys?: string[]) => Promise<void>
    unlock?: () => void
  }
}

// DesktopPanel renders an xpra-forwarded graphical application inside a pane
// using Caw's own bundled HTML5 client (../xpra). The client connects over
// the proxied WebSocket at /ws/desktop/{id} and draws into a canvas mounted
// directly in this pane — no iframe, no runtime style injection. The live
// client (WebSocket + canvases) is kept alive across tab/workspace switches
// in the client registry so resuming a pane is instant.
//
// It is the desktop equivalent of TerminalPanel: ensureDesktop spawns the
// xpra server for the leaf, then the bundled client connects and renders.
// When the app closes or the stream dies, the pane closes itself via
// onClose.
export function DesktopPanel({ leafId, cwd, cmd, env, isActive, preview, onFocusPane, onClose }: DesktopPanelProps): ReactNode {
  const [appMeta] = useState(() => {
    const apps = getDesktopApps()
    return (
      apps.find((a) => a.id === leafId) ??
      apps.find((a) => cmd && a.cmd.length === cmd.length && a.cmd.every((c, i) => c === cmd[i])) ??
      null
    )
  })
  // Fast path: when the session already exists (returning to a parked pane),
  // skip the spinner and surface it immediately.
  const [ready, setReady] = useState(() => {
    const s = getDesktopSession(leafId)
    return !!s && !s.exited
  })
  const [failed, setFailed] = useState(false)
  const [stream, setStream] = useState<DesktopStreamPrefs>(() => getPrefs().desktopStream)
  const [muted, setMuted] = useState(false)
  const hostRef = useRef<HTMLDivElement>(null)
  const clientRef = useRef<XpraClient | null>(null)
  const onCloseRef = useRef(onClose)
  onCloseRef.current = onClose
  const onFocusPaneRef = useRef(onFocusPane)
  onFocusPaneRef.current = onFocusPane

  useEffect(() => subscribePrefs(() => setStream(getPrefs().desktopStream)), [])

  // Spawn the xpra server for this leaf.
  useEffect(() => {
    if (preview) return
    let cancelled = false
    setFailed(false)
    const boot = async () => {
      try {
        await ensureDesktop(leafId, cwd, cmd, env)
        if (!cancelled) setReady(true)
      } catch (err) {
        console.error('desktop session init failed:', err)
        if (!cancelled) setFailed(true)
      }
    }
    void boot()
    return () => {
      cancelled = true
      // Detach the client's container (the session/WS keeps running).
      detachClient(leafId)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [leafId, cwd, cmd, env, preview])

  // Health-check the session so we can close the pane as soon as the xpra
  // server dies (app closed, crash). The bundled client also surfaces a
  // 'closed' state, but the poll catches process death even if the WS is
  // stuck reconnecting.
  useEffect(() => {
    if (!ready || preview) return
    let active = true
    const check = async () => {
      if (!active) return
      const alive = await desktopHealthCheck(leafId)
      if (!active) return
      if (!alive) {
        destroyClient(leafId)
        markDesktopExited(leafId)
        onCloseRef.current(leafId)
      }
    }
    void check()
    const t = setInterval(check, 1000)
    return () => { active = false; clearInterval(t) }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [leafId, ready, preview])

  // Acquire/attach the live client once the session is ready.
  useEffect(() => {
    const host = hostRef.current
    if (!host || !ready || preview) return

    const wsUrl = `ws://${location.host}/ws/desktop/${encodeURIComponent(leafId)}`
    const entry = acquireClient(
      leafId,
      wsUrl,
      stream,
      // onStateChange: surface only fatal 'closed'/'error' for now; the
      // spinner is driven by `ready`.
      (state, details) => {
        if (state === 'closed' || state === 'error') {
          console.warn('xpra client', state, details ?? '')
        }
      },
      () => { /* first window: nothing extra needed, spinner already hidden */ },
      (reason) => {
        void reason
        destroyClient(leafId)
        markDesktopExited(leafId)
        onCloseRef.current(leafId)
      },
    )
    clientRef.current = entry.client
    attachClient(leafId, host)

    // Push Caw hotkey combos so the client intercepts them (pane switching).
    const combos = Object.keys(DEFAULT_HOTKEYS)
      .map((action) => getHotkey(action))
      .filter(Boolean)
    entry.client.setInterceptedCombos(combos)

    // Size the client to the pane and keep it in sync on resize.
    const ro = new ResizeObserver(() => {
      const rect = host.getBoundingClientRect()
      const w = Math.max(1, Math.round(rect.width))
      const h = Math.max(1, Math.round(rect.height))
      entry.client.setScreenSize(w, h)
    })
    ro.observe(host)

    // Report pointer-down so the pane becomes active.
    const onPointerDown = () => onFocusPaneRef.current?.(leafId)
    host.addEventListener('pointerdown', onPointerDown)

    return () => {
      ro.disconnect()
      host.removeEventListener('pointerdown', onPointerDown)
      detachClient(leafId)
      clientRef.current = null
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [leafId, ready, preview])

  // Give the desktop surface keyboard focus whenever this pane becomes the
  // active pane.
  useEffect(() => {
    if (!isActive || !ready || preview) return
    // Focus the container so keystrokes land on the canvas/document.
    hostRef.current?.focus()
  }, [isActive, ready, preview])

  // Keyboard capture: while the pane is fullscreen, lock the keyboard so
  // browser-reserved shortcuts reach the app instead of the browser.
  const enterCapture = useCallback(async () => {
    const host = hostRef.current
    if (!host) return
    try {
      if (!document.fullscreenElement) await host.requestFullscreen()
      await (navigator as KeyboardApi).keyboard?.lock?.()
    } catch {
      // Fullscreen denied or unsupported — ignore.
    }
  }, [])

  useEffect(() => {
    const onFsChange = () => {
      const host = hostRef.current
      if (document.fullscreenElement && host && document.fullscreenElement === host) {
        void (navigator as KeyboardApi).keyboard?.lock?.().catch(() => {})
      } else {
        ;(navigator as KeyboardApi).keyboard?.unlock?.()
      }
    }
    document.addEventListener('fullscreenchange', onFsChange)
    return () => {
      const host = hostRef.current
      document.removeEventListener('fullscreenchange', onFsChange)
      if (host && document.fullscreenElement === host) void document.exitFullscreen().catch(() => {})
      ;(navigator as KeyboardApi).keyboard?.unlock?.()
    }
  }, [])

  // Mute toggle: ask the client to start/stop audio.
  const toggleMute = useCallback(() => {
    const c = getClient(leafId)
    if (!c) return
    const next = !c.isAudioMuted()
    c.setAudioMuted(next)
    setMuted(next)
  }, [leafId])

  if (preview) {
    return (
      <div className="flex h-full w-full flex-col items-center justify-center gap-2 bg-black" data-pane-id={leafId}>
        <DesktopAppIcon appId={appMeta?.id ?? leafId} icon={appMeta?.icon} iconColor={appMeta?.iconColor} size={40} className="opacity-80" />
        <span className="max-w-[80%] truncate text-xs text-muted-foreground/80">{appMeta?.label ?? cmd?.[0] ?? 'Desktop app'}</span>
      </div>
    )
  }

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
      className="relative h-full w-full overflow-hidden bg-black outline-none"
      data-active={isActive ? 'true' : 'false'}
      data-pane-id={leafId}
      tabIndex={0}
    >
      {!ready && (
        <div className="absolute inset-0 flex items-center justify-center bg-black">
          <Loader2 className="h-6 w-6 animate-spin text-muted-foreground/60" />
        </div>
      )}
      <div ref={hostRef} className="h-full w-full" />
      {ready && (
        <>
          <button
            onClick={(e) => { e.stopPropagation(); toggleMute() }}
            className="absolute top-1 right-7 z-20 h-5 w-5 rounded bg-background/80 text-muted-foreground hover:text-foreground flex items-center justify-center opacity-0 hover:opacity-100 focus:opacity-100 transition-opacity"
            title={muted ? 'Unmute audio' : 'Mute audio'}
          >
            {muted ? <VolumeX className="h-3 w-3" /> : <Volume2 className="h-3 w-3" />}
          </button>
          <button
            onClick={(e) => { e.stopPropagation(); void enterCapture() }}
            className="absolute top-1 right-1 z-20 h-5 w-5 rounded bg-background/80 text-muted-foreground hover:text-foreground flex items-center justify-center opacity-0 hover:opacity-100 focus:opacity-100 transition-opacity"
            title="Fullscreen (captures all keyboard input)"
          >
            <Maximize2 className="h-3 w-3" />
          </button>
        </>
      )}
    </div>
  )
}