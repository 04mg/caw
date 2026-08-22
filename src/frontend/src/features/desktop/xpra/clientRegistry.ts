// Live-client keep-alive registry. A desktop session's WebSocket connection
// and window canvases survive pane unmounts (tab/workspace switches) so the
// stream resumes instantly — mirroring the old persistent-iframe behaviour
// without the iframe. The client's DOM container is moved between panes
// rather than recreated, so no re-handshake or re-paint is needed.

import { XpraClient, type ConnectionState } from './client'
import type { DesktopStreamPrefs } from '@/features/prefs/stores/prefsStore'

interface Entry {
  client: XpraClient
  container: HTMLDivElement
}

const registry = new Map<string, Entry>()

// acquire returns (creating if needed) the live client for a leaf. The
// client owns a detached <div> container; the caller mounts it into its
// pane via attach().
export function acquireClient(
  leafId: string,
  wsUrl: string,
  stream: DesktopStreamPrefs,
  onStateChange: (state: ConnectionState, details?: string) => void,
  onFirstWindow: () => void,
  onClose: (reason?: string) => void,
): { client: XpraClient; container: HTMLDivElement } {
  let entry = registry.get(leafId)
  if (!entry) {
    const container = document.createElement('div')
    container.style.cssText = 'position:relative;width:100%;height:100%;background:#000;overflow:hidden;'
    const client = new XpraClient(container, wsUrl, stream, { onStateChange, onFirstWindow, onClose })
    client.connect()
    entry = { client, container }
    registry.set(leafId, entry)
  } else {
    // Re-wire callbacks for the new React mount (the old pane is gone).
    entry.client.setCallbacks({ onStateChange, onFirstWindow, onClose })
  }
  return entry
}

export function attachClient(leafId: string, host: HTMLElement): HTMLDivElement | null {
  const entry = registry.get(leafId)
  if (!entry) return null
  if (entry.container.parentElement !== host) host.appendChild(entry.container)
  return entry.container
}

export function detachClient(leafId: string): void {
  const entry = registry.get(leafId)
  if (!entry) return
  // Keep the container in the registry but take it out of the DOM; the WS
  // keeps flowing and the canvases keep their last frame.
  entry.container.remove()
}

export function destroyClient(leafId: string): void {
  const entry = registry.get(leafId)
  if (!entry) return
  registry.delete(leafId)
  entry.client.close()
  entry.container.remove()
}

export function getClient(leafId: string): XpraClient | undefined {
  return registry.get(leafId)?.client
}

export function hasClient(leafId: string): boolean {
  return registry.has(leafId)
}