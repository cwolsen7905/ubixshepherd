import { Button } from './Button'
import { StatusGlyph } from './StatusGlyph'
import './ui.css'

/**
 * What a view shows when a read failed: what did not load, why, when, and the ways on. `Retry`
 * shows when the page can ask again; `Open status` always, as the daemon is the usual cause.
 * `request` is the failed call in mono, for the person who has to report it.
 */
export function ErrorState({ what, error, at, request, onRetry, back }: { what: string; error?: string; at?: string; request?: string; onRetry?: () => void; back?: { href: string; label: string } }) {
  return (
    <div className="ui-error" role="alert" data-component="ErrorState">
      <p className="ui-error-line">
        <StatusGlyph glyph="✗" tone="bad" label="Failed" /> {what} did not load.
        {error ? ` ${error.replace(/\.$/, '')}` : ''}
        {at ? ` at ${at}` : ''}.
      </p>
      {request && <p className="ui-error-request">{request}</p>}
      <p className="ui-error-actions">
        {onRetry && <Button size="small" onClick={onRetry}>Retry</Button>}
        {back && <a className="sh-link" href={back.href}>{back.label}</a>}
        <a className="sh-link" href="#/status">Open status</a>
      </p>
    </div>
  )
}
