// iframeLot keeps desktop-app <iframe> elements alive while their pane is
// not mounted (user switched tab or workspace). Moving an iframe within the
// same document does NOT reload it, so parking the element in a hidden
// container preserves the live xpra session: switching back is instant
// instead of replaying the full client handshake. This is the desktop
// equivalent of terminalRegistry's off-screen parking lot.

let container: HTMLDivElement | null = null
const parked = new Map<string, HTMLIFrameElement>()

function getContainer(): HTMLDivElement {
  if (!container) {
    container = document.createElement('div')
    container.style.display = 'none'
    document.body.appendChild(container)
  }
  return container
}

export function parkIframe(leafId: string, el: HTMLIFrameElement): void {
  const existing = parked.get(leafId)
  if (existing && existing !== el) existing.remove()
  parked.set(leafId, el)
  getContainer().appendChild(el)
}

export function takeIframe(leafId: string): HTMLIFrameElement | null {
  const el = parked.get(leafId)
  if (!el) return null
  parked.delete(leafId)
  return el
}

export function discardParkedIframe(leafId: string): void {
  const el = parked.get(leafId)
  if (el) {
    el.remove()
    parked.delete(leafId)
  }
}
