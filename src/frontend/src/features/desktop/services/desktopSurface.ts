// desktopSurface keeps every desktop session's <iframe> alive for the
// lifetime of its session inside a single fixed-position layer attached to
// <body>. Iframes are NEVER detached once created — removing an iframe
// from the document discards its browsing context and forces a full
// reload — so "parking" a pane means hiding its surface instead, and
// switching tabs/workspaces resumes instantly with keyboard focus intact.

export interface DesktopSurface {
  wrapper: HTMLDivElement
  iframe: HTMLIFrameElement
}

const LAYER_ID = 'caw-desktop-surface-layer'

const surfaces = new Map<string, DesktopSurface>()
let layer: HTMLElement | null = null

function getLayer(): HTMLElement {
  if (layer) return layer
  layer = document.createElement('div')
  layer.id = LAYER_ID
  // Sits above regular pane content (z-auto / z-[1]) but BELOW the split
  // separator handles (z-10) so pane borders stay visible between desktop
  // panes, and below pane overlay controls (z-20); dialogs and dropdown
  // portals live at z-50. The layer ignores pointers; each visible wrapper
  // re-enables them so exactly the on-screen iframe is interactive.
  layer.style.cssText = 'position:fixed;inset:0;z-index:5;pointer-events:none;'
  document.body.appendChild(layer)
  // Fullscreen must fill the viewport even though wrappers carry inline
  // geometry while docked in a pane.
  const style = document.createElement('style')
  style.textContent = `
    #${LAYER_ID} > div:fullscreen { left:0 !important; top:0 !important; width:100vw !important; height:100vh !important; }
    /* During tab drags the wrappers must not eat pointer events, or the
       drop-overlay logic in the app never sees pointermove/pointerup. */
    #${LAYER_ID}[data-inert] > div { pointer-events: none !important; }
  `
  document.head.appendChild(style)
  return layer
}

// setDesktopSurfacesInert disables pointer interaction on every session
// wrapper while a tab drag is in progress, so drag/drop hit-testing flows
// through to the app's drop overlays.
export function setDesktopSurfacesInert(inert: boolean): void {
  if (!layer) return
  if (inert) layer.setAttribute('data-inert', '')
  else layer.removeAttribute('data-inert')
}

// acquireSurface returns (creating if needed) the persistent surface for a
// leaf and points its iframe at clientUrl — navigating only when the URL
// actually changed, so reacquiring an unchanged session never reloads.
export function acquireSurface(leafId: string, clientUrl: string): DesktopSurface {
  const l = getLayer()
  let s = surfaces.get(leafId)
  if (!s) {
    const wrapper = document.createElement('div')
    wrapper.style.cssText = 'position:fixed;left:0;top:0;width:0;height:0;visibility:hidden;pointer-events:auto;background:#000;overflow:hidden;'
    wrapper.dataset.leafId = leafId
    const iframe = document.createElement('iframe')
    iframe.title = 'Desktop'
    iframe.className = 'h-full w-full border-0'
    iframe.setAttribute('sandbox', 'allow-same-origin allow-scripts allow-forms allow-popups allow-popups-to-escape-sandbox allow-downloads')
    iframe.allow = 'clipboard-read; clipboard-write; fullscreen'
    wrapper.appendChild(iframe)
    l.appendChild(wrapper)
    s = { wrapper, iframe }
    surfaces.set(leafId, s)
  }
  if (s.iframe.dataset.cawSrc !== clientUrl) {
    s.iframe.dataset.cawSrc = clientUrl
    s.iframe.src = clientUrl
  }
  return s
}

// showSurface pins the surface over the pane's rect. Keyboard focus is
// restored to the iframe document ONLY on the hidden→visible transition:
// the caller invokes this every animation frame while docked, and grabbing
// focus per-frame would continuously steal keystrokes from terminal panes.
export function showSurface(leafId: string, rect: DOMRect): void {
  const s = surfaces.get(leafId)
  if (!s) return
  const st = s.wrapper.style
  st.left = `${rect.left}px`
  st.top = `${rect.top}px`
  st.width = `${rect.width}px`
  st.height = `${rect.height}px`
  const wasHidden = st.visibility !== 'visible'
  st.visibility = 'visible'
  if (wasHidden) s.iframe.contentWindow?.focus()
}

export function hideSurface(leafId: string): void {
  const s = surfaces.get(leafId)
  if (!s) return
  s.wrapper.style.visibility = 'hidden'
}

export function getSurface(leafId: string): DesktopSurface | undefined {
  return surfaces.get(leafId)
}

// isSurfaceElement reports whether el is one of our fullscreen-able
// wrappers, so cleanup code knows whether an active fullscreen belongs to
// a (possibly already unmounted) desktop pane.
export function isSurfaceElement(el: Element | null): boolean {
  return !!el && el.parentElement?.id === LAYER_ID
}

export function destroySurface(leafId: string): void {
  const s = surfaces.get(leafId)
  if (!s) return
  surfaces.delete(leafId)
  s.wrapper.remove()
}
