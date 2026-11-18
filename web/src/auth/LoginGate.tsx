import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { ApiError, webSession } from '../api/client'
import { LoginPage } from '../pages/Login'

/**
 * Shows the app when the browser has a session and the sign-in page when the daemon says
 * it has none (401). A daemon that cannot be reached shows the app, whose own offline
 * banner says so: a login form would be no use there. A daemon on loopback (plain http)
 * keeps the sign-in link flow: its 401 shows the app's own "run shepherd web" hint.
 */
export function LoginGate({
  children,
  read = webSession.read,
  hosted = window.location.protocol === 'https:',
}: {
  children: ReactNode
  read?: () => Promise<unknown>
  /** The daemon is reached over https, which only a hosted daemon is: loopback is plain http and signs in by link. */
  hosted?: boolean
}) {
  const [state, setState] = useState<'checking' | 'in' | 'out'>('checking')
  const check = useCallback(() => {
    read().then(
      () => setState('in'),
      (e: unknown) => setState(hosted && e instanceof ApiError && e.status === 401 ? 'out' : 'in'),
    )
  }, [read, hosted])
  useEffect(check, [check])
  if (state === 'checking') return null
  if (state === 'out') return <LoginPage onSignedIn={check} />
  return <>{children}</>
}
