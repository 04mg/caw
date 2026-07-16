/**
 * Global error reporter.
 *
 * The frontend ships minified/obfuscated bundles, so raw stack traces from the
 * browser (e.g. `index-DdOCn7aO.js:9:49743`) are useless when triaging errors
 * like `Invalid 3 panel layout: 15%, 85%`. This module:
 *
 *   1. Installs `window` listeners for uncaught errors and unhandled rejections.
 *   2. Enriches each error with as much non-PII context as we can gather at
 *      throw time (error type, message, stack, source URL, component stack
 *      when available, recent console.error calls, and a rolling event log).
 *   3. Keeps an in-memory ring buffer of the most recent reports so they can
 *      be inspected in devtools (`window.__cawErrors`) or surfaced by the UI.
 *
 * The reporter is intentionally framework-agnostic and dependency-free so it
 * can be installed as early as possible in `main.tsx`, before React mounts.
 *
 * Nothing here is sent anywhere automatically — the goal is to give us
 * actionable local logs the next time an opaque error surfaces.
 */

const MAX_REPORTS = 50
const MAX_EVENT_LOG = 200
const MAX_CONSOLE_ERRORS = 20

export interface ErrorReport {
  id: string
  timestamp: number
  kind: 'error' | 'unhandledrejection' | 'react' | 'manual'
  message: string
  name: string
  stack?: string
  componentStack?: string
  source?: string
  lineno?: number
  colno?: number
  context: Record<string, unknown>
}

interface EventLogEntry {
  timestamp: number
  type: string
  detail: unknown
}

const reports: ErrorReport[] = []
const eventLog: EventLogEntry[] = []
const consoleErrors: string[] = []

let installed = false
let consoleErrorPatched = false
let seq = 0

function nextId(): string {
  seq += 1
  return `caw-err-${Date.now().toString(36)}-${seq.toString(36)}`
}

function logEvent(type: string, detail: unknown): void {
  eventLog.push({ timestamp: Date.now(), type, detail })
  while (eventLog.length > MAX_EVENT_LOG) eventLog.shift()
}

function stringify(value: unknown): string {
  if (value === undefined) return 'undefined'
  if (typeof value === 'string') return value
  try {
    return JSON.stringify(value, Object.getOwnPropertyNames(value ?? {}))
  } catch {
    return String(value)
  }
}

function buildContext(extra: Record<string, unknown> = {}): Record<string, unknown> {
  const ctx: Record<string, unknown> = {
    url: typeof location !== 'undefined' ? location.href : undefined,
    userAgent: typeof navigator !== 'undefined' ? navigator.userAgent : undefined,
  }

  if (typeof document !== 'undefined') {
    ctx.viewport = {
      width: window.innerWidth,
      height: window.innerHeight,
    }
  }

  try {
    const lsSidebar = localStorage.getItem('caw:sidebarCollapsed')
    const lsFolder = localStorage.getItem('caw:folderSidebarCollapsed')
    const lsSize = localStorage.getItem('caw:sidebarSize')
    if (lsSidebar !== null) ctx.cawSidebarCollapsed = lsSidebar
    if (lsFolder !== null) ctx.cawFolderSidebarCollapsed = lsFolder
    if (lsSize !== null) ctx.cawSidebarSize = lsSize
  } catch {
    // localStorage may be unavailable (private mode, sandbox)
  }

  ctx.recentEventLog = eventLog.slice(-20)
  ctx.recentConsoleErrors = consoleErrors.slice(-10)

  return { ...ctx, ...extra }
}

function record(report: ErrorReport): void {
  reports.push(report)
  while (reports.length > MAX_REPORTS) reports.shift()

  if (typeof window !== 'undefined') {
    ;(window as unknown as { __cawErrors?: ErrorReport[] }).__cawErrors = reports
  }

  const tag = `[caw:error:${report.kind}]`
  // eslint-disable-next-line no-console
  console.groupCollapsed(`${tag} ${report.message}`)
  // eslint-disable-next-line no-console
  console.error(report.stack || report.message)
  if (report.componentStack) {
    // eslint-disable-next-line no-console
    console.error('Component stack:\n', report.componentStack)
  }
  // eslint-disable-next-line no-console
  console.log('report', report)
  // eslint-disable-next-line no-console
  console.groupEnd()
}

