// The login's calls. The login itself is the one write that has no session yet, so it
// carries no CSRF header: the daemon checks the page's Origin against its public URL
// instead. The device calls are ordinary session writes and go through the shared client.
import { get, post } from '../api/client'

export interface LoginFailure {
  /** The daemon's one line for the person. */
  message: string
  /** Seconds until another try, when the account or address is locked. */
  retryAfter?: number
}

export type LoginResult = { ok: true } | ({ ok: false } & LoginFailure)

/** POST /v1/web/login: the password and a TOTP or recovery code, for a session cookie. */
export async function login(password: string, code: string, signal?: AbortSignal): Promise<LoginResult> {
  let res: Response
  try {
    res = await fetch('/v1/web/login', {
      method: 'POST',
      signal,
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify({ password, code }),
    })
  } catch {
    return { ok: false, message: 'The daemon could not be reached. Check the address and try again.' }
  }
  if (res.ok) return { ok: true }
  let message = `Sign-in failed (${res.status}).`
  try {
    const b = (await res.json()) as { error?: string }
    if (b.error) message = b.error
  } catch {
    /* a proxy's own error page: keep the status line */
  }
  const ra = Number(res.headers.get('Retry-After'))
  return { ok: false, message, ...(res.status === 429 && ra > 0 ? { retryAfter: ra } : {}) }
}

/** A device token as the daemon lists it: never the token or its hash. */
export interface Device {
  id: string
  name: string
  created: string
  last_used: string
  /** When it stops working if unused; the cap is `max_expires`. */
  expires: string
  max_expires: string
}

export const devices = {
  list: () => get<{ devices: Device[] | null }>('/v1/devices').then((r) => r.devices ?? []),
  revoke: (id: string) => post<{ revoked: boolean }>(`/v1/devices/${encodeURIComponent(id)}/revoke`, {}),
}

/** Ends every browser session and every device token, this one included. */
export const signOutEverywhere = () => post<{ sessions: number; devices: number }>('/v1/web/signout-all', {})
