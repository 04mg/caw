import { Component, type ErrorInfo, type ReactNode, createElement } from 'react'

import { reportReactError } from './errorReporter'

interface ErrorBoundaryProps {
  children: ReactNode
  /** Optional name for this boundary, used in logs. */
  name?: string
  /**
   * Custom fallback UI. Defaults to a minimal inline error banner so a render
   * failure never blanks the whole screen.
   */
  fallback?: (error: Error, reset: () => void) => ReactNode
  /**
   * Called after the error has been reported. Useful for telemetry hooks or
   * triggering a toast. Defaults to a no-op.
   */
  onError?: (error: Error, componentStack: string) => void
}

interface ErrorBoundaryState {
  error: Error | null
}

/**
 * Top-level ErrorBoundary that reports render failures through the global error
 * reporter (rich traces + component stack) instead of letting React swallow
 * them into an opaque minified stack.
 *
 * Wrap the application root (and any subtrees prone to third-party throw
 * errors, e.g. resizable-panel layouts) so that a single component throwing
 * does not unmount the entire app silently.
 */
export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = { error: null }

  static getDerivedStateFromError(error: unknown): ErrorBoundaryState {
    return { error: error instanceof Error ? error : new Error(String(error)) }
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    const componentStack = info.componentStack ?? ''
    reportReactError(error, componentStack)
    this.props.onError?.(error, componentStack)
  }

  reset = (): void => {
    this.setState({ error: null })
  }

  render(): ReactNode {
    const { error } = this.state
    if (error) {
      const { fallback } = this.props
      if (fallback) return fallback(error, this.reset)
      return createElement(
        'div',
        {
          style: {
            padding: '12px 16px',
            background: '#3a0d0d',
            color: '#ffb4b4',
            fontFamily: 'monospace',
            fontSize: 12,
            whiteSpace: 'pre-wrap',
            overflow: 'auto',
            maxHeight: '100%',
          },
        },
        `${error.name}: ${error.message}\n\n${error.stack ?? ''}`,
      )
    }
    return this.props.children
  }
}