function patchConsoleError(): void {
  if (consoleErrorPatched || typeof console === 'undefined') return
  consoleErrorPatched = true
  const original = console.error.bind(console)
  console.error = (...args: unknown[]) => {
    try {
      consoleErrors.push(args.map((a) => stringify(a)).slice(0, 500).join(' '))
      while (consoleErrors.length > MAX_CONSOLE_ERRORS) consoleErrors.shift()
    } catch {
      // ignore
    }
    original(...args)
  }
}

function extractComponentStack(error: unknown): string | undefined {
  if (error && typeof error === 'object') {
    const maybe = (error as { componentStack?: unknown }).componentStack
    if (typeof maybe === 'string' && maybe.length > 0) return maybe
    const digest = (error as { digest?: unknown }).digest
    if (typeof digest === 'string') return `digest: ${digest}`
  }
  return undefined
}

interface ReportOptions {
  kind?: ErrorReport['kind']
  componentStack?: string
  context?: Record<string, unknown>
  source?: string
  lineno?: number
  colno?: number
}

export function reportError(error: unknown, options: ReportOptions = {}): ErrorReport {
  const err = error instanceof Error ? error : undefined
  let message: string
  if (err) message = err.message
  else if (typeof error === 'string') message = error
  else message = stringify(error)

  const report: ErrorReport = {
    id: nextId(),
    timestamp: Date.now(),
    kind: options.kind ?? 'manual',
    message,
    name: (err && err.name) || (typeof error === 'string' ? 'String' : 'Unknown'),
    stack: err && err.stack,
    componentStack: options.componentStack ?? extractComponentStack(error),
    source: options.source,
    lineno: options.lineno,
    colno: options.colno,
    context: buildContext(options.context),
  }

  record(report)
  return report
}

export function getReports(): ErrorReport[] {
  return reports
}

export function getRecentEvents(): EventLogEntry[] {
  return eventLog
}

export function installGlobalErrorHandler(): void {
  if (installed || typeof window === 'undefined') return
  installed = true

  patchConsoleError()

  logEvent('install', 'global error handler installed')

  window.addEventListener('error', (event) => {
    logEvent('window.error', {
      message: event.message,
      filename: event.filename,
      lineno: event.lineno,
      colno: event.colno,
    })
    reportError(event.error ?? event.message, {
      kind: 'error',
      source: event.filename,
      lineno: event.lineno,
      colno: event.colno,
      context: { type: event.type, defaultPrevented: event.defaultPrevented },
    })
  })

  window.addEventListener('unhandledrejection', (event) => {
    logEvent('unhandledrejection', { reason: stringify(event.reason) })
    reportError(event.reason, {
      kind: 'unhandledrejection',
      context: { type: event.type },
    })
  })

  if (typeof window !== 'undefined') {
    ;(window as unknown as {
      __cawErrors?: ErrorReport[]
      __cawGetErrors?: () => ErrorReport[]
      __cawGetRecentEvents?: () => EventLogEntry[]
    }).__cawErrors = reports
    ;(window as unknown as { __cawGetErrors?: () => ErrorReport[] }).__cawGetErrors = getReports
    ;(window as unknown as { __cawGetRecentEvents?: () => EventLogEntry[] }).__cawGetRecentEvents = getRecentEvents
  }
}

/**
 * Append a component-stack-bearing report for a React render error.
 * Used by {@link ErrorBoundary} when it catches a render failure.
 */
export function reportReactError(error: unknown, componentStack: string): ErrorReport {
  logEvent('react.error', { message: error instanceof Error ? error.message : stringify(error) })
  return reportError(error, { kind: 'react', componentStack })
}