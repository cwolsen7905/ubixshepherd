import { SIGN_IN_HINT } from '../api/client'
import { Button } from '../ui/Button'
import { useSession } from './useSession'
import '../styles/session.css'

/** The banner appears this long before the session ends. */
export const EXTEND_WINDOW_MS = 30 * 60_000

/**
 * Says when the browser session is about to end and offers to extend it, then, once it has
 * ended, that a new sign-in link is needed. Nothing shows while there is time.
 */
export function SessionBanner() {
  const s = useSession()
  if (s.ended) {
    return (
      <div className="session-banner session-ended" role="alert">
        <span>Session ended. {SIGN_IN_HINT}</span>
      </div>
    )
  }
  if (s.left === null || s.left > EXTEND_WINDOW_MS) return null
  const minutes = Math.max(1, Math.ceil(s.left / 60_000))
  const atCap = s.cap !== null && s.expires !== null && s.cap - s.expires < 60_000
  return (
    <div className="session-banner" role="status">
      <span>
        Session ends in {minutes} min{atCap ? ': this is its longest stretch, sign in again after.' : '.'}
      </span>
      {!atCap && (
        <Button size="small" variant="attention" disabled={s.extending} onClick={() => void s.extend()}>
          {s.extending ? 'Extending…' : 'Extend'}
        </Button>
      )}
      {s.failure && <span className="session-failure">{s.failure}</span>}
    </div>
  )
}
