import { useEffect, useId, useRef, useState, type FormEvent } from 'react'
import { login } from '../auth/api'
import { Button } from '../ui'
import './Login.css'

/** What the code box asks for, by the person's choice. */
const CODE_HINT = { totp: 'Six-digit code from your authenticator app', recovery: 'A recovery code, like abcde-fghij. Each works once.' }

/**
 * The sign-in page of a hosted daemon: the owner's password and a second-factor code. It
 * is shown when the daemon answers a read with 401, in place of the app. `onSignedIn`
 * runs once the daemon has set the session cookie.
 */
export function LoginPage({ onSignedIn, host = window.location.host }: { onSignedIn: () => void; host?: string }) {
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const [recovery, setRecovery] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [wait, setWait] = useState(0)
  const first = useRef<HTMLInputElement>(null)
  const pwId = useId()
  const codeId = useId()

  useEffect(() => first.current?.focus(), [])
  useEffect(() => {
    if (wait <= 0) return
    const t = setTimeout(() => setWait((w) => w - 1), 1000)
    return () => clearTimeout(t)
  }, [wait])

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (busy || wait > 0) return
    setBusy(true)
    setError('')
    const r = await login(password, code)
    setBusy(false)
    if (r.ok) {
      setPassword('')
      setCode('')
      onSignedIn()
      return
    }
    setError(r.message)
    setCode('')
    if (r.retryAfter) setWait(r.retryAfter)
  }

  return (
    <main className="login" data-component="LoginPage">
      <form className="login-card sh-card" onSubmit={submit} aria-labelledby={`${pwId}-h`}>
        <h1 id={`${pwId}-h`} className="login-title">
          Sign in to uBixShepherd
        </h1>
        <p className="login-host">{host}</p>
        {error && (
          <div className="sh-banner sh-banner--bad" role="alert">
            <span aria-hidden="true">✗ </span>
            {error}
            {wait > 0 && <> Try again in {wait} s.</>}
          </div>
        )}
        <label htmlFor={pwId} className="login-label">
          Password
        </label>
        <input id={pwId} ref={first} className="sh-input" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required />
        <label htmlFor={codeId} className="login-label">
          {recovery ? 'Recovery code' : 'Code'}
        </label>
        <input
          id={codeId}
          className="sh-input"
          type="text"
          inputMode={recovery ? 'text' : 'numeric'}
          autoComplete="one-time-code"
          autoCapitalize="none"
          spellCheck={false}
          value={code}
          onChange={(e) => setCode(e.target.value)}
          aria-describedby={`${codeId}-hint`}
          required
        />
        <p id={`${codeId}-hint`} className="login-hint">
          {recovery ? CODE_HINT.recovery : CODE_HINT.totp}
        </p>
        <div className="login-actions">
          <Button type="submit" variant="primary" disabled={busy || wait > 0 || !password || !code}>
            {busy ? 'Signing in' : 'Sign in'}
          </Button>
          <button
            type="button"
            className="login-link"
            onClick={() => {
              setRecovery((r) => !r)
              setCode('')
            }}
          >
            {recovery ? 'Use the authenticator code' : 'Use a recovery code'}
          </button>
        </div>
      </form>
    </main>
  )
}
