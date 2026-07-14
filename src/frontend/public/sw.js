self.addEventListener('install', () => {
  self.skipWaiting()
})

self.addEventListener('activate', (event) => {
  event.waitUntil(self.clients.claim())
})

self.addEventListener('push', (event) => {
  let data = {}
  try {
    data = event.data ? event.data.json() : {}
  } catch {
    data = { title: 'Caw', body: event.data ? event.data.text() : '' }
  }

  const title = data.title || 'Caw'
  const body = data.body || ''
  const tag = data.sessionId ? `caw-${data.sessionId}` : 'caw-notification'

  event.waitUntil(
    self.registration.showNotification(title, {
      body,
      icon: '/icons/icon-192x192.png',
      badge: '/icons/icon-96x96.png',
      tag,
      data: {
        sessionId: data.sessionId || '',
        workspace: data.workspace || '',
        type: data.type || '',
      },
    })
  )
})

self.addEventListener('notificationclick', (event) => {
  event.notification.close()

  const sessionId = event.notification.data && event.notification.data.sessionId

  event.waitUntil(
    clients.matchAll({ type: 'window', includeUncontrolled: true }).then((clientList) => {
      for (const client of clientList) {
        client.focus()
        client.postMessage({
          type: 'notification-click',
          sessionId: sessionId || '',
        })
        return
      }
      return clients.openWindow('/')
    })
  )
})