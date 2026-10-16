import { useCallback, useEffect, useState } from 'react'
import { ApiError, webSession, type SessionClock } from '../api/client'

/** How often the daemon is asked for the expiry; the countdown between asks is local. */
export const SESSION_POLL_MS = 60_000

export interface SessionState {
  /** Epoch ms the session ends; null until read, or when the daemon has no expiry to give (the dev proxy). */
  expires: number | null
  /** Epoch ms of the absolute cap. */
  cap: number | null
  /** Whole milliseconds left, or null. */
  left: number | null
  /** The daemon said the session is over, or its time ran out. */
  ended: boolean
  extending: boolean
  failure: string
  extend: () => Promise<void>
}

const ms = (s?: string) => {
  const t = s ? Date.parse(s) : NaN
  return Number.isNaN(t) ? null : t
}

/** The browser session's clock: read at mount and every minute, counted down locally. */
export function useSession(): SessionState {
  const [clock, setClock] = useState<{ expires: number | null; cap: number | null }>({ expires: null, cap: null })
  const [now, setNow] = useState(() => Date.now())
  const [over, setOver] = useState(false)
  const [extending, setExtending] = useState(false)
  const [failure, setFailure] = useState('')

  const take = useCallback((c: SessionClock) => {
    setClock({ expires: ms(c.expires), cap: ms(c.max_expires) })
    setNow(Date.now())
  }, [])

  useEffect(() => {
    let live = true
    const read = () => {
      webSession.read().then(
        (c) => live && take(c),
        (e) => {
          if (live && e instanceof ApiError && (e.status === 401 || e.status === 403)) setOver(true)
        },
      )
    }
    read()
    const poll = setInterval(read, SESSION_POLL_MS)
    const tick = setInterval(() => setNow(Date.now()), 15_000)
    return () => {
      live = false
      clearInterval(poll)
      clearInterval(tick)
    }
  }, [take])

  const extend = useCallback(async () => {
    setExtending(true)
    setFailure('')
    try {
      take(await webSession.extend())
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) setOver(true)
      else setFailure(e instanceof Error ? e.message : String(e))
    } finally {
      setExtending(false)
    }
  }, [take])

  const left = clock.expires === null ? null : clock.expires - now
  return { ...clock, left, ended: over || (left !== null && left <= 0), extending, failure, extend }
}
