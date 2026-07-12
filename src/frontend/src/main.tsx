import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
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
