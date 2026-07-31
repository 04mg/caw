import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import '@/assets'
import App from './App'
import { ErrorBoundary } from '@/features/shared/errors/ErrorBoundary'
import { installGlobalErrorHandler } from '@/features/shared/errors/errorReporter'

// crypto.randomUUID() is gated to secure contexts (HTTPS or localhost) in
// browsers. When Caw is served over plain HTTP to a remote IP (e.g. a Linux
// server accessed from another machine), it is undefined and the workspace /
// terminal creation flow throws "crypto.randomUUID is not a function" before
// any /api/terminals request is made — so no terminal ever appears.
// crypto.getRandomValues is available in all contexts, so we polyfill a v4
// UUID from it. Install before any React render / app code runs.
if (typeof crypto !== 'undefined' && typeof crypto.randomUUID !== 'function') {
  // @ts-expect-error - augmenting read-only property
  crypto.randomUUID = function uuidv4(): string {
    const b = crypto.getRandomValues(new Uint8Array(16))
    b[6] = (b[6] & 0x0f) | 0x40
    b[8] = (b[8] & 0x3f) | 0x80
    const h = (n: number) => n.toString(16).padStart(2, '0')
    return `${h(b[0])}${h(b[1])}${h(b[2])}${h(b[3])}-${h(b[4])}${h(b[5])}-${h(b[6])}${h(b[7])}-${h(b[8])}${h(b[9])}-${h(b[10])}${h(b[11])}${h(b[12])}${h(b[13])}${h(b[14])}${h(b[15])}`
  }
}

// Install before React mounts so we capture errors during initial render.
installGlobalErrorHandler()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ErrorBoundary name="root">
      <App />
    </ErrorBoundary>
  </StrictMode>,
)

if ('serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js').then((reg) => {
      reg.addEventListener('updatefound', () => {
        const installing = reg.installing
        if (installing) {
          installing.addEventListener('statechange', () => {
            if (installing.state === 'activated') {
              reg.update().catch(() => {})
            }
          })
        }
      })
    }).catch((err) => {
      console.error('SW registration failed:', err)
    })
  })
}
