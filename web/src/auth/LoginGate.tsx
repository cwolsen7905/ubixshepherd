import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { ApiError, webSession } from '../api/client'
import { LoginPage } from '../pages/Login'

/**
 * Shows the app when the browser has a session and the sign-in page when the daemon says
 * it has none (401). A daemon that cannot be reached shows the app, whose own offline
 * banner says so: a login form would be no use there. On loopback a 401 still means a
 * signed-out session, so the page tells the person to sign in, and the link flow stays.
 */
export function LoginGate({ children, read = webSession.read }: { children: ReactNode; read?: () => Promise<unknown> }) {
  const [state, setState] = useState<'checking' | 'in' | 'out'>('checking')
  const check = useCallback(() => {
    read().then(
      () => setState('in'),
      (e: unknown) => setState(e instanceof ApiError && e.status === 401 ? 'out' : 'in'),
    )
  }, [read])
  useEffect(check, [check])
  if (state === 'checking') return null
  if (state === 'out') return <LoginPage onSignedIn={check} />
  return <>{children}</>
}
