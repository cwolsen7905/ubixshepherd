import { Component, type ErrorInfo, type ReactNode } from 'react'

/** What a region shows when it threw: the message, and a retry that remounts the region. */
export function ErrorPanel({ title = 'Something went wrong here', error, onRetry }: { title?: string; error: unknown; onRetry?: () => void }) {
  const text = error instanceof Error ? error.message : String(error)
  return (
    <div className="banner tone-bad" role="alert">
      <strong>{title}.</strong> <span>{text}</span>{' '}
      {onRetry && (
        <button type="button" className="link-button" onClick={onRetry}>
          Retry
        </button>
      )}
    </div>
  )
}

interface Props {
  /** Names the region in the panel, e.g. "The page". */
  region: string
  /** A change of this value clears a caught error, so navigating away recovers the page. */
  resetKey?: unknown
  children: ReactNode
}

interface State {
  error: unknown
  failed: boolean
}

/** Catches a render error under it so one bad answer blanks a region, not the whole app. */
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null, failed: false }

  static getDerivedStateFromError(error: unknown): State {
    return { error, failed: true }
  }

  componentDidCatch(error: unknown, info: ErrorInfo) {
    console.error(`${this.props.region} failed to render`, error, info.componentStack)
  }

  componentDidUpdate(prev: Props) {
    if (this.state.failed && prev.resetKey !== this.props.resetKey) this.setState({ error: null, failed: false })
  }

  private retry = () => this.setState({ error: null, failed: false })

  render() {
    if (!this.state.failed) return this.props.children
    return <ErrorPanel title={`${this.props.region} could not be shown`} error={this.state.error} onRetry={this.retry} />
  }
}
