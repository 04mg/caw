const DEVICE_ID_KEY = 'caw:deviceId'
const DEVICE_NAME_KEY = 'caw:deviceName'

function generateId(): string {
  return crypto.randomUUID()
}

function defaultDeviceName(): string {
  const ua = navigator.userAgent || ''
  let browser = 'Web'
  if (ua.includes('Chrome') && !ua.includes('Edg')) browser = 'Chrome'
  else if (ua.includes('Firefox')) browser = 'Firefox'
  else if (ua.includes('Safari') && !ua.includes('Chrome')) browser = 'Safari'
  else if (ua.includes('Edg')) browser = 'Edge'
  let os = ''
  if (ua.includes('Windows')) os = 'Windows'
  else if (ua.includes('Mac')) os = 'macOS'
  else if (ua.includes('Linux')) os = 'Linux'
  else if (ua.includes('iPhone') || ua.includes('iPad')) os = 'iOS'
  else if (ua.includes('Android')) os = 'Android'
  return os ? `${browser} · ${os}` : browser
}

export function getDeviceId(): string {
  let id = localStorage.getItem(DEVICE_ID_KEY)
  if (!id) {
    id = generateId()
    localStorage.setItem(DEVICE_ID_KEY, id)
  }
  return id
}

export function getDeviceName(): string {
  let name = localStorage.getItem(DEVICE_NAME_KEY)
  if (!name) {
    name = defaultDeviceName()
    localStorage.setItem(DEVICE_NAME_KEY, name)
  }
  return name
}

export function setDeviceName(name: string) {
  localStorage.setItem(DEVICE_NAME_KEY, name)
}
